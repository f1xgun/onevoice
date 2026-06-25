// Package hitldedupe provides per-agent deduplication of HITL approvals
// via Redis SetNX. The key format is `hitl:approval:{business_id}:{approval_id}`.
// A completed result keeps a 24h TTL matching the pending_tool_calls.expires_at
// window; the pre-execution "executing" sentinel uses a short TTL so a crashed
// or failed claim auto-expires and a legitimate retry can re-claim.
//
// The package is shared across all platform agents (telegram, vk,
// yandex-business, google-business) so dedupe semantics never drift.
package hitldedupe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	// DedupeTTL matches the 24h window of pending_tool_calls.expires_at so a
	// retry of an approved tool within the approval window gets deduped. It is
	// applied only to the COMPLETED result written by Store, so a genuinely
	// finished call keeps its full 24h dedupe window.
	DedupeTTL = 24 * time.Hour

	// ExecutingTTL bounds how long the "executing" sentinel survives before the
	// owning execution calls Store. A crashed agent or a failed exec never
	// reaches Store, so a long-lived sentinel would block a legitimate retry of
	// the same approved tool for the full DedupeTTL. The short window lets the
	// sentinel auto-expire so a retry can re-Claim, while a completed call still
	// gets the full DedupeTTL via Store.
	ExecutingTTL = 90 * time.Second

	// valueExecuting is the sentinel stored by Claim before the tool executes.
	// Claim uses it to distinguish "in-flight elsewhere" from "completed".
	valueExecuting = "executing"
)

// ClaimOutcome enumerates the four states a Claim call can resolve to.
type ClaimOutcome int

const (
	// ClaimOutcomeSkip: empty ApprovalID — legacy/auto path, bypass dedupe.
	ClaimOutcomeSkip ClaimOutcome = iota
	// ClaimOutcomeClaimed: this caller owns execution; proceed and call Store after success.
	ClaimOutcomeClaimed
	// ClaimOutcomeInFlight: another goroutine is currently executing; caller must abort.
	ClaimOutcomeInFlight
	// ClaimOutcomeDuplicate: already executed previously; cachedResult JSON is returned.
	ClaimOutcomeDuplicate
)

// DedupeClient wraps a *redis.Client with the HITL dedupe semantics.
type DedupeClient struct {
	rdb *redis.Client
}

// New constructs a DedupeClient around an existing go-redis v9 client.
func New(rdb *redis.Client) *DedupeClient {
	return &DedupeClient{rdb: rdb}
}

// KeyFor returns the canonical Redis key for (businessID, approvalID).
// Exported so tests can assert the exact format.
func KeyFor(businessID, approvalID string) string {
	return fmt.Sprintf("hitl:approval:%s:%s", businessID, approvalID)
}

// Claim attempts to register the (businessID, approvalID) tuple as "in-flight".
// Outcomes:
//
//	ClaimOutcomeSkip      — approvalID is empty; skip dedupe entirely.
//	ClaimOutcomeClaimed   — caller MUST execute and then call Store.
//	ClaimOutcomeInFlight  — another goroutine holds the claim; caller MUST NOT execute.
//	ClaimOutcomeDuplicate — execution already completed; cachedResult is the
//	                        JSON-encoded ToolResponse stored by a prior Store call.
//
// CRITICAL invariant: when approvalID is empty, Claim short-circuits BEFORE
// touching Redis — no SetNX is issued. This is the backward-compat path for
// pre-Phase-16 auto-floor tool calls and is proven by TestClaim_EmptyApprovalID_ReturnsSkip.
func (d *DedupeClient) Claim(ctx context.Context, businessID, approvalID string) (ClaimOutcome, string, error) {
	if approvalID == "" {
		return ClaimOutcomeSkip, "", nil
	}
	key := KeyFor(businessID, approvalID)
	ok, err := d.rdb.SetNX(ctx, key, valueExecuting, ExecutingTTL).Result()
	if err != nil {
		return ClaimOutcomeSkip, "", fmt.Errorf("hitldedupe: SetNX: %w", err)
	}
	if ok {
		return ClaimOutcomeClaimed, "", nil
	}
	val, err := d.rdb.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return ClaimOutcomeDuplicate, "", nil
		}
		return ClaimOutcomeSkip, "", fmt.Errorf("hitldedupe: Get after SetNX miss: %w", err)
	}
	if val == valueExecuting {
		return ClaimOutcomeInFlight, "", nil
	}
	return ClaimOutcomeDuplicate, val, nil
}

// Store persists the execution result under the dedupe key with a fresh 24h
// TTL, overwriting the "executing" sentinel. Subsequent Claim calls on the
// same (businessID, approvalID) will see ClaimOutcomeDuplicate and receive
// the cached JSON.
//
// When approvalID is empty, Store is a no-op — mirrors Claim's short-circuit.
func (d *DedupeClient) Store(ctx context.Context, businessID, approvalID string, result interface{}) error {
	if approvalID == "" {
		return nil
	}
	key := KeyFor(businessID, approvalID)
	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("hitldedupe: marshal result: %w", err)
	}
	return d.rdb.Set(ctx, key, string(payload), DedupeTTL).Err()
}

// Release deletes the dedupe key when an execution failed before reaching
// Store. The ExecutingTTL already auto-expires a stranded sentinel, so Release
// is a best-effort belt-and-braces that frees the key immediately rather than
// waiting out the short window. Callers that only held the "executing" sentinel
// (never completed) use it so a retry can re-Claim at once.
//
// When approvalID is empty, Release is a no-op — mirrors Claim's short-circuit.
func (d *DedupeClient) Release(ctx context.Context, businessID, approvalID string) error {
	if approvalID == "" {
		return nil
	}
	return d.rdb.Del(ctx, KeyFor(businessID, approvalID)).Err()
}
