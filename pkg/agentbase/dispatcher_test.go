package agentbase_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
	"github.com/f1xgun/onevoice/pkg/hitldedupe"
)

// newTestDedupe spins up a miniredis-backed *hitldedupe.DedupeClient. Mirrors
// the helper in pkg/hitldedupe/dedupe_test.go so the dispatcher tests run
// against the same in-memory Redis as the dedupe package's own tests.
func newTestDedupe(t *testing.T) (*hitldedupe.DedupeClient, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return hitldedupe.New(rdb), mr
}

// okExec is the canonical happy-path exec callback: returns a populated
// ToolResponse echoing the request's TaskID. Used wherever a test only cares
// that exec runs and its response surfaces.
func okExec(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	return &a2a.ToolResponse{
		TaskID:  req.TaskID,
		Success: true,
		Result:  map[string]interface{}{"status": "sent"},
	}, nil
}

// TestNewDispatcher_AcceptsNilDedupe_AndNilClassifier verifies that both deps
// are optional. The four agent main.go files construct a Handler with a nil
// *hitldedupe.DedupeClient when REDIS_URL is empty (dev/test); those paths
// funnel through agentbase.NewDispatcher.
func TestNewDispatcher_AcceptsNilDedupe_AndNilClassifier(t *testing.T) {
	d := agentbase.NewDispatcher(nil, nil)
	require.NotNil(t, d, "NewDispatcher(nil, nil) must return a usable Dispatcher")

	resp, err := d.Dispatch(context.Background(), a2a.ToolRequest{TaskID: "t1"}, okExec)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "t1", resp.TaskID)
	assert.True(t, resp.Success)
}

// TestDispatch_NoDedupe_ExecCalled verifies the simplest path: no dedupe
// gate, no classifier — Dispatch is a thin pass-through.
func TestDispatch_NoDedupe_ExecCalled(t *testing.T) {
	var calls atomic.Int32
	exec := func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		calls.Add(1)
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	}

	d := agentbase.NewDispatcher(nil, nil)
	resp, err := d.Dispatch(context.Background(), a2a.ToolRequest{TaskID: "t1"}, exec)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int32(1), calls.Load(), "exec must be called exactly once")
	assert.Equal(t, "t1", resp.TaskID)
}

// TestDispatch_ClassifierWraps_NonRetryableError verifies the classifier sits
// between exec and the caller. Telegram, VK, Yandex and Google each have a
// classifier that wraps platform-permanent failures as a2a.NonRetryableError;
// those wrappers are passed in via FuncClassifier.
func TestDispatch_ClassifierWraps_NonRetryableError(t *testing.T) {
	platformErr := errors.New("Unauthorized")
	exec := func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, platformErr
	}
	classifier := agentbase.FuncClassifier(func(err error) error {
		if err == nil {
			return nil
		}
		return a2a.NewNonRetryableError(err)
	})

	d := agentbase.NewDispatcher(nil, classifier)
	resp, err := d.Dispatch(context.Background(), a2a.ToolRequest{TaskID: "t1"}, exec)

	assert.Nil(t, resp, "exec returned nil response — dispatcher must not fabricate one")
	require.Error(t, err)

	var nonRetryable *a2a.NonRetryableError
	require.True(t, errors.As(err, &nonRetryable),
		"classifier must wrap permanent errors as *a2a.NonRetryableError")
	assert.Equal(t, "Unauthorized", err.Error(),
		"NonRetryableError.Error() must surface the original message")
	assert.True(t, errors.Is(err, platformErr),
		"original error must remain reachable via errors.Is")
}

// TestDispatch_NilClassifier_PassesErrorThrough verifies that a nil
// classifier leaves exec's error untouched — useful for tests / legacy paths.
func TestDispatch_NilClassifier_PassesErrorThrough(t *testing.T) {
	rawErr := errors.New("some transient error")
	exec := func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, rawErr
	}

	d := agentbase.NewDispatcher(nil, nil)
	_, err := d.Dispatch(context.Background(), a2a.ToolRequest{TaskID: "t1"}, exec)

	require.Error(t, err)
	assert.Same(t, rawErr, err, "nil classifier must not modify exec's error")
}

// TestDispatch_FuncClassifier_NilReceiver_IsIdentity exercises the safety
// path inside FuncClassifier itself: a nil FuncClassifier value (not the
// interface, but the underlying function pointer) returns err unchanged.
func TestDispatch_FuncClassifier_NilReceiver_IsIdentity(t *testing.T) {
	var fc agentbase.FuncClassifier
	rawErr := errors.New("any error")
	got := fc.Classify(rawErr)
	assert.Same(t, rawErr, got, "nil FuncClassifier must act as identity")
	assert.Nil(t, fc.Classify(nil), "nil FuncClassifier with nil err must stay nil")
}

// TestDispatch_DedupeGate_EmptyApprovalID_BypassesGate verifies the
// auto-floor path (no HITL approval). When ApprovalID is empty Dispatch must
// NOT touch Redis — preserves the dedupeGate short-circuit at line 90 of
// the telegram handler.
func TestDispatch_DedupeGate_EmptyApprovalID_BypassesGate(t *testing.T) {
	dedupe, mr := newTestDedupe(t)
	require.Equal(t, 0, len(mr.Keys()), "pre-condition: empty redis")

	var execCalled atomic.Bool
	exec := func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		execCalled.Store(true)
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	}

	d := agentbase.NewDispatcher(dedupe, nil)
	resp, err := d.Dispatch(context.Background(),
		a2a.ToolRequest{TaskID: "t1", BusinessID: "biz-1", ApprovalID: ""}, exec)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, execCalled.Load(), "exec must run when ApprovalID is empty")
	assert.Equal(t, 0, len(mr.Keys()),
		"empty ApprovalID must not write any Redis key (anti-footgun #2)")
}

// TestDispatch_DedupeGate_InFlight_ShortCircuits verifies the in-flight
// outcome. Two parallel dispatches of the same (BusinessID, ApprovalID) must
// see exactly one exec invocation; the second returns the canonical
// "duplicate: already in flight" error response without calling exec.
func TestDispatch_DedupeGate_InFlight_ShortCircuits(t *testing.T) {
	dedupe, _ := newTestDedupe(t)

	out, _, err := dedupe.Claim(context.Background(), "biz-1", "appr-1")
	require.NoError(t, err)
	require.Equal(t, hitldedupe.ClaimOutcomeClaimed, out)

	var execCalled atomic.Bool
	exec := func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		execCalled.Store(true)
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	}

	d := agentbase.NewDispatcher(dedupe, nil)
	resp, err := d.Dispatch(context.Background(),
		a2a.ToolRequest{TaskID: "t-replay", BusinessID: "biz-1", ApprovalID: "appr-1"}, exec)

	require.NoError(t, err, "in-flight outcome surfaces a response, not an error")
	require.NotNil(t, resp)
	assert.False(t, execCalled.Load(), "exec must NOT run when another caller holds the claim")
	assert.Equal(t, "t-replay", resp.TaskID, "response TaskID must reflect THIS request, not the original")
	assert.Equal(t, "duplicate: already in flight", resp.Error,
		"in-flight response must carry the canonical error string")
}

// TestDispatch_DedupeGate_Duplicate_ReturnsCachedResponse verifies the
// cached-result path. After a previous Store, a fresh Dispatch must skip
// exec and return the cached ToolResponse — but with TaskID rewritten to
// the current request's TaskID so the orchestrator correlates correctly.
func TestDispatch_DedupeGate_Duplicate_ReturnsCachedResponse(t *testing.T) {
	dedupe, _ := newTestDedupe(t)
	ctx := context.Background()

	_, _, err := dedupe.Claim(ctx, "biz-1", "appr-1")
	require.NoError(t, err)
	original := &a2a.ToolResponse{
		TaskID:  "t-original",
		Success: true,
		Result:  map[string]interface{}{"status": "sent", "message_id": 42.0},
	}
	require.NoError(t, dedupe.Store(ctx, "biz-1", "appr-1", original))

	var execCalled atomic.Bool
	exec := func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		execCalled.Store(true)
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	}

	d := agentbase.NewDispatcher(dedupe, nil)
	resp, err := d.Dispatch(ctx,
		a2a.ToolRequest{TaskID: "t-replay", BusinessID: "biz-1", ApprovalID: "appr-1"}, exec)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, execCalled.Load(), "exec must NOT run on a cached duplicate")
	assert.Equal(t, "t-replay", resp.TaskID,
		"cached TaskID must be rewritten to the replay's TaskID")
	assert.True(t, resp.Success, "cached Success flag must round-trip")
	assert.Equal(t, "sent", resp.Result["status"], "cached Result fields must round-trip")
	assert.Equal(t, 42.0, resp.Result["message_id"])
}

// TestDispatch_DedupeGate_Duplicate_MalformedJSON_ReturnsGeneric verifies
// the defensive branch in dedupeGate. If a foreign writer corrupts the
// cached JSON the dispatcher must NOT crash or invoke exec — it must return
// the canonical "duplicate: cached result unavailable" envelope. Mirrors
// telegram handler.go:104-108.
func TestDispatch_DedupeGate_Duplicate_MalformedJSON_ReturnsGeneric(t *testing.T) {
	dedupe, mr := newTestDedupe(t)
	ctx := context.Background()

	key := hitldedupe.KeyFor("biz-1", "appr-bad")
	require.NoError(t, mr.Set(key, "{not-json"))

	var execCalled atomic.Bool
	exec := func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		execCalled.Store(true)
		return &a2a.ToolResponse{TaskID: req.TaskID, Success: true}, nil
	}

	d := agentbase.NewDispatcher(dedupe, nil)
	resp, err := d.Dispatch(ctx,
		a2a.ToolRequest{TaskID: "t-replay", BusinessID: "biz-1", ApprovalID: "appr-bad"}, exec)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, execCalled.Load(),
		"exec must NOT run even when cached payload is corrupt — duplicate is duplicate")
	assert.Equal(t, "t-replay", resp.TaskID)
	assert.Equal(t, "duplicate: cached result unavailable", resp.Error,
		"malformed cache must surface the canonical fallback error string")
}

// TestDispatch_StoreOnSuccess_Caches verifies the post-exec store step. After
// a successful exec, a re-dispatch with the same ApprovalID must hit the
// cache (ClaimOutcomeDuplicate path) without invoking exec a second time.
// This is the round-trip proof that dedupeStore actually wrote to Redis.
func TestDispatch_StoreOnSuccess_Caches(t *testing.T) {
	dedupe, mr := newTestDedupe(t)
	ctx := context.Background()

	var execCalls atomic.Int32
	exec := func(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
		execCalls.Add(1)
		return &a2a.ToolResponse{
			TaskID:  req.TaskID,
			Success: true,
			Result:  map[string]interface{}{"status": "sent"},
		}, nil
	}

	d := agentbase.NewDispatcher(dedupe, nil)

	resp1, err := d.Dispatch(ctx,
		a2a.ToolRequest{TaskID: "t-1", BusinessID: "biz-1", ApprovalID: "appr-1"}, exec)
	require.NoError(t, err)
	require.NotNil(t, resp1)
	require.Equal(t, int32(1), execCalls.Load())

	key := hitldedupe.KeyFor("biz-1", "appr-1")
	val, err := mr.Get(key)
	require.NoError(t, err)
	assert.NotEqual(t, "executing", val,
		"after a successful Dispatch the executing sentinel must be overwritten")
	var stored a2a.ToolResponse
	require.NoError(t, json.Unmarshal([]byte(val), &stored))
	assert.True(t, stored.Success)
	assert.Equal(t, "sent", stored.Result["status"])

	resp2, err := d.Dispatch(ctx,
		a2a.ToolRequest{TaskID: "t-2", BusinessID: "biz-1", ApprovalID: "appr-1"}, exec)
	require.NoError(t, err)
	require.NotNil(t, resp2)
	assert.Equal(t, int32(1), execCalls.Load(), "exec must NOT run on the cached replay")
	assert.Equal(t, "t-2", resp2.TaskID, "TaskID must be rewritten to the replay's TaskID")
	assert.True(t, resp2.Success)
}

// TestDispatch_StoreSkippedOnError verifies that failed executions are NOT
// cached. A subsequent dispatch with the same ApprovalID must be free to
// retry — the dedupe gate must see the original "executing" sentinel and
// return ClaimOutcomeInFlight, not a cached failure.
//
// Rationale (mirrors handler.go:122-125 comment): "Errors and nil responses
// are NOT cached — a replay should be free to retry when the original failed."
func TestDispatch_StoreSkippedOnError(t *testing.T) {
	dedupe, mr := newTestDedupe(t)
	ctx := context.Background()

	execErr := errors.New("transient failure")
	exec := func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, execErr
	}

	d := agentbase.NewDispatcher(dedupe, nil)
	resp, err := d.Dispatch(ctx,
		a2a.ToolRequest{TaskID: "t-1", BusinessID: "biz-1", ApprovalID: "appr-fail"}, exec)
	require.Error(t, err)
	assert.Same(t, execErr, err)
	assert.Nil(t, resp)

	key := hitldedupe.KeyFor("biz-1", "appr-fail")
	val, gerr := mr.Get(key)
	require.NoError(t, gerr)
	assert.Equal(t, "executing", val,
		"failed exec must NOT overwrite the executing sentinel — replay must be free to retry")
}

// TestDispatch_OrderingContract makes the end-to-end sequence assertion
// explicit: gate(claim) → exec → classifier → store. We use a classifier
// that mutates the error to a sentinel and verify that classify saw exec's
// error AND that store skipped (because err != nil after classify).
func TestDispatch_OrderingContract(t *testing.T) {
	dedupe, mr := newTestDedupe(t)
	ctx := context.Background()

	rawErr := errors.New("Unauthorized")
	wrapped := a2a.NewNonRetryableError(rawErr)

	var classifierCalls atomic.Int32
	classifier := agentbase.FuncClassifier(func(err error) error {
		classifierCalls.Add(1)
		if err == nil {
			return nil
		}
		return wrapped
	})

	exec := func(_ context.Context, _ a2a.ToolRequest) (*a2a.ToolResponse, error) {
		return nil, rawErr
	}

	d := agentbase.NewDispatcher(dedupe, classifier)
	resp, err := d.Dispatch(ctx,
		a2a.ToolRequest{TaskID: "t-1", BusinessID: "biz-1", ApprovalID: "appr-order"}, exec)

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Same(t, wrapped, err, "classifier output must be the dispatcher's returned error")
	assert.Equal(t, int32(1), classifierCalls.Load(), "classifier must run exactly once per Dispatch")

	val, gerr := mr.Get(hitldedupe.KeyFor("biz-1", "appr-order"))
	require.NoError(t, gerr)
	assert.Equal(t, "executing", val,
		"classified error must still be a non-nil error — store must skip")
}
