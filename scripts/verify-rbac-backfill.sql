-- scripts/verify-rbac-backfill.sql
-- Phase 1 v2.0 RBAC: post-deploy invariants.
-- Each invariant uses `RAISE EXCEPTION` to abort with a non-zero exit code
-- when violated. Driven by `make verify-rbac-backfill`, which runs psql with
-- `-v ON_ERROR_STOP=1` so any RAISE EXCEPTION terminates the script.

-- INVARIANT 1: every business with user_id IS NOT NULL has a matching owner member
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM businesses b
        WHERE b.user_id IS NOT NULL
          AND NOT EXISTS (
              SELECT 1 FROM business_members m
              WHERE m.business_id = b.id
                AND m.user_id = b.user_id
                AND m.role_id = '00000000-0000-0000-0000-000000000001'
          )
    ) THEN
        RAISE EXCEPTION 'INVARIANT 1 FAILED: business(es) with user_id but no matching owner member';
    END IF;
END $$;

-- INVARIANT 2: no duplicate (business_id, user_id) rows in business_members
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM business_members
        GROUP BY business_id, user_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'INVARIANT 2 FAILED: duplicate (business_id, user_id) rows in business_members';
    END IF;
END $$;

-- INVARIANT 3: every business has at least one owner member
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM businesses b
        WHERE NOT EXISTS (
            SELECT 1 FROM business_members m
            WHERE m.business_id = b.id
              AND m.role_id = '00000000-0000-0000-0000-000000000001'
        )
    ) THEN
        RAISE EXCEPTION 'INVARIANT 3 FAILED: business(es) with no owner member';
    END IF;
END $$;

-- All three invariants passed
\echo 'verify-rbac-backfill: all invariants OK'
