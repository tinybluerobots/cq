# issuebot

[![Go Version](https://img.shields.io/badge/Go-1.26-blue?logo=go)](https://pkg.go.dev/github.com/tinybluerobots/issuebot)

Autonomous GitHub issue processor powered by any CLI tool. Watches repos for open issues, dispatches a command to resolve them, and pushes fixes or opens PRs.

## Install

Via [Homebrew](https://brew.sh):

```bash
brew install --cask tinybluerobots/tap/issuebot
```

Via [mise](https://mise.jdx.dev):

```bash
mise use -g go:github.com/tinybluerobots/issuebot
```

Via `go install`:

```bash
go install github.com/tinybluerobots/issuebot@latest
```

Or download a prebuilt binary from the [latest release](https://github.com/tinybluerobots/issuebot/releases/latest).

## Usage

```bash
# Watch current directory's repo with Claude
issuebot --command "claude -p {prompt} --dangerously-skip-permissions"

# Work directly in current directory (no clone)
issuebot --local --command "claude -p {prompt} --dangerously-skip-permissions"

# Watch a single repo with Copilot
issuebot --repo owner/repo --command "copilot -p {prompt} --yolo"

# Watch all repos in an org with Gemini
issuebot --org myorg --command "gemini -p {prompt} --yolo"

# Only process issues labelled "bug"
issuebot --repo owner/repo --label bug --command "claude -p {prompt} --dangerously-skip-permissions"

# Preview what the command would do without pushing
issuebot --repo owner/repo --dry-run --command "claude -p {prompt} --dangerously-skip-permissions"

# Open PRs instead of pushing directly
issuebot --repo owner/repo --strategy pr --command "copilot -p {prompt} --yolo"

# Poll every 5 minutes with 10 workers
issuebot --org myorg --interval 5m --workers 10 --command "gemini -p {prompt} --yolo"

# Get notified on failures via ntfy.sh
issuebot --repo owner/repo --ntfy-topic my-alerts --command "copilot -p {prompt} --yolo"

# Log to a file for background operation
issuebot --org myorg --log-file ~/.issuebot/issuebot.log --command "claude -p {prompt} --dangerously-skip-permissions"

# Use a custom script
issuebot --local --command "./my-issue-handler.sh {prompt}"
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--org` | | GitHub org to watch (all non-archived source repos) |
| `--repo` | | Single `owner/repo` to watch |
| `--label` | | Only process issues with this label (default: all open issues) |
| `--strategy` | `commit` | Git strategy: `pr` (branch + PR) or `commit` (push to default branch) |
| `--interval` | `30s` | Polling interval |
| `--workers` | `5` | Max concurrent repo workers |
| `--workspace` | `~/.issuebot/repos` | Directory for cloned repos |
| `--local` | `false` | Use current directory instead of cloning |
| `--command` | **(required)** | Command to run (`{prompt}` is replaced with the rendered prompt) |
| `--prompt-file` | `~/.issuebot/prompt.tmpl` | Path to prompt template file |
| `--dry-run` | `false` | Run command but skip push/PR (print diff instead) |
| `--max-retries` | `3` | Max retry attempts per issue |
| `--log-file` | | Log file path (defaults to stdout) |
| `--ntfy-topic` | | [ntfy.sh](https://ntfy.sh) topic for error notifications |


### Authentication

Requires a GitHub token. Set `GITHUB_TOKEN` or run `gh auth login`.

## How It Works

1. **Polls** GitHub API for open issues (optionally filtered by label)
2. **Clones** the repo (or uses current dir with `--local`)
3. **Dispatches** the configured command with the issue as a prompt
4. **Opens a PR** or pushes directly, depending on strategy
5. **Tracks state** in `~/.issuebot/state.json` to avoid re-processing

Each repo gets at most one concurrent worker to prevent conflicts. Failed issues are retried up to `--max-retries` times.

## Prompt Template

The prompt template controls what your command receives as input. On first run, issuebot writes a default template to `~/.issuebot/prompt.tmpl`. Use `--prompt-file` to specify a different path.

The template uses Go's [text/template](https://pkg.go.dev/text/template) syntax. The rendered output replaces `{prompt}` in your `--command`.

**Default template:**

```
You are working through a GitHub issue queue autonomously.
The repo is: {{.Repo}}

GitHub Issue #{{.Number}}: {{.Title}}

{{.Body}}

Steps:
1. Read the issue carefully. Implement the changes needed to resolve it.
2. Run tests (check package.json / Makefile / go.mod for how).
3. Commit with a descriptive message referencing the issue number.
4. Push to origin.
5. Close the issue: gh issue close {{.Number}} --repo {{.Repo}} --comment "Resolved in $(git rev-parse --short HEAD)"
6. Output exactly: ISSUE_RESOLVED #{{.Number}}

If you cannot resolve the issue, output exactly: ISSUE_FAILED #{{.Number}} with a brief explanation.
```

The `ISSUE_FAILED` marker is important — if the command output contains it, issuebot marks the issue as failed and retries later.

**Available fields:**

| Field | Description |
|-------|-------------|
| `{{.Repo}}` | Repository full name (`owner/repo`) |
| `{{.Number}}` | Issue number |
| `{{.Title}}` | Issue title |
| `{{.Body}}` | Issue body |
| `{{.Author}}` | Issue author's GitHub username |
| `{{.Labels}}` | Comma-separated list of issue labels |
| `{{.DefaultBranch}}` | Repository default branch |

## Command Interface

The `--command` flag is required. Use `{prompt}` as a placeholder — issuebot substitutes it with the rendered prompt template:

```bash
# Claude Code
issuebot --command "claude -p {prompt} --dangerously-skip-permissions"

# GitHub Copilot
issuebot --command "copilot -p {prompt} --yolo"

# Google Gemini CLI
issuebot --command "gemini -p {prompt} --yolo"
```

## Per-Issue Configuration

Override defaults per issue by adding a config block to the issue body:

```markdown
<!-- issuebot
strategy: commit
branch: custom-branch-name
-->
```

Example with post-command to request review after PR creation:

```markdown
<!-- issuebot
strategy: pr
post-command: gh pr edit $PR_NUMBER --add-reviewer octocat
-->
```

| Key | Values | Description |
|-----|--------|-------------|
| `strategy` | `pr`, `commit` | Override the default git strategy |
| `branch` | any string | Custom branch name (default: `issuebot/issue-{N}`) |
| `post-command` | any command | Shell command to run after PR creation (`$PR_URL` and `$PR_NUMBER` available) |

## Tips for Best Results

issuebot works best when issues are **atomic and self-contained** — each issue should include enough context for the AI to complete the work without guessing.

**Recommended workflow:**

1. **Plan first** — use your AI tool's planning capabilities to design the feature or fix
2. **Create atomic issues** — break the plan into small, focused issues where each one contains the relevant context (files to change, acceptance criteria, dependencies). This repo includes a [plan-to-issues](plan-to-issues.md) skill you can install locally in your AI tool to automate this
3. **Let issuebot work** — it picks up each issue, resolves it independently, and pushes the result

Vague issues like "refactor the auth system" will produce vague results. An issue that says "extract `validateToken` from `auth.go` into a new `token.go` file, update imports in `server.go` and `middleware.go`, run `mise run test`" gives the AI everything it needs.

## Development

Requires [mise](https://mise.jdx.dev):

```bash
mise run all      # format + lint + test
mise run lint     # golangci-lint
mise run test     # gotestsum
mise run build    # build binary
```
