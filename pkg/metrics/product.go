package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// chatTurnsTotal counts chat-turn processing outcomes — the product-level
// view of "is the assistant doing work, and how does each turn end". It is
// emitted once per turn segment: once when a fresh send is processed, and
// again when a paused (HITL) turn is resumed. So a turn that pauses then
// completes contributes two samples ({outcome="pause_hitl"} then
// {outcome="done"}); this is intentional — it lets dashboards separate
// pauses from completions rather than collapsing them.
//
// The "outcome" label is the closed set produced by
// chatturn.TurnOutcome.String() (done, error, pause_hitl,
// reemitted_approval, rejoined_resume, orchestrator_unavailable,
// missing_message, business_not_found, inline_error) — bounded and stable
// (it is part of the observability contract; Grafana panels key on these).
//
// See pkg/metrics/README.md for the label-cardinality convention.
var chatTurnsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "chat_turns_total",
	Help: "Chat-turn processing outcomes, labeled by outcome (chatturn.TurnOutcome). Emitted per send and per resume segment.",
}, []string{"outcome"})

// hitlDecisionsTotal counts human-in-the-loop approval verdicts as they are
// persisted, labeled by the EFFECTIVE decision after server-side policy
// re-evaluation (an approve/edit rewritten to reject by a revoked tool
// floor counts as "reject"). The "decision" label is the closed set
// {approve, edit, reject, other}. Useful for spotting HITL friction (a
// spike in rejects) and confirming approvals flow.
//
// "other" is a defensive catch-all (like the mongo `op` whitelist): the
// resolve endpoint does not currently validate the per-call `action`
// against the oneof tag, so an arbitrary decision string could otherwise
// reach this label and explode cardinality. IncHITLDecision collapses
// anything outside the known set to "other".
//
// See pkg/metrics/README.md for the label-cardinality convention.
var hitlDecisionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "hitl_decisions_total",
	Help: "HITL approval verdicts persisted, labeled by effective decision {approve|edit|reject|other}.",
}, []string{"decision"})

// IncChatTurn increments chat_turns_total for the given outcome. Pass
// chatturn.TurnOutcome.String(); any other value still increments but
// pollutes the bounded label set.
func IncChatTurn(outcome string) {
	chatTurnsTotal.WithLabelValues(outcome).Inc()
}

// IncHITLDecision increments hitl_decisions_total for the given effective
// decision. Anything outside the closed set {approve, edit, reject} is
// collapsed to "other" so an unvalidated upstream action string cannot
// explode label cardinality.
func IncHITLDecision(decision string) {
	switch decision {
	case "approve", "edit", "reject":
	default:
		decision = "other"
	}
	hitlDecisionsTotal.WithLabelValues(decision).Inc()
}
