BEGIN;

-- The integration-test schema never had the legacy subscriptions table, so the
-- reverse of the up migration is simply dropping the business-keyed table.
DROP TABLE IF EXISTS subscriptions;

COMMIT;
