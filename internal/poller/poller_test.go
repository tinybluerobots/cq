package poller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v69/github"
)

func testClient(srv *httptest.Server) *github.Client {
	client := github.NewClient(nil)
	u, _ := url.Parse(srv.URL + "/")
	client.BaseURL = u

	return client
}

func TestPoller_ListRepos_Org(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/myorg/repos" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		if r.URL.Query().Get("type") != "sources" {
			t.Fatalf("expected type=sources, got %s", r.URL.Query().Get("type"))
		}

		repos := []*github.Repository{
			{FullName: github.Ptr("myorg/good-repo"), Archived: github.Ptr(false), Fork: github.Ptr(false)},
			{FullName: github.Ptr("myorg/archived-repo"), Archived: github.Ptr(true), Fork: github.Ptr(false)},
			{FullName: github.Ptr("myorg/forked-repo"), Archived: github.Ptr(false), Fork: github.Ptr(true)},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(repos); err != nil {
			t.Errorf("encode repos: %v", err)
		}
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
		Org:    "myorg",
	}

	repos, err := p.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 1 {
		t.Fatalf("expected 1 repo, got %d: %v", len(repos), repos)
	}

	if repos[0] != "myorg/good-repo" {
		t.Fatalf("expected myorg/good-repo, got %s", repos[0])
	}
}

func TestPoller_ListRepos_Single(t *testing.T) {
	p := &Poller{
		SingleRepo: "owner/single-repo",
	}

	repos, err := p.ListRepos(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repos) != 1 || repos[0] != "owner/single-repo" {
		t.Fatalf("expected [owner/single-repo], got %v", repos)
	}
}

func TestPoller_ListIssues_WithLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/myorg/myrepo/issues" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		if r.URL.Query().Get("state") != "open" {
			t.Fatalf("expected state=open, got %s", r.URL.Query().Get("state"))
		}

		if r.URL.Query().Get("labels") != "claude" {
			t.Fatalf("expected labels=claude, got %s", r.URL.Query().Get("labels"))
		}

		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("Real issue")},
			{Number: github.Ptr(2), Title: github.Ptr("PR disguised"), PullRequestLinks: &github.PullRequestLinks{}},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(issues); err != nil {
			t.Errorf("encode issues: %v", err)
		}
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
		Label:  "claude",
	}

	issues, err := p.ListIssues(context.Background(), "myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	if issues[0].GetNumber() != 1 {
		t.Fatalf("expected issue #1, got #%d", issues[0].GetNumber())
	}
}

func TestPoller_ListIssues_WithAuthor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("By trusted"), User: &github.User{Login: github.Ptr("trusteduser")}},
			{Number: github.Ptr(2), Title: github.Ptr("By stranger"), User: &github.User{Login: github.Ptr("stranger")}},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(issues); err != nil {
			t.Errorf("encode issues: %v", err)
		}
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
		Author: "trusteduser",
	}

	issues, err := p.ListIssues(context.Background(), "myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}

	if issues[0].GetNumber() != 1 {
		t.Fatalf("expected issue #1, got #%d", issues[0].GetNumber())
	}
}

func TestPoller_ListIssues_NoLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("labels") != "" {
			t.Fatalf("expected no labels filter, got %s", r.URL.Query().Get("labels"))
		}

		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("Any issue")},
		}

		w.Header().Set("Content-Type", "application/json")

		if err := json.NewEncoder(w).Encode(issues); err != nil {
			t.Errorf("encode issues: %v", err)
		}
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
	}

	issues, err := p.ListIssues(context.Background(), "myorg/myrepo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
}
