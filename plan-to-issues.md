# Plan to Issues

Convert an implementation plan into precise, self-contained GitHub issues that agents can implement without additional context.

## Core Principle

**Issues must contain the HOW, not just the WHAT.** An agent picking up an issue should be able to implement it from the issue body alone, without reading the plan document. Every code snippet, file path, and command from the plan goes into the issue.

## Process

1. **Create a plan** — use your AI tool to design the feature, fix, or refactor
2. **Parse into vertical slices** — each task becomes one issue. A vertical slice cuts through all layers needed (not horizontal like "all tests" or "all CSS")
3. **Review the breakdown** — check granularity, dependencies, and whether any slices should be merged or split
4. **Create issues in dependency order** — blockers first, so you can reference real issue numbers

For each slice, extract from the plan:
- **Files** to create/modify (exact paths)
- **Code snippets** (complete, copy-pasteable)
- **Commands** to run (with expected output)
- **Dependencies** on other slices

## Issue Template

```markdown
## Parent Plan

Link or reference to the plan document (Task N)

## What to Build

One paragraph: what this slice delivers end-to-end.

## Implementation

### Files
- Create: `exact/path/to/new_file.go`
- Modify: `exact/path/to/existing.go`
- Test: `exact/path/to/test_file.go`

### Steps

1. **Step description**

   ```lang
   // Complete code snippet from the plan
   // Not pseudocode. Not "add validation here".
   // The actual code.
   ```

2. **Next step**

   Run: `exact command with flags`
   Expected: what should happen

### Verification

```bash
command to verify this slice works
```

## Acceptance Criteria

- [ ] Criterion 1
- [ ] Criterion 2

## Blocked by

- #N (title) — or "None"
```

## Slice Types

- **AFK** — agent can implement autonomously
- **HITL** — needs human decision or review

## Red Flags

| Symptom | Fix |
|---------|-----|
| Issue says "implement the helper" without code | Copy the code from the plan into the issue |
| Issue says "modify the file" without saying how | Include the exact edit: what to add, where |
| Issue has acceptance criteria but no steps | Add implementation steps with code snippets |
| Issue references "see plan for details" | Issue must be self-contained. Inline the details |
| Dependencies reference slice names not issue numbers | Create in order, use real `#N` references |

## What NOT to Include

- The full plan document (link to it instead)
- Steps from other slices
- Architecture discussion (that's in the spec)
- Alternative approaches considered
