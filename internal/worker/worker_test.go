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

	"github.com/google/go-github/v69/github"
	"github.com/jon/claude-afk/internal/config"
	"github.com/jon/claude-afk/internal/notify"
	"github.com/jon/claude-afk/internal/state"
)

func mockClaude(t *testing.T, dir string, output string, exitCode int) string {
	t.Helper()
	script := filepath.Join(dir, "claude")
	content := fmt.Sprintf("#!/bin/bash\necho '%s'\nexit %d\n", output, exitCode)
	if err := os.WriteFile(script, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func initBareRepo(t *testing.T, dir string) string {
	t.Helper()
	bare := filepath.Join(dir, "upstream.git")
	run(t, "git", "init", "--bare", bare)

	// Clone, add a commit, push so the bare repo has content.
	work := filepath.Join(dir, "work")
	run(t, "git", "clone", bare, work)
	run(t, "git", "-C", work, "config", "user.email", "test@test.com")
	run(t, "git", "-C", work, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# test"), 0644); err != nil {
		t.Fatal(err)
	}
	run(t, "git", "-C", work, "add", ".")
	run(t, "git", "-C", work, "commit", "-m", "init")
	run(t, "git", "-C", work, "push")
	return bare
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v failed: %v", name, args, err)
	}
}

func testWorker(t *testing.T) *Worker {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "state.json")
	st, err := state.Load(storePath)
	if err != nil {
		t.Fatal(err)
	}
	return &Worker{
		State:     st,
		Notifier:  notify.New(""),
		CLIConfig: config.CLIConfig{Strategy: "pr", MaxRetries: 3, Workspace: filepath.Join(dir, "repos")},
		Workspace: filepath.Join(dir, "repos"),
	}
}

func TestWorker_BuildPrompt(t *testing.T) {
	w := testWorker(t)
	num := 42
	title := "Fix the widget"
	body := "The widget is broken\nPlease fix it"
	issue := &github.Issue{
		Number: &num,
		Title:  &title,
		Body:   &body,
	}

	prompt := w.BuildPrompt("myorg/myrepo", issue)

	for _, want := range []string{"42", "Fix the widget", "The widget is broken", "myorg/myrepo", "ISSUE_RESOLVED", "ISSUE_FAILED"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestWorker_RunClaude_Success(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()

	resultJSON := `{"type":"result","result":"ISSUE_RESOLVED #1"}`
	w.ClaudePath = mockClaude(t, dir, resultJSON, 0)

	result, err := w.RunClaude(context.Background(), dir, "fix the thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "ISSUE_RESOLVED") {
		t.Errorf("result = %q, want ISSUE_RESOLVED", result)
	}
}

func TestWorker_RunClaude_Failure(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()

	w.ClaudePath = mockClaude(t, dir, `{"type":"result","result":"nope"}`, 1)

	_, err := w.RunClaude(context.Background(), dir, "fix the thing")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestWorker_EnsureRepo_Clone(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	repoDir, err := w.EnsureRepo(context.Background(), bare, "testorg/testrepo")
	if err != nil {
		t.Fatalf("EnsureRepo failed: %v", err)
	}

	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		t.Fatalf(".git dir does not exist at %s", gitDir)
	}
}

func TestWorker_ProcessIssue_PRStrategy(t *testing.T) {
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	// Mock GitHub API for PR creation.
	prNum := 99
	prURL := "https://github.com/testorg/testrepo/pull/99"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/testorg/testrepo/pulls", func(w http.ResponseWriter, r *http.Request) {
		pr := &github.PullRequest{Number: &prNum, HTMLURL: &prURL}
		json.NewEncoder(w).Encode(pr)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ghClient, _ := github.NewClient(nil).WithEnterpriseURLs(ts.URL+"/", ts.URL+"/")

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	claudeOutput := `{"type":"result","result":"ISSUE_RESOLVED #1"}`
	claudePath := mockClaude(t, dir, claudeOutput, 0)

	w := &Worker{
		Client:     ghClient,
		State:      st,
		Notifier:   notify.New(""),
		CLIConfig:  config.CLIConfig{Strategy: "pr", MaxRetries: 3, Workspace: filepath.Join(dir, "repos")},
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

	st2, ok := w.State.Get("testorg/testrepo#1")
	if !ok {
		t.Fatal("state not found after ProcessIssue")
	}
	if st2.Status != state.StatusCompleted {
		t.Errorf("status = %q, want %q (error: %s)", st2.Status, state.StatusCompleted, st2.Error)
	}
}

func TestWorker_ProcessIssue_IssueConfigOverride(t *testing.T) {
	cli := config.CLIConfig{Strategy: "direct"}
	body := "<!-- claude-afk\nstrategy: pr\nbranch: custom-branch\n-->"
	issueCfg := config.ParseIssueConfig(body)

	strategy := config.ResolveStrategy(cli, issueCfg)
	if strategy != "pr" {
		t.Errorf("strategy = %q, want %q", strategy, "pr")
	}

	branch := config.ResolveBranch(issueCfg, 5)
	if branch != "custom-branch" {
		t.Errorf("branch = %q, want %q", branch, "custom-branch")
	}
}
