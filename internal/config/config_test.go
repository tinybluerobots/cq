package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseIssueConfig_Full(t *testing.T) {
	body := `Some issue text here.

<!-- issuebot
strategy: commit
branch: my-feature
-->

More text.`

	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "commit" {
		t.Errorf("strategy: got %q, want %q", cfg.Strategy, "commit")
	}

	if cfg.Branch != "my-feature" {
		t.Errorf("branch: got %q, want %q", cfg.Branch, "my-feature")
	}
}

func TestParseIssueConfig_Empty(t *testing.T) {
	body := "Just a normal issue body with no config block."

	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "" {
		t.Errorf("strategy: got %q, want empty", cfg.Strategy)
	}

	if cfg.Branch != "" {
		t.Errorf("branch: got %q, want empty", cfg.Branch)
	}
}

func TestParseIssueConfig_PartialFields(t *testing.T) {
	body := `<!-- issuebot
strategy: worktree
-->`

	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "worktree" {
		t.Errorf("strategy: got %q, want %q", cfg.Strategy, "worktree")
	}

	if cfg.Branch != "" {
		t.Errorf("branch: got %q, want empty", cfg.Branch)
	}
}

func TestParseIssueConfig_UnknownKeysIgnored(t *testing.T) {
	body := `<!-- issuebot
strategy: pr
branch: fix-123
unknown_key: some_value
another: thing
-->`

	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "pr" {
		t.Errorf("strategy: got %q, want %q", cfg.Strategy, "pr")
	}

	if cfg.Branch != "fix-123" {
		t.Errorf("branch: got %q, want %q", cfg.Branch, "fix-123")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultCLIConfig()

	tests := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"strategy", cfg.Strategy, "commit"},
		{"interval", cfg.Interval, 30 * time.Second},
		{"workers", cfg.Workers, 5},
		{"max-retries", cfg.MaxRetries, 3},
		{"label", cfg.Label, ""},
		{"workspace-has-issuebot", strings.Contains(cfg.Workspace, ".issuebot/repos"), true},
		{"workspace-no-tilde", strings.HasPrefix(cfg.Workspace, "~"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestParseIssueConfig_MalformedYAML(t *testing.T) {
	body := `<!-- issuebot
strategy: [broken
-->`

	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "" {
		t.Errorf("expected empty strategy on malformed YAML, got %q", cfg.Strategy)
	}
}

func TestDefaultConfig_WorkspaceUsesHomeDir(t *testing.T) {
	cfg := DefaultCLIConfig()
	home, _ := os.UserHomeDir()

	expected := filepath.Join(home, ".issuebot", "repos")
	if cfg.Workspace != expected {
		t.Errorf("workspace: got %q, want %q", cfg.Workspace, expected)
	}
}

func TestResolveStrategy_IssueOverrides(t *testing.T) {
	cli := DefaultCLIConfig()

	issue := IssueConfig{Strategy: "commit"}
	if got := ResolveStrategy(cli, issue); got != "commit" {
		t.Errorf("got %q, want %q", got, "commit")
	}
}

func TestResolveStrategy_FallbackToCLI(t *testing.T) {
	cli := DefaultCLIConfig()

	issue := IssueConfig{}
	if got := ResolveStrategy(cli, issue); got != "commit" {
		t.Errorf("got %q, want %q", got, "commit")
	}
}

func TestResolveBranch_IssueOverrides(t *testing.T) {
	issue := IssueConfig{Branch: "custom-branch"}
	if got := ResolveBranch(issue, 42); got != "custom-branch" {
		t.Errorf("got %q, want %q", got, "custom-branch")
	}
}

func TestResolveBranch_Default(t *testing.T) {
	issue := IssueConfig{}
	if got := ResolveBranch(issue, 42); got != "issuebot/issue-42" {
		t.Errorf("got %q, want %q", got, "issuebot/issue-42")
	}
}

func TestParseIssueConfig_PostCommand(t *testing.T) {
	body := "<!-- issuebot\npost-command: gh pr comment $PR_NUMBER -b 'ready'\n-->"
	cfg := ParseIssueConfig(body)

	if cfg.PostCommand != "gh pr comment $PR_NUMBER -b 'ready'" {
		t.Errorf("PostCommand = %q", cfg.PostCommand)
	}
}

func TestParseIssueConfig_PostCommandEmpty(t *testing.T) {
	body := "<!-- issuebot\nstrategy: pr\n-->"
	cfg := ParseIssueConfig(body)

	if cfg.PostCommand != "" {
		t.Errorf("PostCommand = %q, want empty", cfg.PostCommand)
	}
}
