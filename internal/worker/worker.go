package worker

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v69/github"
	"github.com/tinybluerobots/cq/internal/config"
	"github.com/tinybluerobots/cq/internal/notify"
	"github.com/tinybluerobots/cq/internal/poller"
	"github.com/tinybluerobots/cq/internal/prompt"
	"github.com/tinybluerobots/cq/internal/ratelimit"
	"github.com/tinybluerobots/cq/internal/state"
)

// Worker processes GitHub issues by invoking the Claude CLI.
type Worker struct {
	Client     *github.Client
	State      *state.Store
	Notifier   *notify.Notifier
	CLIConfig  config.CLIConfig
	Workspace  string
	ClaudePath string // path to claude binary, defaults to "claude"
	CloneURL   string // override clone URL for testing (use local dir)
}

// BuildPrompt renders the prompt template for a given issue.
func (w *Worker) BuildPrompt(repo string, issue *github.Issue) (string, error) {
	return prompt.Render(w.CLIConfig.PromptFile, repo, issue)
}

// EnsureRepo clones a repository if it doesn't exist, or pulls latest changes.
func (w *Worker) EnsureRepo(ctx context.Context, cloneURL, repo string) (string, error) {
	repoDir := filepath.Join(w.Workspace, repo)

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); os.IsNotExist(err) {
		return w.cloneRepo(ctx, cloneURL, repoDir)
	}

	return w.pullRepo(ctx, repoDir)
}

func (w *Worker) cloneRepo(ctx context.Context, cloneURL, repoDir string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(repoDir), 0755); err != nil {
		return "", fmt.Errorf("mkdir workspace: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, repoDir)

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git clone: %w", err)
	}

	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "config", "user.email", "cq@bot").Run(); err != nil {
		return "", fmt.Errorf("git config user.email: %w", err)
	}

	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "config", "user.name", "cq").Run(); err != nil {
		return "", fmt.Errorf("git config user.name: %w", err)
	}

	return repoDir, nil
}

func (w *Worker) pullRepo(ctx context.Context, repoDir string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "pull", "--ff-only")

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git pull: %w", err)
	}

	return repoDir, nil
}

// RunCommand executes the configured command with the prompt on stdin.
// If no command is configured, it defaults to the Claude CLI.
func (w *Worker) RunCommand(ctx context.Context, workDir, prompt string) (string, error) {
	if w.CLIConfig.Command != "" {
		return w.runCustomCommand(ctx, workDir, prompt)
	}

	return w.runClaude(ctx, workDir, prompt)
}

func (w *Worker) runCustomCommand(ctx context.Context, workDir, prompt string) (string, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", w.CLIConfig.Command)
	cmd.Dir = workDir
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("command exited with error: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// streamJSON represents a line of Claude's stream-json output.
type streamJSON struct {
	Type   string `json:"type"`
	Result string `json:"result"`
	Text   string `json:"text"`
}

func (w *Worker) runClaude(ctx context.Context, workDir, prompt string) (string, error) {
	claudeBin := w.ClaudePath
	if claudeBin == "" {
		claudeBin = "claude"
	}

	cmd := exec.CommandContext(ctx, claudeBin, "-p", prompt, "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose")
	cmd.Dir = workDir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start claude: %w", err)
	}

	var result string

	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()

		var msg streamJSON
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "assistant":
			if msg.Text != "" {
				slog.Info("claude", "text", msg.Text)
			}
		case "result":
			result = msg.Result
		}
	}

	if err := cmd.Wait(); err != nil {
		return "", fmt.Errorf("claude exited with error: %w", err)
	}

	return result, nil
}

// ProcessIssue orchestrates the full issue processing flow.
func (w *Worker) ProcessIssue(ctx context.Context, repo string, issue *github.Issue) {
	key := fmt.Sprintf("%s#%d", repo, issue.GetNumber())
	logger := slog.With("issue", key)

	// Mark in_progress.
	attempts := w.getAttempts(key)
	w.State.Set(key, state.IssueState{
		Status:      state.StatusInProgress,
		Attempts:    attempts + 1,
		LastAttempt: time.Now(),
	})

	if err := w.State.Save(); err != nil {
		logger.Error("save state", "error", err)
	}

	// Parse per-issue config.
	body := ""
	if issue.Body != nil {
		body = *issue.Body
	}

	issueCfg := config.ParseIssueConfig(body)
	strategy := config.ResolveStrategy(w.CLIConfig, issueCfg)
	branch := config.ResolveBranch(issueCfg, issue.GetNumber())

	var (
		repoDir string
		err     error
	)

	if w.CLIConfig.Local {
		repoDir, err = os.Getwd()
		if err != nil {
			w.fail(ctx, key, logger, fmt.Sprintf("getwd: %v", err))
			return
		}
	} else {
		cloneURL := w.CloneURL
		if cloneURL == "" {
			cloneURL = fmt.Sprintf("https://github.com/%s.git", repo)
		}

		repoDir, err = w.EnsureRepo(ctx, cloneURL, repo)
		if err != nil {
			w.fail(ctx, key, logger, fmt.Sprintf("ensure repo: %v", err))
			return
		}
	}

	// If PR strategy, create branch.
	if strategy == "pr" {
		cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", "-b", branch)

		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			w.fail(ctx, key, logger, fmt.Sprintf("checkout branch: %v", err))
			return
		}
	}

	// Run command.
	prompt, err := w.BuildPrompt(repo, issue)
	if err != nil {
		w.fail(ctx, key, logger, fmt.Sprintf("build prompt: %v", err))
		return
	}

	result, err := w.RunCommand(ctx, repoDir, prompt)
	if err != nil {
		w.fail(ctx, key, logger, fmt.Sprintf("claude: %v", err))
		return
	}

	// Check for failure marker.
	if strings.Contains(result, "ISSUE_FAILED") {
		w.fail(ctx, key, logger, fmt.Sprintf("claude reported failure: %s", result))
		return
	}

	// Dry-run: show diff and mark complete without pushing.
	if w.CLIConfig.DryRun {
		diffCmd := exec.CommandContext(ctx, "git", "-C", repoDir, "diff", "HEAD")
		diffCmd.Stdout = os.Stdout
		diffCmd.Stderr = os.Stderr
		_ = diffCmd.Run()

		logger.Info("dry-run complete, skipping push")

		w.State.Set(key, state.IssueState{
			Status:      state.StatusCompleted,
			Attempts:    attempts + 1,
			LastAttempt: time.Now(),
		})

		if err := w.State.Save(); err != nil {
			logger.Error("save state", "error", err)
		}

		return
	}

	// Push and optionally create PR.
	if strategy == "pr" {
		if err := w.pushAndCreatePR(ctx, key, logger, repoDir, repo, branch, issue, attempts); err != nil {
			return
		}
	} else {
		// Direct strategy: push to current branch.
		cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "push")

		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			w.fail(ctx, key, logger, fmt.Sprintf("git push: %v", err))
			return
		}

		w.State.Set(key, state.IssueState{
			Status:      state.StatusCompleted,
			Attempts:    attempts + 1,
			LastAttempt: time.Now(),
		})
	}

	if err := w.State.Save(); err != nil {
		logger.Error("save state", "error", err)
	}

	logger.Info("issue processed", "status", state.StatusCompleted)
}

// pushAndCreatePR pushes the branch and creates a pull request via the GitHub API.
// It returns a non-nil error if any step failed (the failure is already recorded via w.fail).
func (w *Worker) pushAndCreatePR(ctx context.Context, key string, logger *slog.Logger, repoDir, repo, branch string, issue *github.Issue, attempts int) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "push", "-u", "origin", branch)

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		w.fail(ctx, key, logger, fmt.Sprintf("git push branch: %v", err))
		return err
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		w.fail(ctx, key, logger, fmt.Sprintf("invalid repo format: %s", repo))
		return fmt.Errorf("%w: %s", poller.ErrInvalidRepoFormat, repo)
	}

	// Check for existing PR on this branch to avoid duplicates.
	existing, _, err := w.Client.PullRequests.List(ctx, parts[0], parts[1], &github.PullRequestListOptions{
		Head:  fmt.Sprintf("%s:%s", parts[0], branch),
		State: "open",
	})
	if err != nil {
		ratelimit.Wait(ctx, err)
		logger.Warn("check existing PRs", "error", err)
		// Non-fatal: proceed to create, GitHub will reject if duplicate.
	}

	if len(existing) > 0 {
		pr := existing[0]
		logger.Info("PR already exists", "url", pr.GetHTMLURL())

		w.State.Set(key, state.IssueState{
			Status:      state.StatusCompleted,
			Attempts:    attempts + 1,
			LastAttempt: time.Now(),
			PRURL:       pr.GetHTMLURL(),
		})

		return nil
	}

	prTitle := fmt.Sprintf("Fix #%d: %s", issue.GetNumber(), issue.GetTitle())
	prBody := fmt.Sprintf("Resolves #%d\n\nAutomated by cq.", issue.GetNumber())
	base := "main"

	var pr *github.PullRequest

	pr, _, err = w.Client.PullRequests.Create(ctx, parts[0], parts[1], &github.NewPullRequest{
		Title: &prTitle,
		Body:  &prBody,
		Head:  &branch,
		Base:  &base,
	})
	if err != nil {
		if ratelimit.Wait(ctx, err) {
			pr, _, err = w.Client.PullRequests.Create(ctx, parts[0], parts[1], &github.NewPullRequest{
				Title: &prTitle,
				Body:  &prBody,
				Head:  &branch,
				Base:  &base,
			})
		}

		if err != nil {
			w.fail(ctx, key, logger, fmt.Sprintf("create PR: %v", err))
			return err
		}
	}

	w.State.Set(key, state.IssueState{
		Status:      state.StatusCompleted,
		Attempts:    attempts + 1,
		LastAttempt: time.Now(),
		PRURL:       pr.GetHTMLURL(),
	})

	return nil
}

func (w *Worker) fail(ctx context.Context, key string, logger *slog.Logger, reason string) {
	logger.Error("issue processing failed", "reason", reason)

	attempts := w.getAttempts(key)
	w.State.Set(key, state.IssueState{
		Status:      state.StatusFailed,
		Attempts:    attempts,
		LastAttempt: time.Now(),
		Error:       reason,
	})

	if err := w.State.Save(); err != nil {
		logger.Error("save state", "error", err)
	}

	if err := w.Notifier.Send(ctx, fmt.Sprintf("cq failed %s: %s", key, reason)); err != nil {
		logger.Error("send notification", "error", err)
	}
}

func (w *Worker) getAttempts(key string) int {
	st, ok := w.State.Get(key)
	if !ok {
		return 0
	}

	return st.Attempts
}
