\set ON_ERROR_STOP on
BEGIN TRANSACTION READ ONLY;
WITH collisions AS (
    SELECT lower(btrim(email)) AS canonical_email
    FROM users
    GROUP BY lower(btrim(email))
    HAVING count(*) > 1
)
SELECT c.canonical_email, u.id AS user_id, u.email, u.created_at,
       u.deleted_at,
       (SELECT max(a.created_at) FROM audit_logs a
        WHERE a.user_id = u.id AND a.action = 'auth.login_success') AS last_login,
       (SELECT count(*) FROM business_members m
        WHERE m.user_id = u.id) AS business_count
FROM users u
JOIN collisions c ON lower(btrim(u.email)) = c.canonical_email
ORDER BY c.canonical_email, u.created_at, u.id;
COMMIT;
