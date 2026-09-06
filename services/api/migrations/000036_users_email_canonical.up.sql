BEGIN;
LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE;
DO $$
BEGIN
    IF EXISTS (
        SELECT lower(btrim(email)) FROM users
        GROUP BY lower(btrim(email)) HAVING count(*) > 1
    ) THEN
        RAISE EXCEPTION 'Resolve duplicate canonical user emails before applying this migration';
    END IF;
END $$;
UPDATE users SET email = lower(btrim(email));
CREATE UNIQUE INDEX users_email_canonical_unique ON users (lower(btrim(email)));
COMMIT;
