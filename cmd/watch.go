package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/google/go-github/v69/github"
	"github.com/jon/claude-afk/internal/config"
	"github.com/jon/claude-afk/internal/notify"
	"github.com/jon/claude-afk/internal/poller"
	"github.com/jon/claude-afk/internal/state"
	"github.com/jon/claude-afk/internal/worker"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"
)

var cfg = config.DefaultCLIConfig()

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch GitHub repos for issues and process them with Claude",
	RunE:  runWatch,
}

func init() {
	f := watchCmd.Flags()
	f.StringVar(&cfg.Org, "org", cfg.Org, "GitHub organization")
	f.StringVar(&cfg.Repo, "repo", cfg.Repo, "GitHub repository")
	f.StringVar(&cfg.Label, "label", cfg.Label, "Issue label to watch")
	f.StringVar(&cfg.Strategy, "strategy", cfg.Strategy, "Default strategy (pr, commit, worktree)")
	f.DurationVar(&cfg.Interval, "interval", cfg.Interval, "Poll interval")
	f.IntVar(&cfg.Workers, "workers", cfg.Workers, "Max concurrent workers")
	f.IntVar(&cfg.MaxRetries, "max-retries", cfg.MaxRetries, "Max retries per issue")
	f.StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "Workspace directory for repo clones")
	f.StringVar(&cfg.LogFile, "log-file", cfg.LogFile, "Log file path")
	f.StringVar(&cfg.NtfyTopic, "ntfy-topic", cfg.NtfyTopic, "ntfy.sh topic for notifications")

	rootCmd.AddCommand(watchCmd)
}

func githubToken() string {
	if tok := os.Getenv("GITHUB_TOKEN"); tok != "" {
		return tok
	}
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		return strings.TrimSpace(string(out))
	}
	return ""
}

func runWatch(cmd *cobra.Command, args []string) error {
	// Logging
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Errorf("open log file: %w", err)
		}
		defer f.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(f, opts)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
	}

	// Notifications
	ntfyTopic := cfg.NtfyTopic
	if ntfyTopic == "" {
		ntfyTopic = os.Getenv("NTFY_TOPIC")
	}
	notifier := notify.New(ntfyTopic)

	// GitHub client
	token := githubToken()
	if token == "" {
		return fmt.Errorf("no GitHub token found (set GITHUB_TOKEN or run 'gh auth login')")
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	ghClient := github.NewClient(oauth2.NewClient(context.Background(), ts))

	// Resolve repo from current dir if needed
	if cfg.Org == "" && cfg.Repo == "" {
		out, err := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner").Output()
		if err != nil {
			return fmt.Errorf("no --org or --repo specified and not in a git repo")
		}
		cfg.Repo = strings.TrimSpace(string(out))
	}

	// State
	home, _ := os.UserHomeDir()
	stateDir := home + "/.claude-afk"
	os.MkdirAll(stateDir, 0755)
	st, err := state.Load(stateDir + "/state.json")
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}
	st.RecoverCrashed()
	st.Save()

	// Workspace
	os.MkdirAll(cfg.Workspace, 0755)

	// Poller
	p := &poller.Poller{
		Client:     ghClient,
		Org:        cfg.Org,
		SingleRepo: cfg.Repo,
		Label:      cfg.Label,
	}

	// Signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	sem := make(chan struct{}, cfg.Workers)
	var repoLocks sync.Map

	mode := "current repo"
	if cfg.Org != "" {
		mode = fmt.Sprintf("org '%s'", cfg.Org)
	} else if cfg.Repo != "" {
		mode = cfg.Repo
	}
	slog.Info("starting claude-afk",
		"mode", mode, "label", cfg.Label,
		"strategy", cfg.Strategy, "interval", cfg.Interval,
		"workers", cfg.Workers,
	)

	poll := func() {
		repos, err := p.ListRepos(ctx)
		if err != nil {
			slog.Error("list repos", "error", err)
			return
		}

		for _, repo := range repos {
			issues, err := p.ListIssues(ctx, repo)
			if err != nil {
				slog.Error("list issues", "repo", repo, "error", err)
				continue
			}

			for _, issue := range issues {
				key := fmt.Sprintf("%s#%d", repo, issue.GetNumber())
				if !st.ShouldProcess(key, cfg.MaxRetries) {
					continue
				}

				lockI, _ := repoLocks.LoadOrStore(repo, &sync.Mutex{})
				repoMu := lockI.(*sync.Mutex)
				if !repoMu.TryLock() {
					continue
				}

				repo, issue := repo, issue
				sem <- struct{}{}

				go func() {
					defer func() {
						<-sem
						repoMu.Unlock()
					}()

					w := &worker.Worker{
						Client:    ghClient,
						State:     st,
						Notifier:  notifier,
						CLIConfig: cfg,
						Workspace: cfg.Workspace,
					}
					w.ProcessIssue(ctx, repo, issue)
				}()
			}
		}
	}

	// Run immediately, then on interval
	poll()

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			return nil
		case <-ticker.C:
			poll()
		}
	}
}
