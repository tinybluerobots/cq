# cq

[![Go Version](https://img.shields.io/badge/Go-1.26-blue?logo=go)](https://pkg.go.dev/github.com/tinybluerobots/cq)

Autonomous GitHub issue processor powered by any CLI tool. Watches repos for open issues, dispatches a command to resolve them, and pushes fixes or opens PRs.

## Install

Download a prebuilt binary from the [latest release](https://github.com/tinybluerobots/cq/releases/latest).

Or via `go install`:

```bash
go install github.com/tinybluerobots/cq@latest
```

Or build from source:

```bash
git clone https://github.com/tinybluerobots/cq.git
cd cq
go build -o cq .
```

## Usage

```bash
# Watch current directory's repo
cq

# Work directly in current directory (no clone)
cq --local

# Watch a single repo
cq --repo owner/repo

# Watch all repos in an org
cq --org myorg

# Only process issues labelled "cq"
cq --repo owner/repo --label cq

# Preview what cq would do without pushing
cq --repo owner/repo --dry-run

# Push fixes directly instead of opening PRs
cq --repo owner/repo --strategy commit

# Use a custom command instead of Claude
cq --local --command "my-ai-tool"

# Poll every 5 minutes with 10 workers
cq --org myorg --interval 5m --workers 10

# Get notified on failures via ntfy.sh
cq --repo owner/repo --ntfy-topic my-alerts

# Log to a file for background operation
cq --org myorg --log-file ~/.cq/cq.log
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
| `--workspace` | `~/.cq/repos` | Directory for cloned repos |
| `--local` | `false` | Use current directory instead of cloning |
| `--command` | | Custom command to run (prompt via stdin, default: Claude CLI) |
| `--prompt-file` | `~/.cq/prompt.tmpl` | Path to prompt template file |
| `--dry-run` | `false` | Run command but skip push/PR (print diff instead) |
| `--max-retries` | `3` | Max retry attempts per issue |
| `--log-file` | | Log file path (defaults to stdout) |
| `--ntfy-topic` | | [ntfy.sh](https://ntfy.sh) topic for error notifications |

### Authentication

Requires a GitHub token. Set `GITHUB_TOKEN` or run `gh auth login`.

## How It Works

1. **Polls** GitHub API for open issues (optionally filtered by label)
2. **Clones** the repo (or uses current dir with `--local`)
3. **Dispatches** a command (default: Claude CLI) with the issue as a prompt
4. **Opens a PR** or pushes directly, depending on strategy
5. **Tracks state** in `~/.cq/state.json` to avoid re-processing

Each repo gets at most one concurrent worker to prevent conflicts. Failed issues are retried up to `--max-retries` times.

## Prompt Template

On first run, cq writes a default prompt template to `~/.cq/prompt.tmpl`. Edit it to customise how issues are presented to your command. Available template fields:

| Field | Description |
|-------|-------------|
| `{{.Repo}}` | Repository full name (`owner/repo`) |
| `{{.Number}}` | Issue number |
| `{{.Title}}` | Issue title |
| `{{.Body}}` | Issue body |
| `{{.Author}}` | Issue author's GitHub username |
| `{{.Labels}}` | Comma-separated list of issue labels |
| `{{.DefaultBranch}}` | Repository default branch |

## Custom Commands

Use `--command` to swap Claude for any CLI tool. By default, the prompt is passed via stdin. If your tool needs it as an argument, use `{prompt}` as a placeholder (stdin is not used when `{prompt}` is present):

```bash
# Use GitHub Copilot
cq --local --command "copilot -p {prompt} --yolo"

# Use Google Gemini CLI
cq --local --command "gemini -p {prompt} --yolo"

# Pipe to a script
cq --local --command "./my-issue-handler.sh"
```

## Per-Issue Configuration

Override defaults per issue by adding a config block to the issue body:

```markdown
<!-- cq
strategy: commit
branch: custom-branch-name
-->
```

Example with post-command to request review after PR creation:

```markdown
<!-- cq
strategy: pr
post-command: gh pr edit $PR_NUMBER --add-reviewer octocat
-->
```

| Key | Values | Description |
|-----|--------|-------------|
| `strategy` | `pr`, `commit` | Override the default git strategy |
| `branch` | any string | Custom branch name (default: `cq/issue-{N}`) |
| `post-command` | any command | Shell command to run after PR creation (`$PR_URL` and `$PR_NUMBER` available) |

## Development

Requires [mise](https://mise.jdx.dev):

```bash
mise run all      # format + lint + test
mise run lint     # golangci-lint
mise run test     # gotestsum
mise run build    # build binary
```
