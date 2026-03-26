package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_LoadEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")

	store, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(store.Issues) != 0 {
		t.Fatalf("expected empty issues, got %d", len(store.Issues))
	}
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
	if !ok {
		t.Fatal("expected key to exist")
	}

	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}

	if got.Attempts != want.Attempts {
		t.Errorf("Attempts = %d, want %d", got.Attempts, want.Attempts)
	}

	if !got.LastAttempt.Equal(want.LastAttempt) {
		t.Errorf("LastAttempt = %v, want %v", got.LastAttempt, want.LastAttempt)
	}

	if got.PRURL != want.PRURL {
		t.Errorf("PRURL = %q, want %q", got.PRURL, want.PRURL)
	}

	if got.Error != want.Error {
		t.Errorf("Error = %q, want %q", got.Error, want.Error)
	}
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

	if err := store.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	store2, err := Load(path)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	got, ok := store2.Get("issue-42")
	if !ok {
		t.Fatal("expected key to exist after reload")
	}

	if got.Status != want.Status {
		t.Errorf("Status = %q, want %q", got.Status, want.Status)
	}

	if got.PRURL != want.PRURL {
		t.Errorf("PRURL = %q, want %q", got.PRURL, want.PRURL)
	}
}

func TestStore_RecoverInProgress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	store, _ := Load(path)

	store.Set("a", IssueState{Status: StatusInProgress})
	store.Set("b", IssueState{Status: StatusCompleted})
	store.Set("c", IssueState{Status: StatusFailed})

	store.RecoverCrashed()

	got, _ := store.Get("a")
	if got.Status != StatusPending {
		t.Errorf("in_progress should become pending, got %q", got.Status)
	}

	got, _ = store.Get("b")
	if got.Status != StatusCompleted {
		t.Errorf("completed should stay completed, got %q", got.Status)
	}

	got, _ = store.Get("c")
	if got.Status != StatusFailed {
		t.Errorf("failed should stay failed, got %q", got.Status)
	}
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

			if got := store.ShouldProcess(tt.key, tt.maxRetries); got != tt.want {
				t.Errorf("ShouldProcess(%q, %d) = %v, want %v", tt.key, tt.maxRetries, got, tt.want)
			}
		})
	}
}

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

	bakPath := path + ".bak"
	if _, err := os.Stat(bakPath); !os.IsNotExist(err) {
		t.Fatal("backup should not exist after first save")
	}

	st.Set("org/repo#2", IssueState{Status: StatusFailed, Attempts: 2})

	if err := st.Save(); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(bakPath); err != nil {
		t.Fatalf("backup not created: %v", err)
	}

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

func TestLoad_FallsBackToBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	bakPath := path + ".bak"

	bak, _ := Load(bakPath)
	bak.Set("org/repo#1", IssueState{Status: StatusCompleted, Attempts: 1})

	if err := bak.Save(); err != nil {
		t.Fatal(err)
	}

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

func TestStore_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("not json {{{garbage"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := Load(path)
	if err != nil {
		t.Fatalf("expected no error on corrupt file, got %v", err)
	}

	if len(store.Issues) != 0 {
		t.Fatalf("expected empty issues on corrupt file, got %d", len(store.Issues))
	}
}
