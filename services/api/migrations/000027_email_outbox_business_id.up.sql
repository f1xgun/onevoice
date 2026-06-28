-- email_outbox: nullable business scoping column.
-- (See migrations/postgres/000028_email_outbox_business_id.up.sql for full doc.)
--
-- Integration-test path.

ALTER TABLE email_outbox
  ADD COLUMN business_id UUID;
