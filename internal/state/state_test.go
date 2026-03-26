package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStore_LoadEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")

	store, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, store.Issues)
}

func TestStore_SetAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, _ := Load(path)

	now := time.Now().Truncate(time.Second)
	want := IssueState{
		Status:      StatusInProgress,
		Attempts:    2,
		LastAttempt: now,
		PRURL:       "https://github.com/foo/bar/pull/1",
		Error:       "something broke",
	}
	store.Set("issue-1", want)

	got, ok := store.Get("issue-1")
	require.True(t, ok, "expected key to exist")
	assert.Equal(t, want.Status, got.Status)
	assert.Equal(t, want.Attempts, got.Attempts)
	assert.True(t, got.LastAttempt.Equal(want.LastAttempt), "LastAttempt = %v, want %v", got.LastAttempt, want.LastAttempt)
	assert.Equal(t, want.PRURL, got.PRURL)
	assert.Equal(t, want.Error, got.Error)
}

func TestStore_SaveAndReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, _ := Load(path)

	want := IssueState{
		Status:   StatusCompleted,
		Attempts: 1,
		PRURL:    "https://github.com/foo/bar/pull/42",
	}
	store.Set("issue-42", want)

	require.NoError(t, store.Save(), "Save failed")

	store2, err := Load(path)
	require.NoError(t, err, "Load failed")

	got, ok := store2.Get("issue-42")
	require.True(t, ok, "expected key to exist after reload")
	assert.Equal(t, want.Status, got.Status)
	assert.Equal(t, want.PRURL, got.PRURL)
}

func TestStore_RecoverInProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, _ := Load(path)

	store.Set("a", IssueState{Status: StatusInProgress})
	store.Set("b", IssueState{Status: StatusCompleted})
	store.Set("c", IssueState{Status: StatusFailed})

	store.RecoverCrashed()

	got, _ := store.Get("a")
	assert.Equal(t, StatusPending, got.Status, "in_progress should become pending")

	got, _ = store.Get("b")
	assert.Equal(t, StatusCompleted, got.Status, "completed should stay completed")

	got, _ = store.Get("c")
	assert.Equal(t, StatusFailed, got.Status, "failed should stay failed")
}

func TestStore_ShouldProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, _ := Load(path)

	tests := []struct {
		name       string
		key        string
		state      *IssueState // nil = not set
		maxRetries int
		want       bool
	}{
		{"new issue", "new", nil, 3, true},
		{"completed", "done", &IssueState{Status: StatusCompleted}, 3, false},
		{"in_progress", "wip", &IssueState{Status: StatusInProgress}, 3, false},
		{"failed under max", "fail-low", &IssueState{Status: StatusFailed, Attempts: 1}, 3, true},
		{"failed at max", "fail-max", &IssueState{Status: StatusFailed, Attempts: 3}, 3, false},
		{"pending", "pend", &IssueState{Status: StatusPending}, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.state != nil {
				store.Set(tt.key, *tt.state)
			}

			assert.Equal(t, tt.want, store.ShouldProcess(tt.key, tt.maxRetries))
		})
	}
}

func TestStore_Save_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")

	st, err := Load(path)
	require.NoError(t, err)

	st.Set("org/repo#1", IssueState{Status: StatusCompleted, Attempts: 1})

	require.NoError(t, st.Save())

	bakPath := path + ".bak"
	_, err = os.Stat(bakPath)
	assert.True(t, os.IsNotExist(err), "backup should not exist after first save")

	st.Set("org/repo#2", IssueState{Status: StatusFailed, Attempts: 2})

	require.NoError(t, st.Save())

	_, err = os.Stat(bakPath)
	require.NoError(t, err, "backup not created")

	bakStore, err := Load(bakPath)
	require.NoError(t, err)

	assert.Contains(t, bakStore.Issues, "org/repo#1", "backup missing org/repo#1")
	assert.NotContains(t, bakStore.Issues, "org/repo#2", "backup should not contain org/repo#2")
}

func TestLoad_FallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	bakPath := path + ".bak"

	bak, _ := Load(bakPath)
	bak.Set("org/repo#1", IssueState{Status: StatusCompleted, Attempts: 1})

	require.NoError(t, bak.Save())
	require.NoError(t, os.WriteFile(path, []byte("{invalid json"), 0644))

	st, err := Load(path)
	require.NoError(t, err)

	assert.Contains(t, st.Issues, "org/repo#1", "should have recovered org/repo#1 from backup")
}

func TestStore_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	require.NoError(t, os.WriteFile(path, []byte("not json {{{garbage"), 0644))

	store, err := Load(path)
	require.NoError(t, err, "expected no error on corrupt file")
	assert.Empty(t, store.Issues, "expected empty issues on corrupt file")
}
