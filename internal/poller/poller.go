package poller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/go-github/v69/github"
	"github.com/tinybluerobots/issuebot/internal/ratelimit"
)

// ErrInvalidRepoFormat is returned when a repo string is not in "owner/name" format.
var ErrInvalidRepoFormat = errors.New("invalid repo format, expected owner/name")

// Poller polls GitHub for repositories and issues matching configured criteria.
type Poller struct {
	Client     *github.Client
	Org        string
	SingleRepo string
	Label      string
	Author     string
}

// ListRepos returns repository full names to monitor. If SingleRepo is set,
// it returns just that without making any API calls. If Org is set, it
// paginates through all source repositories, filtering out archived and forked repos.
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
			if ratelimit.Wait(ctx, err) {
				continue
			}

			return nil, fmt.Errorf("listing repos for org %s: %w", p.Org, err)
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

// ListIssues returns open issues (not PRs) for a repo that match the configured label.
func (p *Poller) ListIssues(ctx context.Context, repo string) ([]*github.Issue, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: %q", ErrInvalidRepoFormat, repo)
	}

	owner, name := parts[0], parts[1]

	var allIssues []*github.Issue

	opts := &github.IssueListByRepoOptions{
		State:       "open",
		ListOptions: github.ListOptions{PerPage: 100},
	}

	if p.Label != "" {
		opts.Labels = []string{p.Label}
	}

	for {
		issues, resp, err := p.Client.Issues.ListByRepo(ctx, owner, name, opts)
		if err != nil {
			if ratelimit.Wait(ctx, err) {
				continue
			}

			return nil, fmt.Errorf("listing issues for %s: %w", repo, err)
		}

		for _, issue := range issues {
			if issue.PullRequestLinks != nil {
				continue
			}

			if p.Author != "" && issue.GetUser().GetLogin() != p.Author {
				continue
			}

			allIssues = append(allIssues, issue)
		}

		if resp.NextPage == 0 {
			break
		}

		opts.Page = resp.NextPage
	}

	return allIssues, nil
}
