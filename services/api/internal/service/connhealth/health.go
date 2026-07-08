// Package connhealth models per-integration connection liveness — a distinct
// concept from the composite presence-health score (service/presencehealth).
// Connection health answers "can OneVoice still act on this channel right now?"
// (bot still admin, VK token still has wall scope, Yandex session still alive),
// whereas presence health is a 0-100 rollup of rating/SLA/coverage/sync.
//
// The health state is stored inside Integration.Metadata under a namespaced
// sub-object ("connection_health") so it never collides with channel_title /
// linked_group_status / group_id / etc. and needs no schema migration: the
// worker enumerates via ListAllActiveByPlatforms (which returns Metadata) and
// UpdateMetadata performs a targeted jsonb write that will not clobber a
// concurrent MarkTokenExpired status flip.
package connhealth

import "time"

// MetadataKey is the Integration.Metadata sub-object under which the health
// result is stored.
const MetadataKey = "connection_health"

// Status is the per-integration liveness verdict.
type Status string

const (
	// StatusActive: the channel is reachable and OneVoice retains the rights it
	// needs to act (post, reply, read).
	StatusActive Status = "active"
	// StatusDegraded: the channel is reachable but a non-blocking capability is
	// missing (reserved for future partial-permission cases).
	StatusDegraded Status = "degraded"
	// StatusBroken: a conclusive, fixable failure — the bot lost admin, the
	// token lost a required scope, or the session expired. Surfaces a Reconnect
	// affordance on the FE and drives the owner nudge.
	StatusBroken Status = "broken"
	// StatusUnknown: the probe was inconclusive (rate-limit, anti-bot, transport
	// or NATS timeout). NEVER alarms the owner and NEVER overwrites a prior
	// active — see DemoteOnlyIfConclusive.
	StatusUnknown Status = "unknown"
)

// Reason codes are stable machine strings resolved to human copy on the FE
// (next-intl) and in the worker nudge (pkg/i18n). Metadata is written
// locale-agnostic so a baked-in localized string would be wrong for the
// reader's locale.
const (
	ReasonOK                  = "ok"
	ReasonInconclusive        = "inconclusive"
	ReasonTelegramNotAdmin    = "tg_not_admin"
	ReasonTelegramNoPostRight = "tg_no_post_rights"
	ReasonVKWallScopeMissing  = "vk_wall_scope_missing"
	ReasonVKTokenInvalid      = "vk_token_invalid"
	ReasonYandexSessionExpiry = "yandex_session_expired"
)

// Result is the outcome of one platform probe.
type Result struct {
	Status     Status
	ReasonCode string
	CheckedAt  time.Time
}

// IntegrationHealth pairs a Result with the integration coordinates the caller
// (verify endpoint, worker) surfaces to the client.
type IntegrationHealth struct {
	Platform   string
	ExternalID string
	Status     Status
	ReasonCode string
	CheckedAt  time.Time
}

// ReadFromMetadata extracts the previously stored health sub-object from an
// integration's metadata, returning the zero Result (empty status) when absent
// or malformed. Callers treat an empty Status as "no prior verdict".
func ReadFromMetadata(meta map[string]interface{}) Result {
	sub, ok := subObject(meta)
	if !ok {
		return Result{}
	}
	var res Result
	if s, ok := sub["status"].(string); ok {
		res.Status = Status(s)
	}
	if rc, ok := sub["reason_code"].(string); ok {
		res.ReasonCode = rc
	}
	if ts, ok := sub["checked_at"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			res.CheckedAt = parsed
		}
	}
	return res
}

// ReadNudgedAt extracts the owner-nudge throttle timestamp from the stored
// health sub-object, returning the zero time when absent.
func ReadNudgedAt(meta map[string]interface{}) time.Time {
	sub, ok := subObject(meta)
	if !ok {
		return time.Time{}
	}
	if ts, ok := sub["nudged_at"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// MergeIntoMetadata returns a NEW metadata map that copies every sibling key of
// existing (channel_title, linked_group_status, telegram_user_id, …) and sets
// the connection_health sub-object to res, preserving any prior nudged_at so the
// owner-nudge throttle survives a routine health write. The input map is never
// mutated.
func MergeIntoMetadata(existing map[string]interface{}, res Result) map[string]interface{} {
	out := make(map[string]interface{}, len(existing)+1)
	for k, v := range existing {
		out[k] = v
	}
	sub := map[string]interface{}{
		"status":      string(res.Status),
		"reason_code": res.ReasonCode,
		"checked_at":  res.CheckedAt.UTC().Format(time.RFC3339),
	}
	if nudged := ReadNudgedAt(existing); !nudged.IsZero() {
		sub["nudged_at"] = nudged.UTC().Format(time.RFC3339)
	}
	out[MetadataKey] = sub
	return out
}

// HealthPatch returns the targeted metadata patch that sets ONLY the
// connection_health sub-object (status/reason_code/checked_at, preserving any
// prior nudged_at). It is persisted via a server-side jsonb_set that touches
// just this key, so sibling keys (telegram_user_id, channel_title,
// access_verified, …) written by a concurrent connect/owner-bind flow can never
// be reverted by a routine health write — the whole-map write path could. The
// returned map is the { connection_health: {...} } patch, not the full metadata.
func HealthPatch(existing map[string]interface{}, res Result) map[string]interface{} {
	sub := map[string]interface{}{
		"status":      string(res.Status),
		"reason_code": res.ReasonCode,
		"checked_at":  res.CheckedAt.UTC().Format(time.RFC3339),
	}
	if nudged := ReadNudgedAt(existing); !nudged.IsZero() {
		sub["nudged_at"] = nudged.UTC().Format(time.RFC3339)
	}
	return map[string]interface{}{MetadataKey: sub}
}

// MergeNudgedAt returns a NEW metadata map identical to existing but with the
// connection_health.nudged_at stamp set to at (or cleared when at is zero),
// preserving the current status/reason_code/checked_at. Used by the worker to
// stamp a sent nudge or clear it on recovery without recomputing the verdict.
func MergeNudgedAt(existing map[string]interface{}, at time.Time) map[string]interface{} {
	out := make(map[string]interface{}, len(existing)+1)
	for k, v := range existing {
		out[k] = v
	}
	sub := map[string]interface{}{}
	if prev, ok := subObject(existing); ok {
		for k, v := range prev {
			sub[k] = v
		}
	}
	if at.IsZero() {
		delete(sub, "nudged_at")
	} else {
		sub["nudged_at"] = at.UTC().Format(time.RFC3339)
	}
	out[MetadataKey] = sub
	return out
}

// DemoteOnlyIfConclusive applies the fail-soft rule: an inconclusive probe
// (StatusUnknown) NEVER overwrites a prior conclusive verdict. When the new
// probe is Unknown and a prior status exists, the prior status/reason are kept
// and only checked_at advances; when there is no prior status, the Unknown is
// recorded as-is. A conclusive new probe (active/degraded/broken) always wins.
func DemoteOnlyIfConclusive(prev, next Result) Result {
	if next.Status != StatusUnknown {
		return next
	}
	if prev.Status == "" || prev.Status == StatusUnknown {
		return next
	}
	return Result{
		Status:     prev.Status,
		ReasonCode: prev.ReasonCode,
		CheckedAt:  next.CheckedAt,
	}
}

// subObject returns the connection_health sub-map from an integration's
// metadata. The JSONB round-trip through the driver hands back
// map[string]interface{}, so that is the only shape we accept.
func subObject(meta map[string]interface{}) (map[string]interface{}, bool) {
	if meta == nil {
		return nil, false
	}
	sub, ok := meta[MetadataKey].(map[string]interface{})
	return sub, ok
}
