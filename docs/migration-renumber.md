# Migration Renumber Recovery (Phase 20 / Plan 20-01)

## Background

Prior to v1.4 Phase 20, two collisions existed in the migration tree:

- `migrations/postgres/` (production path): two files at version `000008` — `audit_log_infrastructure` and `rbac_cleanup`. `golang-migrate` applied whichever the filesystem returned first and silently skipped the other.
- `services/api/migrations/` (integration-test path): two files at version `000007` — `audit_log_infrastructure` and `user_preferred_locale`.

Plan 20-01 reconciled both paths:

| Path | Before | After |
|------|--------|-------|
| `migrations/postgres/000008_audit_log_infrastructure` | 000008 | 000008 (unchanged — kept by tiebreaker) |
| `migrations/postgres/000008_rbac_cleanup` | 000008 | **000009** (renumbered) |
| `migrations/postgres/000009_user_preferred_locale` | 000009 | **000010** (renumbered) |
| `services/api/migrations/000007_audit_log_infrastructure` | 000007 | 000007 (unchanged — kept by tiebreaker) |
| `services/api/migrations/000007_user_preferred_locale` | 000007 | **000008** (renumbered) |

Order-of-application was verified commutative for both collisions (the two files in each pair touch different tables — audit_logs/roles vs businesses/users in prod; audit_logs/roles vs users.preferred_locale in test) so applying them in either order produces the same final schema.

## Are you affected?

Run against each DB you care about:

```sql
SELECT version, dirty FROM schema_migrations;
```

- **Production-path DB** (the one your `docker-compose.yml` migrate service points at): if `version >= 8`, you are affected by the prod-path renumber.
- **Integration-test DB** (the throwaway DB used by `make test-integration`): if `version >= 7`, you are affected by the test-path renumber. Integration test DBs are rebuilt every CI run from scratch via `docker-compose.test.yml`, so most contributors will see no issue — only persistent dev DBs are affected.

## Recovery: development databases

### Prod-path dev DB at version 9 (had both 000008s applied via FS-order accident + 000009_locale on top)

```bash
# Confirm current state first
psql "$DATABASE_URL" -c 'SELECT version, dirty FROM schema_migrations;'
# Expected: version=9, dirty=false

# Force the version table to 000010 (the new top after renumber).
# This tells golang-migrate "the schema is at version 10 as far as you're concerned" —
# safe because the underlying schema has all three migrations applied already (just under
# the old version labels).
migrate -database "$DATABASE_URL" -path migrations/postgres force 10

# Verify
migrate -database "$DATABASE_URL" -path migrations/postgres version
# Expected: 10
```

### Prod-path dev DB at version 8 (only one of the two 000008s applied)

This is the worse case — your DB is missing either `rbac_cleanup` OR `audit_log_infrastructure` and you may not know which.

```sql
-- Detect which 000008 ran:
-- If businesses.user_id column still exists → rbac_cleanup did NOT run
SELECT column_name FROM information_schema.columns
WHERE table_name = 'businesses' AND column_name = 'user_id';

-- If audit_logs.business_id does NOT exist → audit_log_infrastructure did NOT run
SELECT column_name FROM information_schema.columns
WHERE table_name = 'audit_logs' AND column_name = 'business_id';
```

Then manually apply the missing migration's SQL (read the relevant `.up.sql` file and run by hand), then `migrate force 9` (if rbac_cleanup was missing) or `migrate force 8` (if audit_log_infrastructure was missing), then `migrate up` to apply the remaining ones.

### Test-path dev DB at version 7

```bash
migrate -database "$TEST_DATABASE_URL" -path services/api/migrations force 8
migrate -database "$TEST_DATABASE_URL" -path services/api/migrations version
# Expected: 8
```

### Easiest recovery (for any throwaway dev DB)

Just drop and recreate. Most v1.4 dev DBs are docker-compose volumes and have no real data:

```bash
docker compose down -v   # destroys volumes
docker compose up -d postgres
docker compose run --rm migrate up
```

## DO NOT run `migrate force` against production without these steps

Per PITFALLS §11.4: `migrate force` is appropriate ONLY for "the schema is in known state X and the version table just disagrees." It does NOT inspect the DB to confirm.

Before any production `migrate force`:

1. Take a fresh `pg_dump` backup and verify the dump size matches recent backups (sanity check).
2. Confirm `SELECT version, dirty FROM schema_migrations` matches your expectation.
3. Confirm schema column-presence as shown above (`businesses.user_id` absent + `audit_logs.business_id` present = all three pre-renumber migrations ran).
4. Have a peer reviewer sign off on the command.
5. Log the action to the operator audit channel.

## Prevention

`scripts/check-migrations-parity.sh` (added in Plan 20-01) is wired into `make lint-all` and rejects:
- Duplicate version numbers within either path
- Missing companion `down.sql` files

Run it locally before any migration-touching PR:
```bash
make lint-migrations
```
