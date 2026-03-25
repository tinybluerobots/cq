# cq

Autonomous GitHub issue processor powered by [Claude](https://claude.ai). Watches repos for labeled issues, dispatches Claude to resolve them, and opens PRs with the fixes.

## Install

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
# Watch all repos in an org
cq watch --org myorg

# Watch a single repo
cq watch --repo owner/repo

# Watch current directory's repo
cq watch
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--org` | | GitHub org to watch (all non-archived source repos) |
| `--repo` | | Single `owner/repo` to watch |
| `--label` | `cq` | Only process issues with this label |
| `--strategy` | `pr` | Git strategy: `pr` (branch + PR) or `direct` (push to default branch) |
| `--interval` | `30s` | Polling interval |
| `--workers` | `5` | Max concurrent repo workers |
| `--workspace` | `~/.cq/repos` | Directory for cloned repos |
| `--max-retries` | `3` | Max retry attempts per issue |
| `--log-file` | | Log file path (defaults to stdout) |
| `--ntfy-topic` | | [ntfy.sh](https://ntfy.sh) topic for error notifications |

### Authentication

Requires a GitHub token. Set `GITHUB_TOKEN` or run `gh auth login`.

## How It Works

1. **Polls** GitHub API for open issues with the target label
2. **Clones** the repo (or pulls if already cloned)
3. **Dispatches** Claude to read the issue, implement a fix, and run tests
4. **Opens a PR** (or pushes directly, depending on strategy)
5. **Tracks state** in `~/.cq/state.json` to avoid re-processing

Each repo gets at most one concurrent worker to prevent conflicts. Failed issues are retried up to `--max-retries` times.

## Per-Issue Configuration

Override defaults per issue by adding a config block to the issue body:

```markdown
<!-- cq
strategy: direct
branch: custom-branch-name
-->
```

| Key | Values | Description |
|-----|--------|-------------|
| `strategy` | `pr`, `direct` | Override the default git strategy |
| `branch` | any string | Custom branch name (default: `cq/issue-{N}`) |

## Development

Requires [mise](https://mise.jdx.dev):

```bash
mise run all      # format + lint + test
mise run lint     # golangci-lint
mise run test     # gotestsum
mise run build    # build binary
```
