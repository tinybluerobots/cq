#!/bin/bash
set -euo pipefail

usage() {
  echo "Usage: claude-afk.sh [--org <org>] [--repo <owner/repo>] [--interval <seconds>] [--log <file>] [--workspace <dir>]"
  echo ""
  echo "Modes:"
  echo "  --org <org>          Watch all repos in a GitHub org"
  echo "  --repo <owner/repo>  Watch a single repo (default: current directory's repo)"
  echo ""
  echo "Options:"
  echo "  --interval <seconds>  Polling interval (default: 30)"
  echo "  --log <file>          Log file (default: claude-afk.log)"
  echo "  --workspace <dir>     Directory for cloned repos (default: ~/.claude-afk/repos)"
  exit 1
}

POLL_INTERVAL=30
LOGFILE="claude-afk.log"
WORKSPACE="${HOME}/.claude-afk/repos"
ORG=""
SINGLE_REPO=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --org) ORG="$2"; shift 2 ;;
    --repo) SINGLE_REPO="$2"; shift 2 ;;
    --interval) POLL_INTERVAL="$2"; shift 2 ;;
    --log) LOGFILE="$2"; shift 2 ;;
    --workspace) WORKSPACE="$2"; shift 2 ;;
    --help|-h) usage ;;
    *) echo "Unknown option: $1"; usage ;;
  esac
done

TMPFILE=$(mktemp)
trap 'rm -f "$TMPFILE"' EXIT

log() {
  local msg="[$(date '+%Y-%m-%d %H:%M:%S')] $*"
  echo "$msg"
  echo "$msg" >> "$LOGFILE"
}

# Get the local path for a repo, cloning if needed
ensure_repo() {
  local repo="$1"
  local repo_dir="${WORKSPACE}/${repo}"
  if [ ! -d "$repo_dir" ]; then
    log "Cloning ${repo}..."
    mkdir -p "$(dirname "$repo_dir")"
    gh repo clone "$repo" "$repo_dir" -- --depth=1
  else
    git -C "$repo_dir" pull --ff-only --quiet 2>/dev/null || true
  fi
  echo "$repo_dir"
}

# List repos to watch
get_repos() {
  if [ -n "$ORG" ]; then
    gh repo list "$ORG" --no-archived --source --limit 500 --json nameWithOwner -q '.[].nameWithOwner'
  elif [ -n "$SINGLE_REPO" ]; then
    echo "$SINGLE_REPO"
  else
    gh repo view --json nameWithOwner -q '.nameWithOwner'
  fi
}

# Process a single issue in a repo
process_issue() {
  local repo="$1"
  local repo_dir="$2"
  local issue_num="$3"
  local issue_title="$4"

  log "=== [${repo}] Issue #${issue_num}: ${issue_title} ==="
  log "Starting claude for ${repo}#${issue_num}..."

  claude -p \
    --dangerously-skip-permissions \
    --output-format stream-json \
    "You are working through a GitHub issue queue autonomously.
The repo is: ${repo} (local path: ${repo_dir})

Steps:
1. Run: gh issue view ${issue_num} --repo ${repo} --json number,title,body
2. Read the issue. Implement the changes needed to resolve it.
3. Run tests (check package.json / Makefile / go.mod for how).
4. Commit with a descriptive message referencing the issue number.
5. Push to origin.
6. Close the issue: gh issue close ${issue_num} --repo ${repo} --comment 'Resolved in \$(git rev-parse --short HEAD)'
7. Output: ISSUE_RESOLVED #${issue_num}" < /dev/null 2>&1 | while IFS= read -r line; do
      type=$(echo "$line" | jq -r '.type // empty' 2>/dev/null)
      if [ "$type" = "assistant" ]; then
        text=$(echo "$line" | jq -r '.message.content[]? | select(.type=="text") | .text' 2>/dev/null)
        [ -n "$text" ] && log "Claude [${repo}]: $text"
      elif [ "$type" = "result" ]; then
        text=$(echo "$line" | jq -r '.result // empty' 2>/dev/null)
        [ -n "$text" ] && log "Result [${repo}]: $text"
        echo "$text" > "$TMPFILE"
      fi
    done
}

# Main loop
mkdir -p "$WORKSPACE"

if [ -n "$ORG" ]; then
  log "Watching all repos in org '${ORG}' (polling every ${POLL_INTERVAL}s)..."
elif [ -n "$SINGLE_REPO" ]; then
  log "Watching ${SINGLE_REPO} (polling every ${POLL_INTERVAL}s)..."
else
  log "Watching current repo (polling every ${POLL_INTERVAL}s)..."
fi
log "Logging to $LOGFILE"

while true; do
  REPOS=$(get_repos)
  if [ -z "$REPOS" ]; then
    log "No repos found."
    sleep "$POLL_INTERVAL"
    continue
  fi

  FOUND_ISSUE=false
  while IFS= read -r repo; do
    ISSUES=$(gh issue list --repo "$repo" --state open --limit 1 --json number,title 2>/dev/null || echo "[]")
    if [ "$ISSUES" = "[]" ] || [ -z "$ISSUES" ]; then
      continue
    fi

    ISSUE_NUM=$(echo "$ISSUES" | jq -r '.[0].number')
    ISSUE_TITLE=$(echo "$ISSUES" | jq -r '.[0].title')

    # Get or clone the repo
    if [ -z "$ORG" ] && [ -z "$SINGLE_REPO" ]; then
      REPO_DIR="$(pwd)"
    else
      REPO_DIR=$(ensure_repo "$repo")
    fi

    (cd "$REPO_DIR" && process_issue "$repo" "$REPO_DIR" "$ISSUE_NUM" "$ISSUE_TITLE")
    FOUND_ISSUE=true
  done <<< "$REPOS"

  if [ "$FOUND_ISSUE" = false ]; then
    log "No open issues across watched repos."
  fi

  sleep "$POLL_INTERVAL"
done
