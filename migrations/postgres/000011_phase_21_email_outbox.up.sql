-- (Account Lifecycle / Email Infrastructure): email_outbox
-- transactional outbox per §1.3.
--
-- Callers Enqueue a row INSIDE the same transaction that creates the
-- originating record (e.g. password_reset_tokens). The outbox worker
-- goroutine in services/api/cmd/main.go drains pending rows on a
-- ~5s poll interval, hits the Unisender Go API, and transitions
-- status: 'pending' -> 'sent' (success) or 'pending' -> 'pending'
-- with attempts++ and next_attempt_at += exp-backoff (transient) or
-- 'pending' -> 'failed' after 5 attempts.
--
-- Prod path: gen_random_uuid per services/api/AGENTS.md.
-- Integration-test mirror at services/api/migrations/000009_phase_21_email_outbox.up.sql.

CREATE TABLE email_outbox (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  to_email        TEXT NOT NULL,
  subject         TEXT NOT NULL,
  body_text       TEXT NOT NULL,
  body_html       TEXT,
  status          TEXT NOT NULL DEFAULT 'pending',
  attempts        INT NOT NULL DEFAULT 0,
  last_error      TEXT,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  sent_at         TIMESTAMPTZ,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_email_outbox_pending ON email_outbox(next_attempt_at) WHERE status = 'pending';
