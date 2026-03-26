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
	"github.com/tinybluerobots/cq/internal/config"
	"github.com/tinybluerobots/cq/internal/notify"
	"github.com/tinybluerobots/cq/internal/prompt"
	"github.com/tinybluerobots/cq/internal/state"
)

const (
	testClaudeOutput = `{"type":"result","result":"ISSUE_RESOLVED #1"}`
	testIssueTitle   = "Test issue"
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
	run(t, "init", "--bare", bare)

	// Clone, add a commit, push so the bare repo has content.
	work := filepath.Join(dir, "work")
	run(t, "clone", bare, work)
	run(t, "-C", work, "config", "user.email", "test@test.com")
	run(t, "-C", work, "config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# test"), 0644); err != nil {
		t.Fatal(err)
	}

	run(t, "-C", work, "add", ".")
	run(t, "-C", work, "commit", "-m", "init")
	run(t, "-C", work, "push")

	return bare
}

func run(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)

	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v failed: %v", args, err)
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

	promptFile := filepath.Join(dir, "prompt.tmpl")
	if err := prompt.EnsureFile(promptFile); err != nil {
		t.Fatalf("ensure prompt file: %v", err)
	}

	return &Worker{
		State:     st,
		Notifier:  notify.New(""),
		CLIConfig: config.CLIConfig{Strategy: "pr", MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile},
		Workspace: filepath.Join(dir, "repos"),
	}
}

func TestWorker_BuildPrompt(t *testing.T) {
	w := testWorker(t)

	promptFile := filepath.Join(t.TempDir(), "prompt.tmpl")
	if err := prompt.EnsureFile(promptFile); err != nil {
		t.Fatalf("ensure prompt file: %v", err)
	}

	w.CLIConfig.PromptFile = promptFile

	num := 42
	title := "Fix the widget"
	body := "The widget is broken\nPlease fix it"
	issue := &github.Issue{
		Number: &num,
		Title:  &title,
		Body:   &body,
	}

	p, err := w.BuildPrompt("myorg/myrepo", issue, "main")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, want := range []string{"42", "Fix the widget", "The widget is broken", "myorg/myrepo", "ISSUE_RESOLVED", "ISSUE_FAILED"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
}

func TestWorker_RunCommand_Claude_Success(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()

	resultJSON := testClaudeOutput
	w.ClaudePath = mockClaude(t, dir, resultJSON, 0)

	result, err := w.RunCommand(context.Background(), dir, "fix the thing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "ISSUE_RESOLVED") {
		t.Errorf("result = %q, want ISSUE_RESOLVED", result)
	}
}

func TestWorker_RunCommand_Claude_Failure(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()

	w.ClaudePath = mockClaude(t, dir, `{"type":"result","result":"nope"}`, 1)

	_, err := w.RunCommand(context.Background(), dir, "fix the thing")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
}

func TestWorker_RunCommand_Custom(t *testing.T) {
	w := testWorker(t)
	w.CLIConfig.Command = "cat"
	dir := t.TempDir()

	result, err := w.RunCommand(context.Background(), dir, "hello from stdin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "hello from stdin" {
		t.Errorf("result = %q, want %q", result, "hello from stdin")
	}
}

func TestWorker_RunCommand_PromptPlaceholder(t *testing.T) {
	w := testWorker(t)
	w.CLIConfig.Command = "echo {prompt}"
	dir := t.TempDir()

	result, err := w.RunCommand(context.Background(), dir, "fix the bug")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "fix the bug" {
		t.Errorf("result = %q, want %q", result, "fix the bug")
	}
}

func TestWorker_RunCommand_PromptPlaceholder_QuotesHandled(t *testing.T) {
	w := testWorker(t)
	w.CLIConfig.Command = "echo {prompt}"
	dir := t.TempDir()

	result, err := w.RunCommand(context.Background(), dir, "it's a test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "it's a test" {
		t.Errorf("result = %q, want %q", result, "it's a test")
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
		if err := json.NewEncoder(w).Encode(pr); err != nil {
			t.Errorf("encode PR response: %v", err)
		}
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	ghClient, _ := github.NewClient(nil).WithEnterpriseURLs(ts.URL+"/", ts.URL+"/")

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	claudeOutput := testClaudeOutput
	claudePath := mockClaude(t, dir, claudeOutput, 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	if err := prompt.EnsureFile(promptFile); err != nil {
		t.Fatalf("ensure prompt file: %v", err)
	}

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
	issueTitle := testIssueTitle
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

func TestWorker_ProcessIssue_DryRun(t *testing.T) {
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	claudeOutput := testClaudeOutput
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
	issueTitle := testIssueTitle
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

	if s.Status != state.StatusCompleted {
		t.Errorf("status = %q, want %q (error: %s)", s.Status, state.StatusCompleted, s.Error)
	}

	if s.PRURL != "" {
		t.Errorf("dry-run should not create a PR, got %s", s.PRURL)
	}
}

func TestWorker_ProcessIssue_PostCommand(t *testing.T) {
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	markerFile := filepath.Join(dir, "post-command-ran")

	prNum := 99
	prURL := "https://github.com/testorg/testrepo/pull/99"
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v3/repos/testorg/testrepo/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
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

	claudeOutput := testClaudeOutput
	claudePath := mockClaude(t, dir, claudeOutput, 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	if err := prompt.EnsureFile(promptFile); err != nil {
		t.Fatalf("ensure prompt file: %v", err)
	}

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
	issueTitle := testIssueTitle
	issue := &github.Issue{
		Number: &issueNum,
		Title:  &issueTitle,
		Body:   &issueBody,
	}

	w.ProcessIssue(context.Background(), "testorg/testrepo", issue)

	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("post-command did not run: %v", err)
	}

	if !strings.Contains(string(data), prURL) {
		t.Errorf("post-command output = %q, want to contain %q", string(data), prURL)
	}
}

func TestWorker_ProcessIssue_IssueConfigOverride(t *testing.T) {
	cli := config.CLIConfig{Strategy: "direct"}
	body := "<!-- cq\nstrategy: pr\nbranch: custom-branch\n-->"
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
