// Package email is the shared transactional-email abstraction consumed by
// services/api (for password reset, email verification, account deletion
// confirmation). It exposes a Sender interface plus two implementations:
// UnisenderSender (production, against Unisender Go HTTPS-JSON API) and
// NoopSender (tests + local dev, records sent messages in-memory).
//
// Callers MUST NOT call Sender.Send directly from inside a request
// handler. Instead they enqueue a row into the email_outbox table inside
// the same transaction that creates the originating row (e.g. a
// password_reset_tokens row). A background worker in
// services/api/cmd/main.go drains the outbox by calling Sender.Send and
// recording success/failure per D-03 in
// .planning/phases/21-account-lifecycle/21-CONTEXT.md.
package email
