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

// resumePersistFailuresTotal counts hard Message.Update failures on the HITL
// resume persistence paths. A failure here does not strand the conversation —
// the self-heal gate (gateHealStranded → finalizeStranded) retries the
// write-back on the next request because the un-persisted Complete status
// leaves the DB row active — but it was previously logged at warn and was
// otherwise invisible. This counter makes the silent failure alertable.
//
// The "path" label is the closed set {partial, repause, done} naming the three
// resume persist call sites; ResumePersistFailure collapses anything else to
// "other" so an unexpected caller cannot explode label cardinality.
//
// See pkg/metrics/README.md for the label-cardinality convention.
var resumePersistFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "chatturn_resume_persist_failures_total",
	Help: "HITL resume Message.Update failures, labeled by path {partial|repause|done}. Recovered by the self-heal gate; surfaced here for alerting.",
}, []string{"path"})

// allowedResumePersistPaths caps the resume-persist `path` label to the three
// known call sites. Anything outside collapses to labelOther.
var allowedResumePersistPaths = map[string]struct{}{
	"partial": {},
	"repause": {},
	"done":    {},
}

// postsPublishedTotal counts publish attempts for content the agents push to a
// platform, as the Post record is written after the SSE stream ends. It is the
// product-level view of "how much is being posted, and how often does a publish
// fail or get deferred". Emitted once per posting tool call that produced a Post
// record.
//
// The "platform" label is the closed AgentID set
// {telegram, vk, yandex_business, google_business}; "result" is the closed set
// {published, scheduled, error} mirroring the Post status postal.go writes
// (immediate publish, delayed publish, failed publish). Both are normalized in
// IncPostsPublished so an unexpected platform or status cannot explode label
// cardinality.
//
// See pkg/metrics/README.md for the label-cardinality convention.
var postsPublishedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "posts_published_total",
	Help: "Post publish attempts, labeled by platform and result {published|scheduled|error}.",
}, []string{"platform", "result"})

// reviewsRepliedTotal counts review records upserted from a *__get_reviews tool
// result, labeled by the reply state carried on the review. It is the
// product-level view of "are fetched reviews carrying a reply, and do upserts
// fail". Emitted once per upserted review.
//
// The "platform" label is the closed AgentID set
// {telegram, vk, yandex_business, google_business}; "result" is the closed set
// {replied, pending, error} mirroring domain.ReviewReplyStatus* plus the
// upsert-failure outcome. Both are normalized in IncReviewsReplied so an
// unexpected platform or status cannot explode label cardinality.
//
// See pkg/metrics/README.md for the label-cardinality convention.
var reviewsRepliedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "reviews_replied_total",
	Help: "Review upserts, labeled by platform and reply result {replied|pending|error}.",
}, []string{"platform", "result"})

// allowedMetricPlatforms caps the `platform` label cardinality to the fixed
// AgentID allowlist. Anything outside collapses to labelOther in
// normalizeMetricPlatform.
var allowedMetricPlatforms = map[string]struct{}{
	"telegram":        {},
	"vk":              {},
	"yandex_business": {},
	"google_business": {},
}

// allowedPostResults caps the post `result` label to the statuses postal.go
// writes onto a Post record. Anything outside collapses to labelOther.
var allowedPostResults = map[string]struct{}{
	"published": {},
	"scheduled": {},
	"error":     {},
}

// allowedReviewResults caps the review `result` label to the reply states a
// review record carries plus the upsert-failure outcome. Anything outside
// collapses to labelOther.
var allowedReviewResults = map[string]struct{}{
	"replied": {},
	"pending": {},
	"error":   {},
}

func normalizeMetricPlatform(platform string) string {
	if _, ok := allowedMetricPlatforms[platform]; ok {
		return platform
	}
	return labelOther
}

// IncChatTurn increments chat_turns_total for the given outcome. Pass
// chatturn.TurnOutcome.String(); any other value still increments but
// pollutes the bounded label set.
func IncChatTurn(outcome string) {
	chatTurnsTotal.WithLabelValues(outcome).Inc()
}

// IncPostsPublished increments posts_published_total for the given platform and
// result. Both labels are collapsed to "other" when outside their closed sets
// (platforms: the AgentID allowlist; results: {published, scheduled, error}) so
// an unexpected upstream value cannot explode label cardinality.
func IncPostsPublished(platform, result string) {
	if _, ok := allowedPostResults[result]; !ok {
		result = labelOther
	}
	postsPublishedTotal.WithLabelValues(normalizeMetricPlatform(platform), result).Inc()
}

// IncReviewsReplied increments reviews_replied_total for the given platform and
// result. Both labels are collapsed to "other" when outside their closed sets
// (platforms: the AgentID allowlist; results: {replied, pending, error}) so an
// unexpected upstream value cannot explode label cardinality.
func IncReviewsReplied(platform, result string) {
	if _, ok := allowedReviewResults[result]; !ok {
		result = labelOther
	}
	reviewsRepliedTotal.WithLabelValues(normalizeMetricPlatform(platform), result).Inc()
}

// ResumePersistFailure increments chatturn_resume_persist_failures_total for
// the given resume persist path. Anything outside the closed set
// {partial, repause, done} is collapsed to "other" so an unexpected caller
// cannot explode label cardinality.
func ResumePersistFailure(path string) {
	if _, ok := allowedResumePersistPaths[path]; !ok {
		path = labelOther
	}
	resumePersistFailuresTotal.WithLabelValues(path).Inc()
}

// IncHITLDecision increments hitl_decisions_total for the given effective
// decision. Anything outside the closed set {approve, edit, reject} is
// collapsed to "other" so an unvalidated upstream action string cannot
// explode label cardinality.
func IncHITLDecision(decision string) {
	switch decision {
	case "approve", "edit", "reject":
	default:
		decision = labelOther
	}
	hitlDecisionsTotal.WithLabelValues(decision).Inc()
}
