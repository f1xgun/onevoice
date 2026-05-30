-- (Account Lifecycle / Email Infrastructure): email_outbox
-- transactional outbox per §1.3.
-- (See migrations/postgres/000011_phase_21_email_outbox.up.sql for full doc.)
--
-- Integration-test path: uuid_generate_v4 per services/api/AGENTS.md.

CREATE TABLE email_outbox (
  id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
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
