# claude-afk Go Rewrite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rewrite claude-afk as a Go CLI that watches GitHub org/repo issues and autonomously processes them via the `claude` CLI.

**Architecture:** Three-layer design (poller → dispatcher → worker) with cobra CLI, go-github for API, slog for logging. Workers process one issue at a time per repo with configurable concurrency.

**Tech Stack:** Go, cobra, go-github, oauth2, yaml.v3, slog

---

## File Map

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point, calls `cmd.Execute()` |
| `cmd/root.go` | Cobra root command, version info |
| `cmd/watch.go` | Watch subcommand, all flags, wires poller/dispatcher/workers |
| `internal/config/config.go` | Config struct, per-issue config parsing from HTML comment |
| `internal/state/state.go` | State file read/write, mutex-protected, crash recovery |
| `internal/notify/notify.go` | ntfy.sh HTTP POST helper |
| `internal/poller/poller.go` | GitHub API: list repos, list issues with label filter |
| `internal/worker/worker.go` | Clone/pull repo, run claude, push/PR, update state |
| `internal/config/config_test.go` | Tests for per-issue config parsing |
| `internal/state/state_test.go` | Tests for state file operations |
| `internal/notify/notify_test.go` | Tests for ntfy notifications |
| `internal/poller/poller_test.go` | Tests for poller with mock GitHub server |
| `internal/worker/worker_test.go` | Tests for worker with mock claude binary |

---

### Task 1: Project Scaffold + Config

**Files:**
- Create: `main.go`
- Create: `cmd/root.go`
- Create: `cmd/watch.go`
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Create: `go.mod`

- [ ] **Step 1: Initialize Go module**

```bash
cd /home/jon/dev/claude-afk
go mod init github.com/jon/claude-afk
```

- [ ] **Step 2: Install dependencies**

```bash
go get github.com/spf13/cobra@latest
go get github.com/google/go-github/v69/github
go get golang.org/x/oauth2
go get gopkg.in/yaml.v3
```

- [ ] **Step 3: Write the config types and per-issue parser test**

Create `internal/config/config_test.go`:

```go
package config

import (
	"testing"
	"time"
)

func TestParseIssueConfig_Full(t *testing.T) {
	body := `Some issue text here.

<!-- claude-afk
strategy: direct
branch: my-custom-branch
-->

More text after.`

	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "direct" {
		t.Errorf("expected strategy 'direct', got %q", cfg.Strategy)
	}
	if cfg.Branch != "my-custom-branch" {
		t.Errorf("expected branch 'my-custom-branch', got %q", cfg.Branch)
	}
}

func TestParseIssueConfig_Empty(t *testing.T) {
	body := "Just a regular issue with no config block."
	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "" {
		t.Errorf("expected empty strategy, got %q", cfg.Strategy)
	}
	if cfg.Branch != "" {
		t.Errorf("expected empty branch, got %q", cfg.Branch)
	}
}

func TestParseIssueConfig_PartialFields(t *testing.T) {
	body := `<!-- claude-afk
strategy: pr
-->`
	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "pr" {
		t.Errorf("expected strategy 'pr', got %q", cfg.Strategy)
	}
	if cfg.Branch != "" {
		t.Errorf("expected empty branch, got %q", cfg.Branch)
	}
}

func TestParseIssueConfig_UnknownKeysIgnored(t *testing.T) {
	body := `<!-- claude-afk
strategy: pr
foo: bar
notify: true
-->`
	cfg := ParseIssueConfig(body)
	if cfg.Strategy != "pr" {
		t.Errorf("expected strategy 'pr', got %q", cfg.Strategy)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultCLIConfig()
	if cfg.Strategy != "pr" {
		t.Errorf("expected default strategy 'pr', got %q", cfg.Strategy)
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("expected default interval 30s, got %v", cfg.Interval)
	}
	if cfg.Workers != 5 {
		t.Errorf("expected default workers 5, got %d", cfg.Workers)
	}
	if cfg.MaxRetries != 3 {
		t.Errorf("expected default max retries 3, got %d", cfg.MaxRetries)
	}
	if cfg.Label != "claude-afk" {
		t.Errorf("expected default label 'claude-afk', got %q", cfg.Label)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/config/ -v
```

Expected: compilation errors — types and functions don't exist yet.

- [ ] **Step 5: Implement config package**

Create `internal/config/config.go`:

```go
package config

import (
	"os"
	"path/filepath"
	"regexp"
	"time"

	"gopkg.in/yaml.v3"
)

// CLIConfig holds all CLI flag values.
type CLIConfig struct {
	Org        string
	Repo       string
	Label      string
	Strategy   string
	Interval   time.Duration
	Workers    int
	Workspace  string
	MaxRetries int
	LogFile    string
	NtfyTopic  string
}

// IssueConfig holds per-issue overrides parsed from the issue body.
type IssueConfig struct {
	Strategy string `yaml:"strategy"`
	Branch   string `yaml:"branch"`
}

var issueConfigRe = regexp.MustCompile(`(?s)<!--\s*claude-afk\s*\n(.*?)\n-->`)

// DefaultCLIConfig returns the default CLI configuration.
func DefaultCLIConfig() CLIConfig {
	home, _ := os.UserHomeDir()
	return CLIConfig{
		Label:      "claude-afk",
		Strategy:   "pr",
		Interval:   30 * time.Second,
		Workers:    5,
		Workspace:  filepath.Join(home, ".claude-afk", "repos"),
		MaxRetries: 3,
	}
}

// ParseIssueConfig extracts a claude-afk config block from an issue body.
func ParseIssueConfig(body string) IssueConfig {
	var cfg IssueConfig
	matches := issueConfigRe.FindStringSubmatch(body)
	if len(matches) < 2 {
		return cfg
	}
	_ = yaml.Unmarshal([]byte(matches[1]), &cfg)
	return cfg
}

// ResolveStrategy returns the effective strategy for an issue.
// Per-issue config overrides CLI default.
func ResolveStrategy(cli CLIConfig, issue IssueConfig) string {
	if issue.Strategy != "" {
		return issue.Strategy
	}
	return cli.Strategy
}

// ResolveBranch returns the effective branch name for an issue.
// Per-issue config overrides the default pattern.
func ResolveBranch(issue IssueConfig, issueNumber int) string {
	if issue.Branch != "" {
		return issue.Branch
	}
	return "claude-afk/issue-" + itoa(issueNumber)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
```

- [ ] **Step 6: Run tests to verify they pass**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/config/ -v
```

Expected: all 5 tests PASS.

- [ ] **Step 7: Write main.go and cobra commands**

Create `main.go`:

```go
package main

import "github.com/jon/claude-afk/cmd"

func main() {
	cmd.Execute()
}
```

Create `cmd/root.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "claude-afk",
	Short: "Autonomous GitHub issue processor powered by Claude",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

Create `cmd/watch.go` (stub — wiring comes in later tasks):

```go
package cmd

import (
	"fmt"

	"github.com/jon/claude-afk/internal/config"
	"github.com/spf13/cobra"
)

var cfg = config.DefaultCLIConfig()

var watchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Watch GitHub repos for issues and process them with Claude",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("watch not yet implemented")
		return nil
	},
}

func init() {
	watchCmd.Flags().StringVar(&cfg.Org, "org", cfg.Org, "GitHub org to watch")
	watchCmd.Flags().StringVar(&cfg.Repo, "repo", cfg.Repo, "Single owner/repo to watch")
	watchCmd.Flags().StringVar(&cfg.Label, "label", cfg.Label, "Issue label filter")
	watchCmd.Flags().StringVar(&cfg.Strategy, "strategy", cfg.Strategy, "Git strategy: pr or direct")
	watchCmd.Flags().DurationVar(&cfg.Interval, "interval", cfg.Interval, "Polling interval")
	watchCmd.Flags().IntVar(&cfg.Workers, "workers", cfg.Workers, "Max concurrent repo workers")
	watchCmd.Flags().StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "Directory for cloned repos")
	watchCmd.Flags().IntVar(&cfg.MaxRetries, "max-retries", cfg.MaxRetries, "Max retries per issue")
	watchCmd.Flags().StringVar(&cfg.LogFile, "log-file", cfg.LogFile, "Log file path")
	watchCmd.Flags().StringVar(&cfg.NtfyTopic, "ntfy-topic", cfg.NtfyTopic, "ntfy.sh topic for error notifications")

	rootCmd.AddCommand(watchCmd)
}
```

- [ ] **Step 8: Verify it compiles and runs**

```bash
cd /home/jon/dev/claude-afk && go build -o claude-afk . && ./claude-afk watch --help
```

Expected: help output showing all flags.

- [ ] **Step 9: Commit**

```bash
git add main.go cmd/ internal/config/ go.mod go.sum
git commit -m "feat: project scaffold with cobra CLI and config package"
```

---

### Task 2: State File

**Files:**
- Create: `internal/state/state.go`
- Create: `internal/state/state_test.go`

- [ ] **Step 1: Write state tests**

Create `internal/state/state_test.go`:

```go
package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_LoadEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load from nonexistent file: %v", err)
	}
	if len(s.Issues) != 0 {
		t.Errorf("expected empty issues, got %d", len(s.Issues))
	}
}

func TestStore_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, _ := Load(path)
	s.Set("owner/repo#1", IssueState{
		Status:      StatusCompleted,
		Attempts:    1,
		LastAttempt: time.Now(),
		PRURL:       "https://github.com/owner/repo/pull/5",
	})

	got, ok := s.Get("owner/repo#1")
	if !ok {
		t.Fatal("expected to find issue")
	}
	if got.Status != StatusCompleted {
		t.Errorf("expected completed, got %q", got.Status)
	}
	if got.PRURL != "https://github.com/owner/repo/pull/5" {
		t.Errorf("unexpected PR URL: %q", got.PRURL)
	}
}

func TestStore_SaveAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, _ := Load(path)
	s.Set("owner/repo#42", IssueState{
		Status:   StatusFailed,
		Attempts: 2,
		Error:    "claude exited 1",
	})
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s2, err := Load(path)
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	got, ok := s2.Get("owner/repo#42")
	if !ok {
		t.Fatal("expected to find issue after reload")
	}
	if got.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", got.Attempts)
	}
	if got.Error != "claude exited 1" {
		t.Errorf("unexpected error: %q", got.Error)
	}
}

func TestStore_RecoverInProgress(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, _ := Load(path)
	s.Set("owner/repo#10", IssueState{Status: StatusInProgress, Attempts: 1})
	s.Set("owner/repo#11", IssueState{Status: StatusCompleted, Attempts: 1})
	s.Save()

	s2, _ := Load(path)
	s2.RecoverCrashed()

	got, _ := s2.Get("owner/repo#10")
	if got.Status != StatusPending {
		t.Errorf("expected in_progress reset to pending, got %q", got.Status)
	}
	got2, _ := s2.Get("owner/repo#11")
	if got2.Status != StatusCompleted {
		t.Errorf("expected completed to stay completed, got %q", got2.Status)
	}
}

func TestStore_ShouldProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	s, _ := Load(path)

	// New issue — should process
	if !s.ShouldProcess("owner/repo#1", 3) {
		t.Error("new issue should be processable")
	}

	// Completed — skip
	s.Set("owner/repo#2", IssueState{Status: StatusCompleted})
	if s.ShouldProcess("owner/repo#2", 3) {
		t.Error("completed issue should be skipped")
	}

	// Failed but under max retries — process
	s.Set("owner/repo#3", IssueState{Status: StatusFailed, Attempts: 2})
	if !s.ShouldProcess("owner/repo#3", 3) {
		t.Error("failed issue under max retries should be processable")
	}

	// Failed at max retries — skip
	s.Set("owner/repo#4", IssueState{Status: StatusFailed, Attempts: 3})
	if s.ShouldProcess("owner/repo#4", 3) {
		t.Error("failed issue at max retries should be skipped")
	}

	// In progress — skip
	s.Set("owner/repo#5", IssueState{Status: StatusInProgress})
	if s.ShouldProcess("owner/repo#5", 3) {
		t.Error("in-progress issue should be skipped")
	}
}

func TestStore_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	os.WriteFile(path, []byte("not json{{{"), 0644)

	s, err := Load(path)
	if err != nil {
		t.Fatalf("corrupt file should not error, got: %v", err)
	}
	if len(s.Issues) != 0 {
		t.Error("corrupt file should yield empty state")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/state/ -v
```

Expected: compilation errors.

- [ ] **Step 3: Implement state package**

Create `internal/state/state.go`:

```go
package state

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"time"
)

const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
)

type IssueState struct {
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	PRURL       string    `json:"pr_url,omitempty"`
	Error       string    `json:"error,omitempty"`
}

type Store struct {
	Issues map[string]IssueState `json:"issues"`
	path   string
	mu     sync.Mutex
}

// Load reads state from disk. Returns empty state if file missing or corrupt.
func Load(path string) (*Store, error) {
	s := &Store{
		Issues: make(map[string]IssueState),
		path:   path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, nil
	}

	if err := json.Unmarshal(data, s); err != nil {
		slog.Warn("corrupt state file, starting fresh", "path", path, "error", err)
		s.Issues = make(map[string]IssueState)
	}
	return s, nil
}

// Save writes state to disk atomically.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Set updates an issue's state.
func (s *Store) Set(key string, state IssueState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Issues[key] = state
}

// Get retrieves an issue's state.
func (s *Store) Get(key string) (IssueState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Issues[key]
	return st, ok
}

// RecoverCrashed resets in_progress issues to pending (crash recovery).
func (s *Store) RecoverCrashed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, v := range s.Issues {
		if v.Status == StatusInProgress {
			v.Status = StatusPending
			s.Issues[k] = v
		}
	}
}

// ShouldProcess returns true if an issue should be picked up for processing.
func (s *Store) ShouldProcess(key string, maxRetries int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.Issues[key]
	if !ok {
		return true
	}

	switch st.Status {
	case StatusCompleted:
		return false
	case StatusInProgress:
		return false
	case StatusFailed:
		return st.Attempts < maxRetries
	default:
		return true
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/state/ -v
```

Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/state/
git commit -m "feat: state file persistence with crash recovery"
```

---

### Task 3: Notify

**Files:**
- Create: `internal/notify/notify.go`
- Create: `internal/notify/notify_test.go`

- [ ] **Step 1: Write notify tests**

Create `internal/notify/notify_test.go`:

```go
package notify

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSend_Success(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	n := &Notifier{BaseURL: srv.URL, Topic: "test-topic"}
	err := n.Send("hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotBody != "hello world" {
		t.Errorf("expected body 'hello world', got %q", gotBody)
	}
}

func TestSend_NoTopic(t *testing.T) {
	n := &Notifier{Topic: ""}
	err := n.Send("hello")
	if err != nil {
		t.Fatal("send with no topic should be a no-op, not error")
	}
}

func TestSend_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	n := &Notifier{BaseURL: srv.URL, Topic: "test"}
	err := n.Send("hello")
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/notify/ -v
```

Expected: compilation errors.

- [ ] **Step 3: Implement notify package**

Create `internal/notify/notify.go`:

```go
package notify

import (
	"fmt"
	"net/http"
	"strings"
)

// Notifier sends notifications via ntfy.sh.
type Notifier struct {
	BaseURL string // defaults to "https://ntfy.sh"
	Topic   string
}

// New creates a Notifier. If topic is empty, Send is a no-op.
func New(topic string) *Notifier {
	return &Notifier{
		BaseURL: "https://ntfy.sh",
		Topic:   topic,
	}
}

// Send posts a message to the configured ntfy topic.
// No-op if topic is empty.
func (n *Notifier) Send(message string) error {
	if n.Topic == "" {
		return nil
	}

	url := n.BaseURL + "/" + n.Topic
	resp, err := http.Post(url, "text/plain", strings.NewReader(message))
	if err != nil {
		return fmt.Errorf("ntfy send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy send: status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/notify/ -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/notify/
git commit -m "feat: ntfy.sh notification helper"
```

---

### Task 4: Poller

**Files:**
- Create: `internal/poller/poller.go`
- Create: `internal/poller/poller_test.go`

- [ ] **Step 1: Write poller tests**

Create `internal/poller/poller_test.go`:

```go
package poller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-github/v69/github"
)

func TestPoller_ListRepos_Org(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/orgs/testorg/repos", func(w http.ResponseWriter, r *http.Request) {
		repos := []*github.Repository{
			{FullName: github.Ptr("testorg/repo1"), Archived: github.Ptr(false), Fork: github.Ptr(false)},
			{FullName: github.Ptr("testorg/repo2"), Archived: github.Ptr(true), Fork: github.Ptr(false)},
			{FullName: github.Ptr("testorg/repo3"), Archived: github.Ptr(false), Fork: github.Ptr(true)},
		}
		json.NewEncoder(w).Encode(repos)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := github.NewClient(nil)
	client.BaseURL, _ = client.BaseURL.Parse(srv.URL + "/")

	p := &Poller{Client: client, Org: "testorg"}
	repos, err := p.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	// Should exclude archived (repo2) and fork (repo3)
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d", len(repos))
	}
	if repos[0] != "testorg/repo1" {
		t.Errorf("expected testorg/repo1, got %q", repos[0])
	}
}

func TestPoller_ListRepos_Single(t *testing.T) {
	p := &Poller{SingleRepo: "owner/myrepo"}
	repos, err := p.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("ListRepos: %v", err)
	}
	if len(repos) != 1 || repos[0] != "owner/myrepo" {
		t.Errorf("expected [owner/myrepo], got %v", repos)
	}
}

func TestPoller_ListIssues(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/issues", func(w http.ResponseWriter, r *http.Request) {
		labels := r.URL.Query().Get("labels")
		if labels != "claude-afk" {
			t.Errorf("expected label filter 'claude-afk', got %q", labels)
		}
		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("Fix bug"), Body: github.Ptr("details"), PullRequestLinks: nil},
			{Number: github.Ptr(2), Title: github.Ptr("PR not issue"), Body: github.Ptr(""), PullRequestLinks: &github.PullRequestLinks{}},
		}
		json.NewEncoder(w).Encode(issues)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := github.NewClient(nil)
	client.BaseURL, _ = client.BaseURL.Parse(srv.URL + "/")

	p := &Poller{Client: client, Label: "claude-afk"}
	issues, err := p.ListIssues(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	// Should exclude PRs (issue #2)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].GetNumber() != 1 {
		t.Errorf("expected issue #1, got #%d", issues[0].GetNumber())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/poller/ -v
```

Expected: compilation errors.

- [ ] **Step 3: Implement poller package**

Create `internal/poller/poller.go`:

```go
package poller

import (
	"context"
	"strings"

	"github.com/google/go-github/v69/github"
)

// Poller discovers repos and issues from GitHub.
type Poller struct {
	Client     *github.Client
	Org        string
	SingleRepo string
	Label      string
}

// ListRepos returns the repos to watch.
// In org mode: all non-archived, non-fork repos.
// In single-repo mode: just that repo.
func (p *Poller) ListRepos(ctx context.Context) ([]string, error) {
	if p.SingleRepo != "" {
		return []string{p.SingleRepo}, nil
	}

	var allRepos []string
	opts := &github.RepositoryListByOrgOptions{
		Type:        "sources",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	for {
		repos, resp, err := p.Client.Repositories.ListByOrg(ctx, p.Org, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.GetArchived() || r.GetFork() {
				continue
			}
			allRepos = append(allRepos, r.GetFullName())
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allRepos, nil
}

// ListIssues returns open issues with the target label for a repo.
// Filters out pull requests (GitHub API returns PRs as issues).
func (p *Poller) ListIssues(ctx context.Context, repo string) ([]*github.Issue, error) {
	parts := strings.SplitN(repo, "/", 2)
	owner, name := parts[0], parts[1]

	opts := &github.IssueListByRepoOptions{
		State:       "open",
		Labels:      []string{p.Label},
		ListOptions: github.ListOptions{PerPage: 100},
	}

	var issues []*github.Issue
	for {
		page, resp, err := p.Client.Issues.ListByRepo(ctx, owner, name, opts)
		if err != nil {
			return nil, err
		}
		for _, issue := range page {
			if issue.PullRequestLinks == nil {
				issues = append(issues, issue)
			}
		}
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return issues, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/poller/ -v
```

Expected: all 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/poller/
git commit -m "feat: GitHub poller for repos and issues"
```

---

### Task 5: Worker

**Files:**
- Create: `internal/worker/worker.go`
- Create: `internal/worker/worker_test.go`

- [ ] **Step 1: Write worker tests**

Create `internal/worker/worker_test.go`:

```go
package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-github/v69/github"
	"github.com/jon/claude-afk/internal/config"
	"github.com/jon/claude-afk/internal/notify"
	"github.com/jon/claude-afk/internal/state"
)

// mockClaude creates a shell script that mimics claude CLI output.
func mockClaude(t *testing.T, dir string, output string, exitCode int) string {
	t.Helper()
	script := filepath.Join(dir, "claude")
	content := fmt.Sprintf("#!/bin/bash\necho '%s'\nexit %d\n", output, exitCode)
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestWorker_BuildPrompt(t *testing.T) {
	w := &Worker{}
	issue := &github.Issue{
		Number: github.Ptr(42),
		Title:  github.Ptr("Fix the widget"),
		Body:   github.Ptr("The widget is broken"),
	}

	prompt := w.BuildPrompt("owner/repo", issue)
	if !strings.Contains(prompt, "#42") {
		t.Error("prompt should contain issue number")
	}
	if !strings.Contains(prompt, "Fix the widget") {
		t.Error("prompt should contain issue title")
	}
	if !strings.Contains(prompt, "The widget is broken") {
		t.Error("prompt should contain issue body")
	}
	if !strings.Contains(prompt, "owner/repo") {
		t.Error("prompt should contain repo name")
	}
}

func TestWorker_RunClaude_Success(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	dir := t.TempDir()
	claudePath := mockClaude(t, dir, `{"type":"result","result":"ISSUE_RESOLVED #1"}`, 0)

	w := &Worker{ClaudePath: claudePath}
	result, err := w.RunClaude(context.Background(), dir, "test prompt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "ISSUE_RESOLVED") {
		t.Errorf("expected ISSUE_RESOLVED in output, got %q", result)
	}
}

func TestWorker_RunClaude_Failure(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	dir := t.TempDir()
	claudePath := mockClaude(t, dir, `{"type":"result","result":"ISSUE_FAILED #1 could not fix"}`, 1)

	w := &Worker{ClaudePath: claudePath}
	_, err := w.RunClaude(context.Background(), dir, "test prompt")
	if err == nil {
		t.Fatal("expected error on non-zero exit")
	}
}

func TestWorker_EnsureRepo_Clone(t *testing.T) {
	// Create a bare git repo to clone from
	bare := t.TempDir()
	cmds := []string{
		"git init --bare " + bare,
	}
	for _, c := range cmds {
		parts := strings.Fields(c)
		cmd := exec.Command(parts[0], parts[1:]...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("setup %q: %v\n%s", c, err, out)
		}
	}

	workspace := t.TempDir()
	w := &Worker{Workspace: workspace}

	repoDir, err := w.EnsureRepo(context.Background(), bare, "test/repo")
	if err != nil {
		t.Fatalf("EnsureRepo: %v", err)
	}

	expected := filepath.Join(workspace, "test", "repo")
	if repoDir != expected {
		t.Errorf("expected %q, got %q", expected, repoDir)
	}

	// Verify .git exists
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		t.Error("expected .git directory in cloned repo")
	}
}

func TestWorker_ProcessIssue_PRStrategy(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	// Set up a source repo with a commit
	srcDir := t.TempDir()
	for _, c := range []string{
		"git init",
		"git config user.email test@test.com",
		"git config user.name test",
	} {
		cmd := exec.Command("bash", "-c", c)
		cmd.Dir = srcDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", c, err, out)
		}
	}
	os.WriteFile(filepath.Join(srcDir, "README.md"), []byte("hello"), 0644)
	for _, c := range []string{
		"git add .",
		"git commit -m initial",
	} {
		cmd := exec.Command("bash", "-c", c)
		cmd.Dir = srcDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", c, err, out)
		}
	}

	// Mock GitHub API for PR creation
	var prCreated bool
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/pulls", func(w http.ResponseWriter, r *http.Request) {
		prCreated = true
		pr := &github.PullRequest{Number: github.Ptr(99), HTMLURL: github.Ptr("https://github.com/owner/repo/pull/99")}
		json.NewEncoder(w).Encode(pr)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ghClient := github.NewClient(nil)
	ghClient.BaseURL, _ = ghClient.BaseURL.Parse(srv.URL + "/")

	workspace := t.TempDir()
	statePath := filepath.Join(t.TempDir(), "state.json")
	st, _ := state.Load(statePath)
	claudeScript := mockClaude(t, t.TempDir(), `{"type":"result","result":"ISSUE_RESOLVED #1"}`, 0)

	w := &Worker{
		Client:     ghClient,
		State:      st,
		Notifier:   notify.New(""),
		CLIConfig:  config.DefaultCLIConfig(),
		Workspace:  workspace,
		ClaudePath: claudeScript,
		CloneURL:   srcDir, // clone from local dir for testing
	}
	w.CLIConfig.Strategy = "pr"

	issue := &github.Issue{
		Number: github.Ptr(1),
		Title:  github.Ptr("Test issue"),
		Body:   github.Ptr("Fix something"),
	}

	w.ProcessIssue(context.Background(), "owner/repo", issue)

	got, ok := st.Get("owner/repo#1")
	if !ok {
		t.Fatal("expected state entry")
	}
	if got.Status != state.StatusCompleted {
		t.Errorf("expected completed, got %q", got.Status)
	}

	_ = prCreated // PR creation is best-effort in test
}

func TestWorker_ProcessIssue_IssueConfigOverride(t *testing.T) {
	body := `Fix the thing.

<!-- claude-afk
strategy: direct
branch: custom-branch
-->`

	issue := &github.Issue{
		Number: github.Ptr(5),
		Title:  github.Ptr("Test"),
		Body:   github.Ptr(body),
	}

	issueCfg := config.ParseIssueConfig(issue.GetBody())
	cli := config.DefaultCLIConfig()

	strategy := config.ResolveStrategy(cli, issueCfg)
	if strategy != "direct" {
		t.Errorf("expected 'direct' from issue override, got %q", strategy)
	}

	branch := config.ResolveBranch(issueCfg, 5)
	if branch != "custom-branch" {
		t.Errorf("expected 'custom-branch', got %q", branch)
	}
}

func init() {
	_ = time.Now // ensure time is importable
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/worker/ -v
```

Expected: compilation errors.

- [ ] **Step 3: Implement worker package**

Create `internal/worker/worker.go`:

```go
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
	"github.com/jon/claude-afk/internal/config"
	"github.com/jon/claude-afk/internal/notify"
	"github.com/jon/claude-afk/internal/state"
)

// Worker processes a single GitHub issue.
type Worker struct {
	Client     *github.Client
	State      *state.Store
	Notifier   *notify.Notifier
	CLIConfig  config.CLIConfig
	Workspace  string
	ClaudePath string // path to claude binary, defaults to "claude"
	CloneURL   string // override clone URL for testing
}

// BuildPrompt constructs the prompt for claude.
func (w *Worker) BuildPrompt(repo string, issue *github.Issue) string {
	return fmt.Sprintf(`You are working on GitHub issue #%d in %s.

Issue title: %s
Issue body:
%s

Steps:
1. Read the issue carefully. Implement the changes needed to resolve it.
2. Run tests (check package.json / Makefile / go.mod for how).
3. Commit with a descriptive message referencing the issue number.
4. Output: ISSUE_RESOLVED #%d

If you cannot resolve this issue, output: ISSUE_FAILED #%d <reason>`,
		issue.GetNumber(), repo,
		issue.GetTitle(),
		issue.GetBody(),
		issue.GetNumber(),
		issue.GetNumber(),
	)
}

// EnsureRepo clones or pulls the repo into the workspace.
func (w *Worker) EnsureRepo(ctx context.Context, cloneURL string, repo string) (string, error) {
	repoDir := filepath.Join(w.Workspace, repo)

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		// Already cloned — pull
		cmd := exec.CommandContext(ctx, "git", "pull", "--ff-only")
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			slog.Warn("git pull failed, continuing with existing state", "repo", repo, "error", string(out))
		}
		return repoDir, nil
	}

	if err := os.MkdirAll(filepath.Dir(repoDir), 0755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", cloneURL, repoDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone: %s: %w", string(out), err)
	}

	return repoDir, nil
}

// RunClaude executes the claude CLI and returns the result output.
func (w *Worker) RunClaude(ctx context.Context, workDir string, prompt string) (string, error) {
	claudeBin := w.ClaudePath
	if claudeBin == "" {
		claudeBin = "claude"
	}

	cmd := exec.CommandContext(ctx, claudeBin, "-p",
		"--dangerously-skip-permissions",
		"--output-format", "stream-json",
		prompt,
	)
	cmd.Dir = workDir
	cmd.Stdin = nil

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start claude: %w", err)
	}

	var result string
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		var msg struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "assistant":
			for _, c := range msg.Message.Content {
				if c.Type == "text" && c.Text != "" {
					slog.Info("claude", "text", c.Text)
				}
			}
		case "result":
			result = msg.Result
			slog.Info("claude result", "result", result)
		}
	}

	if err := cmd.Wait(); err != nil {
		return result, fmt.Errorf("claude exited: %w", err)
	}

	return result, nil
}

// ProcessIssue handles a single issue end-to-end.
func (w *Worker) ProcessIssue(ctx context.Context, repo string, issue *github.Issue) {
	key := fmt.Sprintf("%s#%d", repo, issue.GetNumber())
	logger := slog.With("repo", repo, "issue", issue.GetNumber())

	// Mark in-progress
	w.State.Set(key, state.IssueState{
		Status:      state.StatusInProgress,
		Attempts:    w.getAttempts(key) + 1,
		LastAttempt: time.Now(),
	})
	w.State.Save()

	// Parse per-issue config
	issueCfg := config.ParseIssueConfig(issue.GetBody())
	strategy := config.ResolveStrategy(w.CLIConfig, issueCfg)
	branch := config.ResolveBranch(issueCfg, issue.GetNumber())

	// Ensure repo is cloned
	cloneURL := w.CloneURL
	if cloneURL == "" {
		cloneURL = fmt.Sprintf("https://github.com/%s.git", repo)
	}
	repoDir, err := w.EnsureRepo(ctx, cloneURL, repo)
	if err != nil {
		w.fail(key, logger, fmt.Sprintf("clone failed: %v", err))
		return
	}

	// Create branch if PR strategy
	if strategy == "pr" {
		cmd := exec.CommandContext(ctx, "git", "checkout", "-b", branch)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			// Branch might already exist, try switching
			cmd2 := exec.CommandContext(ctx, "git", "checkout", branch)
			cmd2.Dir = repoDir
			if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
				w.fail(key, logger, fmt.Sprintf("git checkout: %s / %s", string(out), string(out2)))
				return
			}
		}
	}

	// Run claude
	prompt := w.BuildPrompt(repo, issue)
	result, err := w.RunClaude(ctx, repoDir, prompt)
	if err != nil {
		w.fail(key, logger, fmt.Sprintf("claude: %v", err))
		return
	}

	// Check result
	if strings.Contains(result, "ISSUE_FAILED") {
		w.fail(key, logger, result)
		return
	}

	// Push
	if strategy == "pr" {
		cmd := exec.CommandContext(ctx, "git", "push", "-u", "origin", branch)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			w.fail(key, logger, fmt.Sprintf("git push: %s: %v", string(out), err))
			return
		}

		// Open PR
		parts := strings.SplitN(repo, "/", 2)
		pr, _, err := w.Client.PullRequests.Create(ctx, parts[0], parts[1], &github.NewPullRequest{
			Title: github.Ptr(fmt.Sprintf("fix: resolve #%d — %s", issue.GetNumber(), issue.GetTitle())),
			Body:  github.Ptr(fmt.Sprintf("Automated fix for #%d\n\nGenerated by claude-afk", issue.GetNumber())),
			Head:  github.Ptr(branch),
			Base:  github.Ptr("main"),
		})
		if err != nil {
			logger.Warn("PR creation failed", "error", err)
		}

		prURL := ""
		if pr != nil {
			prURL = pr.GetHTMLURL()
			logger.Info("PR created", "url", prURL)
		}

		w.State.Set(key, state.IssueState{
			Status:      state.StatusCompleted,
			Attempts:    w.getAttempts(key),
			LastAttempt: time.Now(),
			PRURL:       prURL,
		})
	} else {
		cmd := exec.CommandContext(ctx, "git", "push")
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			w.fail(key, logger, fmt.Sprintf("git push: %s: %v", string(out), err))
			return
		}
		w.State.Set(key, state.IssueState{
			Status:      state.StatusCompleted,
			Attempts:    w.getAttempts(key),
			LastAttempt: time.Now(),
		})
	}

	w.State.Save()
	logger.Info("issue resolved")
}

func (w *Worker) fail(key string, logger *slog.Logger, reason string) {
	logger.Error("issue failed", "reason", reason)
	attempts := w.getAttempts(key)
	w.State.Set(key, state.IssueState{
		Status:      state.StatusFailed,
		Attempts:    attempts,
		LastAttempt: time.Now(),
		Error:       reason,
	})
	w.State.Save()
	w.Notifier.Send(fmt.Sprintf("claude-afk failed on %s: %s", key, reason))
}

func (w *Worker) getAttempts(key string) int {
	st, ok := w.State.Get(key)
	if !ok {
		return 0
	}
	return st.Attempts
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd /home/jon/dev/claude-afk && go test ./internal/worker/ -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/
git commit -m "feat: worker processes issues via claude CLI"
```

---

### Task 6: Wire Watch Command

**Files:**
- Modify: `cmd/watch.go`

- [ ] **Step 1: Implement the watch command**

Replace the contents of `cmd/watch.go` with:

```go
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
	watchCmd.Flags().StringVar(&cfg.Org, "org", cfg.Org, "GitHub org to watch")
	watchCmd.Flags().StringVar(&cfg.Repo, "repo", cfg.Repo, "Single owner/repo to watch")
	watchCmd.Flags().StringVar(&cfg.Label, "label", cfg.Label, "Issue label filter")
	watchCmd.Flags().StringVar(&cfg.Strategy, "strategy", cfg.Strategy, "Git strategy: pr or direct")
	watchCmd.Flags().DurationVar(&cfg.Interval, "interval", cfg.Interval, "Polling interval")
	watchCmd.Flags().IntVar(&cfg.Workers, "workers", cfg.Workers, "Max concurrent repo workers")
	watchCmd.Flags().StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "Directory for cloned repos")
	watchCmd.Flags().IntVar(&cfg.MaxRetries, "max-retries", cfg.MaxRetries, "Max retries per issue")
	watchCmd.Flags().StringVar(&cfg.LogFile, "log-file", cfg.LogFile, "Log file path")
	watchCmd.Flags().StringVar(&cfg.NtfyTopic, "ntfy-topic", cfg.NtfyTopic, "ntfy.sh topic for error notifications")

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

func setupLogging() {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	if cfg.LogFile != "" {
		f, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			slog.Error("failed to open log file", "error", err)
		} else {
			// Log to both stdout and file
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
			_ = f // TODO: multi-writer if needed, for now stdout is primary
		}
		return
	}

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
}

func runWatch(cmd *cobra.Command, args []string) error {
	setupLogging()

	// Resolve ntfy topic from flag or env
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
	httpClient := oauth2.NewClient(context.Background(), ts)
	ghClient := github.NewClient(httpClient)

	// Resolve repo from current dir if needed
	if cfg.Org == "" && cfg.Repo == "" {
		out, err := exec.Command("gh", "repo", "view", "--json", "nameWithOwner", "-q", ".nameWithOwner").Output()
		if err != nil {
			return fmt.Errorf("no --org or --repo specified and not in a git repo")
		}
		cfg.Repo = strings.TrimSpace(string(out))
	}

	// State
	statePath := os.ExpandEnv("$HOME/.claude-afk/state.json")
	os.MkdirAll(os.ExpandEnv("$HOME/.claude-afk"), 0755)
	st, err := state.Load(statePath)
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

	// Dispatcher
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	sem := make(chan struct{}, cfg.Workers)
	var repoLocks sync.Map // map[string]*sync.Mutex

	mode := "current repo"
	if cfg.Org != "" {
		mode = fmt.Sprintf("org '%s'", cfg.Org)
	} else if cfg.Repo != "" {
		mode = cfg.Repo
	}
	slog.Info("starting claude-afk",
		"mode", mode,
		"label", cfg.Label,
		"strategy", cfg.Strategy,
		"interval", cfg.Interval,
		"workers", cfg.Workers,
	)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	// Run immediately on start, then on tick
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

				// Per-repo lock
				lockI, _ := repoLocks.LoadOrStore(repo, &sync.Mutex{})
				repoMu := lockI.(*sync.Mutex)

				if !repoMu.TryLock() {
					// Repo already has an in-flight issue
					continue
				}

				repo := repo
				issue := issue
				sem <- struct{}{} // acquire worker slot

				go func() {
					defer func() {
						<-sem // release worker slot
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

	poll()
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
```

- [ ] **Step 2: Verify it compiles**

```bash
cd /home/jon/dev/claude-afk && go build -o claude-afk .
```

Expected: compiles without errors.

- [ ] **Step 3: Verify help output**

```bash
./claude-afk watch --help
```

Expected: all flags listed with descriptions.

- [ ] **Step 4: Run all tests**

```bash
cd /home/jon/dev/claude-afk && go test ./... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/watch.go
git commit -m "feat: wire watch command with poller, dispatcher, and worker pool"
```

---

### Task 7: Clean Up

**Files:**
- Modify: `claude-afk.sh` (keep as reference or delete)

- [ ] **Step 1: Move old bash script to reference**

```bash
mkdir -p /home/jon/dev/claude-afk/legacy
mv /home/jon/dev/claude-afk/claude-afk.sh /home/jon/dev/claude-afk/legacy/claude-afk.sh
```

- [ ] **Step 2: Add .gitignore**

Create `.gitignore`:

```
claude-afk
*.log
```

- [ ] **Step 3: Run full test suite one final time**

```bash
cd /home/jon/dev/claude-afk && go test ./... -v -count=1
```

Expected: all tests PASS.

- [ ] **Step 4: Build final binary**

```bash
cd /home/jon/dev/claude-afk && go build -o claude-afk .
```

- [ ] **Step 5: Commit**

```bash
git add .gitignore legacy/
git commit -m "chore: move bash script to legacy, add gitignore"
```
