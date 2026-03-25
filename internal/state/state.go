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

// IssueState tracks the processing state of a single issue.
type IssueState struct {
	Status      string    `json:"status"`
	Attempts    int       `json:"attempts"`
	LastAttempt time.Time `json:"last_attempt,omitempty"`
	PRURL       string    `json:"pr_url,omitempty"`
	Error       string    `json:"error,omitempty"`
}

// Store persists issue processing state to disk.
type Store struct {
	Issues map[string]IssueState `json:"issues"`
	path   string
	mu     sync.Mutex
}

// Load reads state from disk. Returns an empty store if the file is missing or corrupt.
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
		return s, nil
	}

	if s.Issues == nil {
		s.Issues = make(map[string]IssueState)
	}

	return s, nil
}

// Save writes the store to disk atomically via tmp+rename.
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

// Set stores the state for a given issue key.
func (s *Store) Set(key string, state IssueState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Issues[key] = state
}

// Get retrieves the state for a given issue key.
func (s *Store) Get(key string) (IssueState, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Issues[key]
	return st, ok
}

// RecoverCrashed resets any in_progress issues back to pending.
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

// ShouldProcess returns true if the issue should be processed.
// Returns false for completed, in_progress, or failed with attempts >= maxRetries.
func (s *Store) ShouldProcess(key string, maxRetries int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.Issues[key]
	if !ok {
		return true
	}
	switch st.Status {
	case StatusCompleted, StatusInProgress:
		return false
	case StatusFailed:
		return st.Attempts < maxRetries
	default:
		return true
	}
}
