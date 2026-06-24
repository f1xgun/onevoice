package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Transactional-email outbox instrumentation. The outbox worker drains
// email_outbox rows and calls a Sender; these collectors expose what the
// slog lines alone could not — delivery success rate, retry/dead-letter
// growth, and the pending backlog depth.
//
// Label cardinality (see pkg/metrics/README.md):
//
//	result — closed set on EmailsSentTotal:
//	         sent         — Sender returned a provider job id.
//	         noop_dropped — NoopSender accepted the message without sending it
//	                        (dev/non-prod): the mail was DROPPED, not delivered.
//	                        A non-zero rate in production is a misconfiguration.
//
// No business_id / user_id / email labels — per-tenant PII is banned (banlist).
var (
	// EmailsSentTotal counts every Sender.Send that returned without error,
	// split by result so silently-dropped NoopSender mail is visible instead
	// of masquerading as a real delivery.
	EmailsSentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "emails_sent_total",
		Help: "Outbox sends that returned without error, by result (sent|noop_dropped).",
	}, []string{"result"})

	// EmailsRescheduledTotal counts outbox rows rescheduled for a later retry
	// after a transient send failure (backoff path, below the attempt cap).
	EmailsRescheduledTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "emails_rescheduled_total",
		Help: "Outbox rows rescheduled for a later retry after a transient send failure.",
	})

	// EmailsDeadLetteredTotal counts outbox rows moved to status='failed'
	// permanently — either an ErrPermanent send result or the retry cap being
	// reached. Sustained growth means mail is being lost.
	EmailsDeadLetteredTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "emails_dead_lettered_total",
		Help: "Outbox rows marked permanently failed (permanent error or retry cap reached).",
	})

	// OutboxPendingRows is the count of email_outbox rows still awaiting
	// delivery, sampled each worker tick. A rising backlog signals the worker
	// is falling behind or the Sender is wedged.
	OutboxPendingRows = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "outbox_pending_rows",
		Help: "Current number of email_outbox rows in status='pending'.",
	})

	// OutboxStrandedSentRows counts deliveries that succeeded at the Sender but
	// whose 'sent' persist failed, leaving the row in 'pending'. Each one is a
	// row at risk of being re-sent on a later tick — a duplicate email, since
	// the provider has no idempotency key. Any non-zero rate warrants a look.
	OutboxStrandedSentRows = promauto.NewCounter(prometheus.CounterOpts{
		Name: "outbox_stranded_sent_rows_total",
		Help: "Sends that succeeded but whose 'sent' persist failed, risking a duplicate re-send.",
	})
)
