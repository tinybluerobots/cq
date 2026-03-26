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
	"testing"

	"github.com/google/go-github/v69/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tinybluerobots/issuebot/internal/config"
	"github.com/tinybluerobots/issuebot/internal/notify"
	"github.com/tinybluerobots/issuebot/internal/prompt"
	"github.com/tinybluerobots/issuebot/internal/state"
)

const (
	testIssueTitle = "Test issue"
	testEchoCmd    = "echo {prompt}"
)

// mockCommand creates a shell script that echoes output and exits with the given code.
// Returns the path to the script, suitable for use as CLIConfig.Command.
func mockCommand(t *testing.T, dir string, output string, exitCode int) string {
	t.Helper()

	script := filepath.Join(dir, "mock-cmd")

	content := fmt.Sprintf("#!/bin/bash\necho '%s'\nexit %d\n", output, exitCode)
	require.NoError(t, os.WriteFile(script, []byte(content), 0755))

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

	require.NoError(t, os.WriteFile(filepath.Join(work, "README.md"), []byte("# test"), 0644))

	run(t, "-C", work, "add", ".")
	run(t, "-C", work, "commit", "-m", "init")
	run(t, "-C", work, "push")

	return bare
}

func run(t *testing.T, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)

	cmd.Stderr = os.Stderr
	require.NoError(t, cmd.Run(), "git %v failed", args)
}

func testWorker(t *testing.T) *Worker {
	t.Helper()
	dir := t.TempDir()
	storePath := filepath.Join(dir, "state.json")

	st, err := state.Load(storePath)
	require.NoError(t, err)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	require.NoError(t, prompt.EnsureFile(promptFile), "ensure prompt file")

	return &Worker{
		State:     st,
		Notifier:  notify.New(""),
		CLIConfig: config.CLIConfig{Strategy: config.StrategyPR, MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile, Command: testEchoCmd},
	}
}

func TestWorker_BuildPrompt(t *testing.T) {
	w := testWorker(t)

	promptFile := filepath.Join(t.TempDir(), "prompt.tmpl")
	require.NoError(t, prompt.EnsureFile(promptFile), "ensure prompt file")

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
	require.NoError(t, err)

	for _, want := range []string{"42", "Fix the widget", "The widget is broken", "myorg/myrepo", "ISSUE_RESOLVED", "ISSUE_FAILED"} {
		assert.Contains(t, p, want)
	}
}

func TestWorker_RunCommand_Success(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()

	script := mockCommand(t, dir, "ISSUE_RESOLVED #1", 0)
	w.CLIConfig.Command = script

	result, err := w.RunCommand(context.Background(), dir, "fix the thing")
	require.NoError(t, err)

	assert.Contains(t, result, "ISSUE_RESOLVED")
}

func TestWorker_RunCommand_Failure(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()

	script := mockCommand(t, dir, "nope", 1)
	w.CLIConfig.Command = script

	_, err := w.RunCommand(context.Background(), dir, "fix the thing")
	require.Error(t, err, "expected error for non-zero exit")
}

func TestWorker_RunCommand_PromptSubstitution(t *testing.T) {
	w := testWorker(t)
	w.CLIConfig.Command = testEchoCmd
	dir := t.TempDir()

	result, err := w.RunCommand(context.Background(), dir, "hello world")
	require.NoError(t, err)

	assert.Equal(t, "hello world", result)
}

func TestWorker_RunCommand_PromptPlaceholder(t *testing.T) {
	w := testWorker(t)
	w.CLIConfig.Command = testEchoCmd
	dir := t.TempDir()

	result, err := w.RunCommand(context.Background(), dir, "fix the bug")
	require.NoError(t, err)

	assert.Equal(t, "fix the bug", result)
}

func TestWorker_RunCommand_PromptPlaceholder_QuotesHandled(t *testing.T) {
	w := testWorker(t)
	w.CLIConfig.Command = testEchoCmd
	dir := t.TempDir()

	result, err := w.RunCommand(context.Background(), dir, "it's a test")
	require.NoError(t, err)

	assert.Equal(t, "it's a test", result)
}

func TestWorker_EnsureRepo_Clone(t *testing.T) {
	w := testWorker(t)
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	repoDir, err := w.EnsureRepo(context.Background(), bare, "testorg/testrepo")
	require.NoError(t, err)

	gitDir := filepath.Join(repoDir, ".git")
	_, err = os.Stat(gitDir)
	require.NoError(t, err, ".git dir does not exist at %s", gitDir)
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
		assert.NoError(t, json.NewEncoder(w).Encode(pr))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	ghClient, _ := github.NewClient(nil).WithEnterpriseURLs(ts.URL+"/", ts.URL+"/")

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	cmdPath := mockCommand(t, dir, "ISSUE_RESOLVED", 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	require.NoError(t, prompt.EnsureFile(promptFile), "ensure prompt file")

	w := &Worker{
		Client:    ghClient,
		State:     st,
		Notifier:  notify.New(""),
		CLIConfig: config.CLIConfig{Strategy: config.StrategyPR, MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile, Command: cmdPath},

		CloneURL: bare,
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
	require.True(t, ok, "state not found after ProcessIssue")

	assert.Equal(t, state.StatusCompleted, st2.Status, "error: %s", st2.Error)
}

func TestWorker_ProcessIssue_DryRun(t *testing.T) {
	dir := t.TempDir()
	bare := initBareRepo(t, dir)

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	cmdPath := mockCommand(t, dir, "ISSUE_RESOLVED", 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	require.NoError(t, prompt.EnsureFile(promptFile), "ensure prompt file")

	w := &Worker{
		State:     st,
		Notifier:  notify.New(""),
		CLIConfig: config.CLIConfig{Strategy: config.StrategyPR, MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile, DryRun: true, Command: cmdPath},

		CloneURL: bare,
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
	require.True(t, ok, "state not found")

	assert.Equal(t, state.StatusCompleted, s.Status, "error: %s", s.Error)
	assert.Empty(t, s.PRURL, "dry-run should not create a PR")
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
			assert.NoError(t, json.NewEncoder(w).Encode([]*github.PullRequest{}))

			return
		}

		pr := &github.PullRequest{Number: &prNum, HTMLURL: &prURL}
		assert.NoError(t, json.NewEncoder(w).Encode(pr))
	})

	ts := httptest.NewServer(mux)
	defer ts.Close()

	ghClient, _ := github.NewClient(nil).WithEnterpriseURLs(ts.URL+"/", ts.URL+"/")

	storePath := filepath.Join(dir, "state.json")
	st, _ := state.Load(storePath)

	cmdPath := mockCommand(t, dir, "ISSUE_RESOLVED", 0)

	promptFile := filepath.Join(dir, "prompt.tmpl")
	require.NoError(t, prompt.EnsureFile(promptFile), "ensure prompt file")

	postCmd := fmt.Sprintf("touch %s && echo $PR_URL > %s", markerFile, markerFile)
	issueBody := fmt.Sprintf("Fix something\n<!-- issuebot\npost-command: %s\n-->", postCmd)

	w := &Worker{
		Client:    ghClient,
		State:     st,
		Notifier:  notify.New(""),
		CLIConfig: config.CLIConfig{Strategy: config.StrategyPR, MaxRetries: 3, Workspace: filepath.Join(dir, "repos"), PromptFile: promptFile, Command: cmdPath},

		CloneURL: bare,
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
	require.NoError(t, err, "post-command did not run")

	assert.Contains(t, string(data), prURL)
}

func TestWorker_ProcessIssue_IssueConfigOverride(t *testing.T) {
	cli := config.CLIConfig{Strategy: "direct"}
	body := "<!-- issuebot\nstrategy: pr\nbranch: custom-branch\n-->"
	issueCfg := config.ParseIssueConfig(body)

	strategy := config.ResolveStrategy(cli, issueCfg)
	assert.Equal(t, config.StrategyPR, strategy)

	branch := config.ResolveBranch(issueCfg, 5)
	assert.Equal(t, "custom-branch", branch)
}
