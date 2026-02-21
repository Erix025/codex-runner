# Contributing

This repository uses a strict issue-to-PR workflow.

## Required Workflow

1. Select one open issue and lock scope (in-scope / out-of-scope).
2. Start from an up-to-date `main`:

```bash
git fetch origin --prune
git checkout main
git pull --ff-only origin main
```

3. Create a dedicated branch from `main`:

```bash
git checkout -b codex/issue-<id>-<slug>
```

4. Implement only changes related to that issue.
5. Before commit, both local gates must pass:

```bash
go test ./...
make build-all
```

6. Use commit subject format:

```text
fix(issue-<id>): <short summary>
```

7. Push branch and open PR to `main`:

```bash
git push -u origin codex/issue-<id>-<slug>
```

8. PR title format:

```text
fix(issue-<id>): <short summary>
```

9. PR body must include:
- Reproduction and root cause
- Fix approach
- Test evidence (`go test ./...` and `make build-all`)
- Risk and rollback plan
- Issue closing line: `Closes #<id>`

10. Merge policy:
- Merge only after CI is green.
- Use squash merge by default.
- Delete remote fix branch after merge.

## Rules Enforced by CI

On pull requests, policy checks enforce:
- Base branch must be `main`
- Branch name must match `codex/issue-<id>-<slug>`
- PR title must match `fix(issue-<id>): ...`
- PR body must contain `Closes #<id>`
- Issue id in branch, title, and `Closes` line must match
