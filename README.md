# issuebot

[![Go Version](https://img.shields.io/badge/Go-1.26-blue?logo=go)](https://pkg.go.dev/github.com/tinybluerobots/issuebot)

Autonomous GitHub issue processor powered by any CLI tool. Watches repos for open issues, dispatches a command to resolve them, and pushes fixes or opens PRs.

## Install

Download a prebuilt binary from the [latest release](https://github.com/tinybluerobots/issuebot/releases/latest).

Or via `go install`:

```bash
go install github.com/tinybluerobots/issuebot@latest
```

Or build from source:

```bash
git clone https://github.com/tinybluerobots/issuebot.git
cd issuebot
go build -o issuebot .
```

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

# Push fixes directly instead of opening PRs
issuebot --repo owner/repo --strategy commit --command "copilot -p {prompt} --yolo"

# Poll every 5 minutes with 10 workers
issuebot --org myorg --interval 5m --workers 10 --command "gemini -p {prompt} --yolo"

# Get notified on failures via ntfy.sh
issuebot --repo owner/repo --ntfy-topic my-alerts --command "copilot -p {prompt} --yolo"

# Log to a file for background operation
issuebot --org myorg --log-file ~/.issuebot/issuebot.log --command "claude -p {prompt} --dangerously-skip-permissions"

# Use any command that reads from stdin
issuebot --local --command "./my-issue-handler.sh"
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--org` | | GitHub org to watch (all non-archived source repos) |
| `--repo` | | Single `owner/repo` to watch |
| `--label` | | Only process issues with this label (default: all open issues) |
| `--strategy` | `pr` | Git strategy: `pr` (branch + PR) or `commit` (push to default branch) |
| `--interval` | `30s` | Polling interval |
| `--workers` | `5` | Max concurrent repo workers |
| `--workspace` | `~/.issuebot/repos` | Directory for cloned repos |
| `--local` | `false` | Use current directory instead of cloning |
| `--command` | **(required)** | Command to run (prompt via stdin or `{prompt}` placeholder) |
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

On first run, issuebot writes a default prompt template to `~/.issuebot/prompt.tmpl`. Edit it to customise how issues are presented to your command. Available template fields:

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

The `--command` flag is required. By default, the prompt is passed via stdin. If your tool needs it as an argument, use `{prompt}` as a placeholder (stdin is not used when `{prompt}` is present):

```bash
# Claude Code
issuebot --command "claude -p {prompt} --dangerously-skip-permissions"

# GitHub Copilot
issuebot --command "copilot -p {prompt} --yolo"

# Google Gemini CLI
issuebot --command "gemini -p {prompt} --yolo"

# Any tool that reads stdin
issuebot --command "./my-issue-handler.sh"
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

## Development

Requires [mise](https://mise.jdx.dev):

```bash
mise run all      # format + lint + test
mise run lint     # golangci-lint
mise run test     # gotestsum
mise run build    # build binary
```
