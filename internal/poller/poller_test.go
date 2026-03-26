package poller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v69/github"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(srv *httptest.Server) *github.Client {
	client := github.NewClient(nil)
	u, _ := url.Parse(srv.URL + "/")
	client.BaseURL = u

	return client
}

func TestPoller_ListRepos_Org(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/orgs/myorg/repos", r.URL.Path)
		assert.Equal(t, "sources", r.URL.Query().Get("type"))

		repos := []*github.Repository{
			{FullName: github.Ptr("myorg/good-repo"), Archived: github.Ptr(false), Fork: github.Ptr(false)},
			{FullName: github.Ptr("myorg/archived-repo"), Archived: github.Ptr(true), Fork: github.Ptr(false)},
			{FullName: github.Ptr("myorg/forked-repo"), Archived: github.Ptr(false), Fork: github.Ptr(true)},
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(repos))
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
		Org:    "myorg",
	}

	repos, err := p.ListRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "myorg/good-repo", repos[0])
}

func TestPoller_ListRepos_Single(t *testing.T) {
	p := &Poller{
		SingleRepo: "owner/single-repo",
	}

	repos, err := p.ListRepos(context.Background())
	require.NoError(t, err)
	require.Len(t, repos, 1)
	require.Equal(t, "owner/single-repo", repos[0])
}

func TestPoller_ListIssues_WithLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/myorg/myrepo/issues", r.URL.Path)
		assert.Equal(t, "open", r.URL.Query().Get("state"))
		assert.Equal(t, "claude", r.URL.Query().Get("labels"))

		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("Real issue")},
			{Number: github.Ptr(2), Title: github.Ptr("PR disguised"), PullRequestLinks: &github.PullRequestLinks{}},
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(issues))
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
		Label:  "claude",
	}

	issues, err := p.ListIssues(context.Background(), "myorg/myrepo")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, 1, issues[0].GetNumber())
}

func TestPoller_ListIssues_WithAuthor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("By trusted"), User: &github.User{Login: github.Ptr("trusteduser")}},
			{Number: github.Ptr(2), Title: github.Ptr("By stranger"), User: &github.User{Login: github.Ptr("stranger")}},
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(issues))
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
		Author: "trusteduser",
	}

	issues, err := p.ListIssues(context.Background(), "myorg/myrepo")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, 1, issues[0].GetNumber())
}

func TestPoller_ListIssues_NoLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.URL.Query().Get("labels"))

		issues := []*github.Issue{
			{Number: github.Ptr(1), Title: github.Ptr("Any issue")},
		}

		w.Header().Set("Content-Type", "application/json")
		assert.NoError(t, json.NewEncoder(w).Encode(issues))
	}))
	defer srv.Close()

	p := &Poller{
		Client: testClient(srv),
	}

	issues, err := p.ListIssues(context.Background(), "myorg/myrepo")
	require.NoError(t, err)
	require.Len(t, issues, 1)
}
