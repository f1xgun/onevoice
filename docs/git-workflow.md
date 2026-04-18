# Git Workflow — OneVoice

## Commit Messages

**Format:** `<type>: <subject>` (lowercase, no trailing period, imperative mood).

**Types:** `feat`, `fix`, `refactor`, `docs`, `test`, `chore`, `perf`, `ci`.

Examples:

```
feat: add Google Business Profile integration
fix: prevent token refresh race condition
refactor: extract JWT logic to pkg/auth
docs: add API authentication guide
test: add unit tests for orchestrator
chore: update dependencies
```

For commits that touch a specific module, scope it:

```
fix(agent-yandex-business): resolve crop dialog on photo upload
feat(orchestrator): register yandex_business__create_post tool
```

Do **not** include `Co-Authored-By:` lines — project preference.

## Branch Naming

- `main` — production-ready; force-push forbidden.
- `feat/<short-name>` — new feature.
- `fix/<short-name>` — bug fix.
- `refactor/<short-name>` — refactor without behavior change.
- `chore/<short-name>` — tooling, deps, CI.

## Worktrees

Long-running work happens in dedicated git worktrees under `.claude/worktrees/` (the session will offer to clean them up on exit). This is preferred over long-lived branches on the main checkout — it keeps `main` usable for hotfixes.

## Pre-commit Hooks

`lefthook.yml` runs formatters and linters on staged files:

- Go: `gofmt`, `goimports`, `golangci-lint`
- Frontend: `prettier`, `eslint`

Do **not** bypass hooks (`--no-verify`) unless explicitly authorized. If a hook fails, fix the underlying issue and create a *new* commit — don't `--amend` after a hook failure (the commit didn't actually land).

## PR Checklist

Before merging:

- [ ] Tests pass (`make test-all`)
- [ ] Lint passes (`make lint-all`)
- [ ] No secrets in code or logs
- [ ] Errors wrapped with context (Go)
- [ ] Database migrations included if the schema changed
- [ ] Breaking changes noted in the PR description
- [ ] Affected docs updated (`AGENTS.md`, `docs/*.md`)

## Merge Policy

- Squash merges for feature branches — one commit per PR on `main`.
- Preserve per-commit history only for documented refactors where the intermediate states are intentionally atomic.
