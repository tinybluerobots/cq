# cq Enhancements Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Four independent enhancements to make cq more robust and flexible: dry-run mode, richer prompt templates, post-PR hooks, and state file backups.

**Architecture:** Each task is a standalone feature touching 1-2 packages. No cross-dependencies between tasks — they can be implemented in any order.

**Tech Stack:** Go, cobra (CLI), go-github, text/template

---

### Task 1: `--dry-run` flag

Adds a `--dry-run` flag that runs the full pipeline (clone, run command) but skips push/PR creation. Outputs the git diff instead.

**Files:**
- Modify: `internal/config/config.go` — add `DryRun bool` to `CLIConfig`
- Modify: `cmd/watch.go` — add `--dry-run` flag binding
- Modify: `internal/worker/worker.go` — skip push/PR when dry-run, print diff instead
- Test: `internal/worker/worker_test.go` — add `TestWorker_ProcessIssue_DryRun`

- [ ] **Step 1: Add DryRun to CLIConfig**

In `internal/config/config.go`, add `DryRun` field to `CLIConfig`:

```go
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
```

- [ ] **Step 2: Bind the flag in cmd/watch.go**

Add to the `init()` function:

```go
f.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "Run command but skip push/PR (print diff instead)")
```

- [ ] **Step 3: Write failing test for dry-run**

In `internal/worker/worker_test.go`, add:

```go
func TestWorker_ProcessIssue_DryRun(t *testing.T) {
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	claudeOutput := `{"type":"result","result":"ISSUE_RESOLVED #1"}`
	claudePath := mockClaude(t, dir, claudeOutput, 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	if err := prompt.EnsureFile(promptFile); err != nil {
		t.Fatalf("ensure prompt file: %v", err)
	}

	w := &Worker{
		State:      st,
		Notifier:   notify.New(""),
		CLIConfig:  config.CLIConfig{Strategy: "pr", MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile, DryRun: true},
		Workspace:  filepath.Join(dir, "repos"),
		ClaudePath: claudePath,
		CloneURL:   bare,
	}

	issueNum := 1
	issueTitle := "Test issue"
	issueBody := "Fix something"
	issue := &github.Issue{
		Number: &issueNum,
		Title:  &issueTitle,
		Body:   &issueBody,
	}

	w.ProcessIssue(context.Background(), "testorg/testrepo", issue)

	s, ok := w.State.Get("testorg/testrepo#1")
	if !ok {
		t.Fatal("state not found")
	}

	// Dry-run should mark completed without pushing or creating PR.
	if s.Status != state.StatusCompleted {
		t.Errorf("status = %q, want %q (error: %s)", s.Status, state.StatusCompleted, s.Error)
	}

	if s.PRURL != "" {
		t.Errorf("dry-run should not create a PR, got %s", s.PRURL)
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/worker/ -run TestWorker_ProcessIssue_DryRun -v`
Expected: FAIL (no dry-run logic exists yet)

- [ ] **Step 5: Implement dry-run in ProcessIssue**

In `internal/worker/worker.go`, after `RunCommand` succeeds and the ISSUE_FAILED check passes, add the dry-run gate before the push/PR block:

```go
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
```

Place this block immediately before the `if strategy == "pr"` push block (line ~244 in the current file).

- [ ] **Step 6: Run tests**

Run: `go test ./internal/worker/ -v`
Expected: All pass including the new dry-run test.

- [ ] **Step 7: Commit**

```bash
git add internal/config/config.go cmd/watch.go internal/worker/worker.go internal/worker/worker_test.go
git commit -m "feat: add --dry-run flag to skip push/PR and print diff"
```

---

### Task 2: Richer prompt template variables

Add `Labels`, `Author`, and `DefaultBranch` to the prompt template data, giving users more context in custom prompts.

**Files:**
- Modify: `internal/prompt/prompt.go` — expand `Data` struct, update `Render` signature
- Modify: `internal/worker/worker.go` — pass new fields to `Render`
- Test: `internal/prompt/prompt_test.go` (create) — test new template variables

- [ ] **Step 1: Write failing test**

Create `internal/prompt/prompt_test.go`:

```go
package prompt

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-github/v69/github"
)

func TestRender_ExtraFields(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.tmpl")

	tmpl := `Repo: {{.Repo}} Author: {{.Author}} Labels: {{.Labels}} Branch: {{.DefaultBranch}}`
	if err := os.WriteFile(tmplPath, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}

	num := 1
	title := "test"
	body := "body"
	login := "octocat"
	user := &github.User{Login: &login}
	labelName := "bug"
	labels := []*github.Label{{Name: &labelName}}

	issue := &github.Issue{
		Number: &num,
		Title:  &title,
		Body:   &body,
		User:   user,
		Labels: labels,
	}

	result, err := Render(tmplPath, "org/repo", issue, "main")
	if err != nil {
		t.Fatal(err)
	}

	if result != "Repo: org/repo Author: octocat Labels: bug Branch: main" {
		t.Errorf("got %q", result)
	}
}

func TestRender_MultipleLabels(t *testing.T) {
	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "test.tmpl")

	tmpl := `{{.Labels}}`
	if err := os.WriteFile(tmplPath, []byte(tmpl), 0644); err != nil {
		t.Fatal(err)
	}

	num := 1
	title := "test"
	l1 := "bug"
	l2 := "priority"
	labels := []*github.Label{{Name: &l1}, {Name: &l2}}

	issue := &github.Issue{
		Number: &num,
		Title:  &title,
		Labels: labels,
	}

	result, err := Render(tmplPath, "org/repo", issue, "main")
	if err != nil {
		t.Fatal(err)
	}

	if result != "bug, priority" {
		t.Errorf("got %q", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/prompt/ -v`
Expected: FAIL — `Render` has wrong signature.

- [ ] **Step 3: Update Data struct and Render function**

In `internal/prompt/prompt.go`, expand `Data` and update `Render`:

```go
// Data holds the template fields available to the prompt.
type Data struct {
	Repo          string
	Number        int
	Title         string
	Body          string
	Author        string
	Labels        string
	DefaultBranch string
}

// Render loads the template from path and renders it with the given issue data.
func Render(path, repo string, issue *github.Issue, defaultBranch string) (string, error) {
	tmplBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	tmpl, err := template.New("prompt").Parse(string(tmplBytes))
	if err != nil {
		return "", err
	}

	body := ""
	if issue.Body != nil {
		body = *issue.Body
	}

	author := ""
	if issue.User != nil {
		author = issue.User.GetLogin()
	}

	var labelNames []string
	for _, l := range issue.Labels {
		labelNames = append(labelNames, l.GetName())
	}

	data := Data{
		Repo:          repo,
		Number:        issue.GetNumber(),
		Title:         issue.GetTitle(),
		Body:          body,
		Author:        author,
		Labels:        strings.Join(labelNames, ", "),
		DefaultBranch: defaultBranch,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}
```

Add `"strings"` to the imports.

- [ ] **Step 4: Update worker.go BuildPrompt**

In `internal/worker/worker.go`, update `BuildPrompt` to accept and pass `defaultBranch`:

```go
func (w *Worker) BuildPrompt(repo string, issue *github.Issue, defaultBranch string) (string, error) {
	return prompt.Render(w.CLIConfig.PromptFile, repo, issue, defaultBranch)
}
```

Update the call site in `ProcessIssue` (currently `w.BuildPrompt(repo, issue)`) to pass `"main"`:

```go
prompt, err := w.BuildPrompt(repo, issue, "main")
```

Note: For now, hardcode `"main"`. A future enhancement could detect the default branch from the GitHub API.

- [ ] **Step 5: Update worker_test.go BuildPrompt calls**

In `internal/worker/worker_test.go`, update the `TestWorker_BuildPrompt` test to pass the new parameter:

```go
p, err := w.BuildPrompt("myorg/myrepo", issue, "main")
```

- [ ] **Step 6: Run all tests**

Run: `go test ./internal/prompt/ ./internal/worker/ -v`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/prompt/prompt.go internal/prompt/prompt_test.go internal/worker/worker.go internal/worker/worker_test.go
git commit -m "feat: add Author, Labels, DefaultBranch to prompt template"
```

---

### Task 3: Post-PR hooks via per-issue config

Allow issues to specify a `post-command` that runs after a PR is created, with `$PR_URL` and `$PR_NUMBER` available as env vars.

**Files:**
- Modify: `internal/config/config.go` — add `PostCommand` to `IssueConfig`
- Modify: `internal/worker/worker.go` — run post-command after PR creation
- Test: `internal/worker/worker_test.go` — add `TestWorker_ProcessIssue_PostCommand`
- Test: `internal/config/config_test.go` — add parse test

- [ ] **Step 1: Add PostCommand to IssueConfig**

In `internal/config/config.go`, add:

```go
type IssueConfig struct {
	Strategy    string `yaml:"strategy"`
	Branch      string `yaml:"branch"`
	PostCommand string `yaml:"post-command"`
}
```

- [ ] **Step 2: Write config parse test**

In `internal/config/config_test.go`, add:

```go
func TestParseIssueConfig_PostCommand(t *testing.T) {
	body := "<!-- cq\npost-command: gh pr comment $PR_NUMBER -b 'ready'\n-->"
	cfg := ParseIssueConfig(body)

	if cfg.PostCommand != "gh pr comment $PR_NUMBER -b 'ready'" {
		t.Errorf("PostCommand = %q", cfg.PostCommand)
	}
}
```

- [ ] **Step 3: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestParseIssueConfig_PostCommand -v`
Expected: PASS (YAML parsing handles this automatically).

- [ ] **Step 4: Write failing worker test**

In `internal/worker/worker_test.go`, add:

```go
func TestWorker_ProcessIssue_PostCommand(t *testing.T) {
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	// Track post-command execution via a marker file.
	markerFile := filepath.Join(dir, "post-command-ran")

	prNum := 99
	prURL := "https://github.com/testorg/testrepo/pull/99"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/testorg/testrepo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// No existing PRs.
			if err := json.NewEncoder(w).Encode([]*github.PullRequest{}); err != nil {
				t.Errorf("encode: %v", err)
			}

			return
		}

		pr := &github.PullRequest{Number: &prNum, HTMLURL: &prURL}
		if err := json.NewEncoder(w).Encode(pr); err != nil {
			t.Errorf("encode: %v", err)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	ghClient, _ := github.NewClient(nil).WithEnterpriseURLs(ts.URL+"/", ts.URL+"/")

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	claudeOutput := `{"type":"result","result":"ISSUE_RESOLVED #1"}`
	claudePath := mockClaude(t, dir, claudeOutput, 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	if err := prompt.EnsureFile(promptFile); err != nil {
		t.Fatalf("ensure prompt file: %v", err)
	}

	// Issue body with post-command that creates a marker file.
	postCmd := fmt.Sprintf("touch %s && echo $PR_URL > %s", markerFile, markerFile)
	issueBody := fmt.Sprintf("Fix something\n<!-- cq\npost-command: %s\n-->", postCmd)

	w := &Worker{
		Client:     ghClient,
		State:      st,
		Notifier:   notify.New(""),
		CLIConfig:  config.CLIConfig{Strategy: "pr", MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile},
		Workspace:  filepath.Join(dir, "repos"),
		ClaudePath: claudePath,
		CloneURL:   bare,
	}

	issueNum := 1
	issueTitle := "Test issue"
	issue := &github.Issue{
		Number: &issueNum,
		Title:  &issueTitle,
		Body:   &issueBody,
	}

	w.ProcessIssue(context.Background(), "testorg/testrepo", issue)

	// Verify post-command ran.
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("post-command did not run: %v", err)
	}

	if !strings.Contains(string(data), prURL) {
		t.Errorf("post-command output = %q, want to contain %q", string(data), prURL)
	}
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/worker/ -run TestWorker_ProcessIssue_PostCommand -v`
Expected: FAIL — post-command not implemented yet.

- [ ] **Step 6: Implement post-command execution**

In `internal/worker/worker.go`, add a `runPostCommand` method:

```go
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
```

Then in `pushAndCreatePR`, after setting state to completed, call it. This requires passing `issueCfg` into `pushAndCreatePR`. Update the signature:

```go
func (w *Worker) pushAndCreatePR(ctx context.Context, key string, logger *slog.Logger, repoDir, repo, branch string, issue *github.Issue, attempts int, issueCfg config.IssueConfig) error {
```

After the state is set to completed (both the existing-PR path and the new-PR path), add:

```go
w.runPostCommand(ctx, logger, issueCfg.PostCommand, pr.GetHTMLURL(), pr.GetNumber())
```

Update the call site in `ProcessIssue` to pass `issueCfg`:

```go
if err := w.pushAndCreatePR(ctx, key, logger, repoDir, repo, branch, issue, attempts, issueCfg); err != nil {
```

- [ ] **Step 7: Run tests**

Run: `go test ./internal/worker/ -v`
Expected: All pass.

- [ ] **Step 8: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go internal/worker/worker.go internal/worker/worker_test.go
git commit -m "feat: add post-command hook for per-issue config"
```

---

### Task 4: State file backup rotation

Before each save, copy the current state file to `state.json.bak`. If the primary file is corrupt on load, try the backup.

**Files:**
- Modify: `internal/state/state.go` — add backup on save, fallback on load
- Test: `internal/state/state_test.go` — add backup/recovery tests

- [ ] **Step 1: Write failing test for backup creation**

In `internal/state/state_test.go`, add:

```go
func TestStore_Save_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	st.Set("org/repo#1", IssueState{Status: StatusCompleted, Attempts: 1})

	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	// First save has no backup (nothing to back up).
	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
		t.Fatal("backup should not exist after first save")
	}

	// Second save should create backup of the first.
	st.Set("org/repo#2", IssueState{Status: StatusFailed, Attempts: 2})

	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("backup not created: %v", err)
	}

	// Backup should contain only issue #1 (pre-second-save state).
	bakStore, err := Load(bakPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := bakStore.Issues["org/repo#1"]; !ok {
		t.Error("backup missing org/repo#1")
	}

	if _, ok := bakStore.Issues["org/repo#2"]; ok {
		t.Error("backup should not contain org/repo#2")
	}
}
```

- [ ] **Step 2: Write failing test for corrupt file recovery**

```go
func TestLoad_FallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	bakPath := path + ".bak"

	// Write a valid backup.
	bak, _ := Load(bakPath)
	bak.Set("org/repo#1", IssueState{Status: StatusCompleted, Attempts: 1})

	if err := bak.Save(); err != nil {
		t.Fatal(err)
	}

	// Write corrupt primary.
	if err := os.WriteFile(path, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	st, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := st.Issues["org/repo#1"]; !ok {
		t.Error("should have recovered org/repo#1 from backup")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/state/ -run "TestStore_Save_CreatesBackup|TestLoad_FallsBackToBackup" -v`
Expected: FAIL

- [ ] **Step 4: Implement backup on save**

In `internal/state/state.go`, update `Save`:

```go
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Back up current file before overwriting.
	if _, err := os.Stat(s.path); err == nil {
		bakPath := s.path + ".bak"
		data, err := os.ReadFile(s.path)
		if err == nil {
			_ = os.WriteFile(bakPath, data, 0644)
		}
	}

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
```

- [ ] **Step 5: Implement backup fallback on load**

In `internal/state/state.go`, update `Load` to try backup when primary is corrupt:

```go
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
		slog.Warn("corrupt state file, trying backup", "path", path, "error", err)

		return loadBackup(path)
	}

	if s.Issues == nil {
		s.Issues = make(map[string]IssueState)
	}

	return s, nil
}

func loadBackup(path string) (*Store, error) {
	bakPath := path + ".bak"
	s := &Store{
		Issues: make(map[string]IssueState),
		path:   path,
	}

	data, err := os.ReadFile(bakPath)
	if err != nil {
		slog.Warn("no backup available, starting fresh", "path", bakPath)
		return s, nil
	}

	if err := json.Unmarshal(data, s); err != nil {
		slog.Warn("backup also corrupt, starting fresh", "path", bakPath, "error", err)
		return s, nil
	}

	if s.Issues == nil {
		s.Issues = make(map[string]IssueState)
	}

	s.path = path
	slog.Info("recovered state from backup", "path", bakPath, "issues", len(s.Issues))

	return s, nil
}
```

- [ ] **Step 6: Run all state tests**

Run: `go test ./internal/state/ -v`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/state/state.go internal/state/state_test.go
git commit -m "feat: add state file backup rotation and corrupt recovery"
```
