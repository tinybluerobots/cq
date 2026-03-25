package cmd

import (
	"fmt"

	"github.com/jon/claude-afk/internal/config"
	"github.com/spf13/cobra"
)

var cfg = config.DefaultCLIConfig()

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch a GitHub repo for labeled issues and dispatch Claude",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("watch not yet implemented")
		return nil
	},
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
