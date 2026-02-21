# Golden Principles

These are the laws of the codebase. Each is enforced mechanically by linters, CI, or structural conventions.

| # | Principle | Enforced By |
|---|-----------|-------------|
| 1 | Every error must be checked | `errcheck` linter |
| 2 | Errors wrapped with context (`fmt.Errorf("x: %w", err)`) | `revive`, code review |
| 3 | `context.Context` as first parameter | `revive/context-as-argument` |
| 4 | No global mutable state — DI via interfaces | `gocritic`, structural convention |
| 5 | Secrets never in logs, encrypted at rest | `gosec`, code review |
| 6 | Handler → Service → Repository (no layer skipping) | structural convention |
| 7 | Shared code in `pkg/`, service code in `services/*/internal/` | Go module boundaries |
| 8 | Tool naming: `{platform}__{action}` | orchestrator tool registry |
| 9 | Tests before code (TDD) | `WORKFLOW.md` process |
| 10 | All forms: react-hook-form + zod | ESLint, code review |
| 11 | Tailwind classes only, no inline styles | code review |
| 12 | Use `t.Setenv` in tests, not `os.Setenv` | code review |
| 13 | No unused variables or imports | `unused`, `@typescript-eslint/no-unused-vars` |
| 14 | Consistent type imports in TypeScript | `@typescript-eslint/consistent-type-imports` |
| 15 | Code formatted before commit | `gofmt`, `prettier`, `lefthook` pre-commit |

## How Enforcement Works

### Go (backend)

**`.golangci.yml`** configures 20+ linters that run:
- Locally: `make lint` or `lefthook` pre-commit hook
- CI: `golangci-lint-action` on every PR

Key linter groups:
- **Core correctness:** errcheck, govet, staticcheck, typecheck
- **Style consistency:** revive, goimports, gofmt, whitespace
- **Code quality:** gocritic (opinionated), unparam, prealloc
- **Security:** gosec, bodyclose

### Frontend (Next.js)

**ESLint** (`.eslintrc.json`) + **Prettier** (`.prettierrc.json`):
- Locally: `pnpm lint` + `pnpm exec prettier --check .`
- CI: both run in the `frontend` job
- Pre-commit: `lefthook` runs both on staged files

### Process

**`WORKFLOW.md`** enforces:
- Design before code (brainstorming → plan → implement)
- TDD cycle (RED → GREEN → REFACTOR)
- Code review after each task
- No merge without passing lint + tests
