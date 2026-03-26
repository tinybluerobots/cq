package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// CLIConfig holds all CLI-level configuration.
type CLIConfig struct {
	Org        string
	Repo       string
	Label      string
	Strategy   string
	Interval   time.Duration
	Workers    int
	MaxRetries int
	Workspace  string
	Local      bool
	DryRun     bool
	Command    string
	PromptFile string
	LogFile    string
	NtfyTopic  string
}

// IssueConfig holds per-issue configuration extracted from issue body.
type IssueConfig struct {
	Strategy string `yaml:"strategy"`
	Branch   string `yaml:"branch"`
}

var issueConfigRe = regexp.MustCompile(`(?s)<!--\s*cq\s*\n(.*?)\n-->`)

// DefaultCLIConfig returns a CLIConfig with sensible defaults.
func DefaultCLIConfig() CLIConfig {
	return CLIConfig{
		Label:      "",
		Strategy:   "pr",
		Interval:   30 * time.Second,
		Workers:    5,
		MaxRetries: 3,
		Workspace:  defaultWorkspace(),
		PromptFile: defaultPromptFile(),
	}
}

// ParseIssueConfig extracts an IssueConfig from an issue body.
// It looks for a <!-- cq ... --> HTML comment block containing YAML.
func ParseIssueConfig(body string) IssueConfig {
	var cfg IssueConfig

	m := issueConfigRe.FindStringSubmatch(body)
	if len(m) < 2 {
		return cfg
	}

	if err := yaml.Unmarshal([]byte(m[1]), &cfg); err != nil {
		slog.Warn("malformed cq config in issue body", "error", err)
	}

	return cfg
}

// ResolveStrategy returns the issue-level strategy if set, otherwise the CLI-level strategy.
func ResolveStrategy(cli CLIConfig, issue IssueConfig) string {
	if issue.Strategy != "" {
		return issue.Strategy
	}

	return cli.Strategy
}

// ResolveBranch returns the issue-level branch if set, otherwise cq/issue-{N}.
func ResolveBranch(issue IssueConfig, issueNumber int) string {
	if issue.Branch != "" {
		return issue.Branch
	}

	return "cq/issue-" + strconv.Itoa(issueNumber)
}

func defaultWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cq/repos"
	}

	return filepath.Join(home, ".cq", "repos")
}

func defaultPromptFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cq/prompt.tmpl"
	}

	return filepath.Join(home, ".cq", "prompt.tmpl")
}
