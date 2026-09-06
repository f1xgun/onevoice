"""Execute collision-report regressions using read-only PostgreSQL CTE fixtures.

Usage: python3 scripts/test-email-collisions.py psql -X -U postgres -d onevoice
A docker compose exec -T postgres psql command can also be supplied as arguments.
"""
import json
from pathlib import Path
import subprocess
import sys

report = Path(__file__).with_name('report-email-collisions.sql').read_text()
query = report[report.index('WITH collisions'):report.index(';\nCOMMIT')]
fixtures = """
users(id, email, created_at, deleted_at) AS (VALUES
 ('keeper', 'Owner@Example.com', '2026-01-01'::timestamptz, NULL::timestamptz),
 ('renamed', ' owner@example.COM ', '2026-02-01'::timestamptz, '2026-09-01'::timestamptz),
 ('single', 'single@example.com', '2026-03-01'::timestamptz, NULL::timestamptz)
),
business_members(user_id) AS (VALUES ('keeper'), ('keeper'), ('renamed')),
audit_logs(user_id, action, created_at) AS (VALUES
 ('keeper', 'auth.login_success', '2026-08-01'::timestamptz),
 ('keeper', 'auth.login_success', '2026-08-02'::timestamptz),
 ('keeper', 'auth.login_failed', '2026-08-03'::timestamptz)
),
"""
command = sys.argv[1:]
if not command:
    raise SystemExit(__doc__)

for name, data, expected_count in [
    ('canonical collisions including deletion grace', fixtures, 2),
    ('approved distinct replacement resolves collision', fixtures.replace(
        ' owner@example.COM ', 'replacement@example.com'), 0),
    ('canonical replacement still collides', fixtures.replace(
        ' owner@example.COM ', 'owner@example.com'), 2),
]:
    fixture_query = query.replace('WITH collisions', 'WITH ' + data + 'collisions', 1)
    sql = ('BEGIN TRANSACTION READ ONLY;\n'
           "SELECT coalesce(json_agg(r), '[]'::json) FROM (" + fixture_query + ') r;\nROLLBACK;\n')
    result = subprocess.run(command + ['-X', '-qAt', '-v', 'ON_ERROR_STOP=1'],
                            input=sql, text=True, capture_output=True, check=True)
    rows = json.loads(result.stdout)
    assert len(rows) == expected_count, name
    if rows:
        assert [r['user_id'] for r in rows] == ['keeper', 'renamed'], name
        assert [r['business_count'] for r in rows] == [2, 1], name
        assert rows[0]['last_login'].startswith('2026-08-02'), name
        assert rows[1]['last_login'] is None, name
        assert rows[1]['deleted_at'] is not None, name
    print('PASS: ' + name)

runbook = Path(__file__).resolve().parents[1] / 'docs/runbook-founder-manual-actions.md'
procedure = runbook.read_text().split('WITH changed AS (', 1)[1].split(' \\gset', 1)[0]
procedure = 'WITH changed AS (' + procedure
for key, value in {
    'keeper_id': '00000000-0000-0000-0000-000000000011',
    'renamed_id': '00000000-0000-0000-0000-000000000012',
    'operator_id': '00000000-0000-0000-0000-000000000013',
    'old_email': 'owner@example.com',
    'new_email': 'replacement@example.com',
    'reason': 'fixture approval',
}.items():
    procedure = procedure.replace(":'" + key + "'", "'" + value + "'")
subprocess.run(command + ['-X', '-qAt', '-v', 'ON_ERROR_STOP=1'],
               input='BEGIN READ ONLY; EXPLAIN ' + procedure + '; ROLLBACK;',
               text=True, capture_output=True, check=True)
print('PASS: PostgreSQL plans the documented rename and audit transaction (no execution)')

source = runbook.read_text().split('WITH changed AS (', 1)[1].split('    RETURNING u.id', 1)[0]
predicate = source.split('    WHERE ', 1)[1]
ids = {key: '00000000-0000-0000-0000-0000000000' + suffix for key, suffix in [
    ('keeper_id', '11'), ('renamed_id', '12'), ('operator_id', '13')
]}
parameters = dict(ids, old_email=' owner@example.COM ',
                  new_email='replacement@example.com', reason='fixture approval')
users_fixture = """WITH users(id, email) AS (VALUES
 ('00000000-0000-0000-0000-000000000011'::uuid, 'Owner@Example.com'),
 ('00000000-0000-0000-0000-000000000012'::uuid, ' owner@example.COM '),
 ('00000000-0000-0000-0000-000000000013'::uuid, 'operator@example.com'),
 ('00000000-0000-0000-0000-000000000014'::uuid, 'demo-owner@onevoice.local'),
 ('00000000-0000-0000-0000-000000000015'::uuid, 'DEMO-OWNER@ONEVOICE.LOCAL')
) """
for name, overrides, expected in [
    ('approved rename selects exactly one account', {}, [ids['renamed_id']]),
    ('stale address cannot be renamed', {'old_email': 'stale@example.com'}, []),
    ('keeper must belong to the collision', {'keeper_id': ids['operator_id']}, []),
    ('keeper cannot be renamed as its own duplicate', {'keeper_id': ids['renamed_id']}, []),
    ('replacement must be canonically unique', {'new_email': ' OPERATOR@Example.COM '}, []),
    ('blank replacement rejected', {'new_email': ' '}, []),
    ('audit reason required', {'reason': ' '}, []),
    ('demo owner protected', {
        'keeper_id': '00000000-0000-0000-0000-000000000015',
        'renamed_id': '00000000-0000-0000-0000-000000000014',
        'old_email': 'demo-owner@onevoice.local',
    }, []),
]:
    condition = predicate
    for key, value in dict(parameters, **overrides).items():
        condition = condition.replace(":'" + key + "'", "'" + value.replace("'", "''") + "'")
    sql = ("BEGIN READ ONLY; " + users_fixture +
           "SELECT coalesce(json_agg(u.id), '[]'::json) FROM users u WHERE " + condition +
           '; ROLLBACK;')
    result = subprocess.run(command + ['-X', '-qAt', '-v', 'ON_ERROR_STOP=1'],
                            input=sql, text=True, capture_output=True, check=True)
    assert json.loads(result.stdout) == expected, name
    print('PASS: ' + name)
