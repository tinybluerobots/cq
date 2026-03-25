# claude-afk: Go Rewrite Design Spec

## Overview

Rewrite `claude-afk` from a bash script to a Go CLI tool that watches GitHub org (or single repo) issues and autonomously processes them using the `claude` CLI.

## CLI Interface

```
claude-afk watch [flags]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--org` | (none) | Watch all non-archived source repos in a GitHub org |
| `--repo` | (none) | Watch a single `owner/repo`. Falls back to current dir's repo if neither `--org` nor `--repo` set |
| `--label` | `claude-afk` | Only process issues with this label |
| `--strategy` | `pr` | Default git strategy: `pr` (branch + PR) or `direct` (push to default branch) |
| `--interval` | `30s` | Polling interval |
| `--workers` | `5` | Max concurrent repos being processed |
| `--workspace` | `~/.claude-afk/repos` | Directory for cloned repos |
| `--max-retries` | `3` | Max retry attempts per issue before marking failed |
| `--log-file` | (none) | Optional log file path (always logs to stdout) |
| `--ntfy-topic` | (none) | ntfy.sh topic for error notifications (or `NTFY_TOPIC` env var) |

## Architecture

### Three layers

1. **Poller** — On each interval tick, queries GitHub API for repos (if org mode) then open issues with the target label. Filters out issues already tracked in state file as completed/failed. Emits a list of actionable issues.

2. **Dispatcher** — Maintains a worker pool bounded by `--workers`. Enforces one-issue-at-a-time per repo constraint. If a repo already has an in-flight worker, its issues are skipped until the current one finishes.

3. **Worker** — Processes a single issue end-to-end:
   1. Clone repo (shallow) or pull if already cloned
   2. Parse `<!-- claude-afk -->` config block from issue body
   3. Determine git strategy (per-issue override or CLI default)
   4. If PR strategy: create branch `claude-afk/issue-{number}`, checkout
   5. Shell out to `claude -p` with issue context, stream JSON output to structured logger
   6. If PR strategy: push branch, open PR via GitHub API
   7. If direct strategy: push to default branch
   8. Update state file with result
   9. On error: send ntfy.sh notification

### Project Structure

```
claude-afk/
├── main.go
├── cmd/
│   ├── root.go              # cobra root command, global flags
│   └── watch.go             # watch subcommand
├── internal/
│   ├── poller/
│   │   └── poller.go        # GitHub polling, issue discovery
│   ├── worker/
│   │   └── worker.go        # issue processing, claude execution
│   ├── config/
│   │   └── config.go        # CLI config struct + per-issue config parsing
│   ├── state/
│   │   └── state.go         # state.json persistence
│   └── notify/
│       └── notify.go        # ntfy.sh notifications
├── go.mod
└── go.sum
```

### Dependencies

- `github.com/spf13/cobra` — CLI framework
- `github.com/google/go-github/v69/github` — GitHub API client
- `golang.org/x/oauth2` — GitHub token auth
- `log/slog` (stdlib) — structured logging
- `gopkg.in/yaml.v3` — parsing per-issue config blocks

### GitHub Authentication

Uses `GITHUB_TOKEN` env var via oauth2 static token source. Falls back to `gh auth token` if env var not set.

## Concurrency Model

```
main goroutine
  └── poller (ticker loop)
        └── dispatcher
              ├── worker goroutine: owner/repo-a (issue #12)
              ├── worker goroutine: owner/repo-b (issue #7)
              └── worker goroutine: owner/repo-c (issue #34)
```

- Dispatcher uses a semaphore (buffered channel) sized to `--workers`
- Per-repo mutex map prevents concurrent issues in the same repo
- Workers are fire-and-forget goroutines; results written to state file via mutex-protected writer

## Per-Issue Configuration

Optional HTML comment block in issue body:

```markdown
<!-- claude-afk
strategy: pr
branch: fix-the-thing
-->
```

- Extracted via regex: `<!--\s*claude-afk\s*\n(.*?)\n-->`
- Parsed as YAML
- Recognized keys: `strategy` (pr|direct), `branch` (string)
- Unknown keys silently ignored
- Missing block = use CLI defaults

## State File

Location: `~/.claude-afk/state.json`

```json
{
  "issues": {
    "owner/repo#42": {
      "status": "completed|failed|in_progress",
      "attempts": 1,
      "last_attempt": "2026-03-25T22:00:00Z",
      "pr_url": "https://github.com/owner/repo/pull/99",
      "error": ""
    }
  }
}
```

- Read on startup, written after each issue completes
- Mutex-protected for concurrent access
- `in_progress` issues from a previous crashed run are reset to `pending` on startup
- Issues with `attempts >= max-retries` and status `failed` are skipped

## Claude Invocation

```bash
claude -p \
  --dangerously-skip-permissions \
  --output-format stream-json \
  "<prompt>"
```

Prompt template:

```
You are working on GitHub issue #{number} in {repo}.

Issue title: {title}
Issue body:
{body}

Steps:
1. Read the issue carefully. Implement the changes needed to resolve it.
2. Run tests (check package.json / Makefile / go.mod for how).
3. Commit with a descriptive message referencing the issue number.
4. Output: ISSUE_RESOLVED #{number}

If you cannot resolve this issue, output: ISSUE_FAILED #{number} <reason>
```

- Executed with cwd set to the repo clone directory
- Stream JSON output parsed line-by-line for logging
- Exit code 0 + `ISSUE_RESOLVED` = success
- Exit code non-zero or `ISSUE_FAILED` = failure

## Error Handling

| Scenario | Action |
|----------|--------|
| GitHub API rate limit | Exponential backoff, log warning, continue polling |
| Clone failure | Mark issue attempt failed, continue to next |
| Claude non-zero exit | Mark failed attempt, retry next cycle up to max-retries |
| State file corruption | Log warning, rebuild from empty |
| Network error during push | Mark failed attempt, retry next cycle |

## Notifications

- ntfy.sh POST to configured topic on: issue processing errors, worker panics
- Default topic from env var `NTFY_TOPIC` or `--ntfy-topic` flag
- No notification on success (keep it quiet unless something's wrong)

## Testing Strategy

- Unit tests for config parsing, state file operations, per-issue config extraction
- Integration test for poller using a mock GitHub API server
- Integration test for worker using a mock claude binary (shell script that echoes expected output)
