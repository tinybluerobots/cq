package worker

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/go-github/v69/github"
	"github.com/tinybluerobots/issuebot/internal/config"
	"github.com/tinybluerobots/issuebot/internal/notify"
	"github.com/tinybluerobots/issuebot/internal/poller"
	"github.com/tinybluerobots/issuebot/internal/prompt"
	"github.com/tinybluerobots/issuebot/internal/ratelimit"
	"github.com/tinybluerobots/issuebot/internal/state"
)

// Worker processes GitHub issues by invoking a configured command.
type Worker struct {
	Client    *github.Client
	State     *state.Store
	Notifier  *notify.Notifier
	CLIConfig config.CLIConfig
	CloneURL  string // override clone URL for testing (use local dir)
}

// BuildPrompt renders the prompt template for a given issue.
func (w *Worker) BuildPrompt(repo string, issue *github.Issue, defaultBranch string) (string, error) {
	return prompt.Render(w.CLIConfig.PromptFile, repo, issue, defaultBranch)
}

// EnsureRepo clones a repository if it doesn't exist, or pulls latest changes.
func (w *Worker) EnsureRepo(ctx context.Context, cloneURL, repo string) (string, error) {
	repoDir := filepath.Join(w.CLIConfig.Workspace, repo)

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

	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "config", "user.email", "issuebot@bot").Run(); err != nil {
		return "", fmt.Errorf("git config user.email: %w", err)
	}

	if err := exec.CommandContext(ctx, "git", "-C", repoDir, "config", "user.name", "issuebot").Run(); err != nil {
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

// RunCommand executes the configured command with the prompt.
// If the command contains {prompt}, the prompt is substituted as an argument.
// Otherwise, the prompt is passed via stdin.
func (w *Worker) RunCommand(ctx context.Context, workDir, prompt string) (string, error) {
	cmdStr := w.CLIConfig.Command

	// If {prompt} placeholder is present, substitute it and don't pipe stdin.
	useStdin := !strings.Contains(cmdStr, "{prompt}")
	if !useStdin {
		cmdStr = strings.ReplaceAll(cmdStr, "{prompt}", shellescape(prompt))
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = workDir
	cmd.Stderr = os.Stderr

	if useStdin {
		cmd.Stdin = strings.NewReader(prompt)
	}

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("command exited with error: %w", err)
	}

	return strings.TrimSpace(string(out)), nil
}

// shellescape wraps a string in single quotes for safe shell interpolation.
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
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
			w.fail(ctx, key, logger, attempts, fmt.Sprintf("getwd: %v", err))
			return
		}
	} else {
		cloneURL := w.CloneURL
		if cloneURL == "" {
			cloneURL = fmt.Sprintf("https://github.com/%s.git", repo)
		}

		repoDir, err = w.EnsureRepo(ctx, cloneURL, repo)
		if err != nil {
			w.fail(ctx, key, logger, attempts, fmt.Sprintf("ensure repo: %v", err))
			return
		}
	}

	// If PR strategy, create branch.
	if strategy == "pr" {
		cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "checkout", "-b", branch)

		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			w.fail(ctx, key, logger, attempts, fmt.Sprintf("checkout branch: %v", err))
			return
		}
	}

	// Run command.
	prompt, err := w.BuildPrompt(repo, issue, "main")
	if err != nil {
		w.fail(ctx, key, logger, attempts, fmt.Sprintf("build prompt: %v", err))
		return
	}

	result, err := w.RunCommand(ctx, repoDir, prompt)
	if err != nil {
		w.fail(ctx, key, logger, attempts, fmt.Sprintf("command: %v", err))
		return
	}

	// Check for failure marker.
	if strings.Contains(result, "ISSUE_FAILED") {
		w.fail(ctx, key, logger, attempts, fmt.Sprintf("command reported failure: %s", result))
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
		if err := w.pushAndCreatePR(ctx, key, logger, repoDir, repo, branch, issue, attempts, issueCfg); err != nil {
			return
		}
	} else {
		// Direct strategy: push to current branch.
		cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "push")

		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			w.fail(ctx, key, logger, attempts, fmt.Sprintf("git push: %v", err))
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

// runPostCommand executes a post-PR shell command with PR_URL and PR_NUMBER env vars.
func (w *Worker) runPostCommand(ctx context.Context, logger *slog.Logger, postCommand, prURL string, prNumber int) {
	if postCommand == "" {
		return
	}

	cmd := exec.CommandContext(ctx, "sh", "-c", postCommand)

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("PR_URL=%s", prURL),
		fmt.Sprintf("PR_NUMBER=%d", prNumber),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		logger.Error("post-command failed", "error", err)
	}
}

func (w *Worker) pushAndCreatePR(ctx context.Context, key string, logger *slog.Logger, repoDir, repo, branch string, issue *github.Issue, attempts int, issueCfg config.IssueConfig) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoDir, "push", "-u", "origin", branch)

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		w.fail(ctx, key, logger, attempts, fmt.Sprintf("git push branch: %v", err))
		return err
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		w.fail(ctx, key, logger, attempts, fmt.Sprintf("invalid repo format: %s", repo))
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

		w.runPostCommand(ctx, logger, issueCfg.PostCommand, pr.GetHTMLURL(), pr.GetNumber())

		return nil
	}

	prTitle := fmt.Sprintf("Fix #%d: %s", issue.GetNumber(), issue.GetTitle())
	prBody := fmt.Sprintf("Resolves #%d\n\nAutomated by issuebot.", issue.GetNumber())
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
			w.fail(ctx, key, logger, attempts, fmt.Sprintf("create PR: %v", err))
			return err
		}
	}

	w.State.Set(key, state.IssueState{
		Status:      state.StatusCompleted,
		Attempts:    attempts + 1,
		LastAttempt: time.Now(),
		PRURL:       pr.GetHTMLURL(),
	})

	w.runPostCommand(ctx, logger, issueCfg.PostCommand, pr.GetHTMLURL(), pr.GetNumber())

	return nil
}

func (w *Worker) fail(ctx context.Context, key string, logger *slog.Logger, attempts int, reason string) {
	logger.Error("issue processing failed", "reason", reason)

	w.State.Set(key, state.IssueState{
		Status:      state.StatusFailed,
		Attempts:    attempts,
		LastAttempt: time.Now(),
		Error:       reason,
	})

	if err := w.State.Save(); err != nil {
		logger.Error("save state", "error", err)
	}

	if err := w.Notifier.Send(ctx, fmt.Sprintf("issuebot failed %s: %s", key, reason)); err != nil {
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
