package agentbase_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/agentbase"
)

// Test fixture: a tool exec that records invocation and returns a canned
// response/error. Allows asserting which route was hit and how many times.
type recordingExec struct {
	calls int
	resp  *a2a.ToolResponse
	err   error
}

func (r *recordingExec) Exec(_ context.Context, req a2a.ToolRequest) (*a2a.ToolResponse, error) {
	r.calls++
	if r.resp != nil {
		r.resp.TaskID = req.TaskID
	}
	return r.resp, r.err
}

// stubDispatcher captures the exec it received and lets the test control the
// returned (resp, err). It implements agentbase.Dispatcher.
type stubDispatcher struct {
	gotExec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error)
	gotReq  a2a.ToolRequest
	resp    *a2a.ToolResponse
	err     error
	// When invokeExec is true, the stub actually calls the exec it received
	// (so router→dispatcher→routeTool chains can be observed end-to-end).
	invokeExec bool
}

func (s *stubDispatcher) Dispatch(
	ctx context.Context,
	req a2a.ToolRequest,
	exec func(context.Context, a2a.ToolRequest) (*a2a.ToolResponse, error),
) (*a2a.ToolResponse, error) {
	s.gotExec = exec
	s.gotReq = req
	if s.invokeExec {
		return exec(ctx, req)
	}
	return s.resp, s.err
}

func TestNewRouter_NilDispatcher_RoutesByToolName(t *testing.T) {
	post := &recordingExec{resp: &a2a.ToolResponse{Success: true}}
	photo := &recordingExec{resp: &a2a.ToolResponse{Success: true}}
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__post":  post.Exec,
		"x__photo": photo.Exec,
	}, nil, nil)

	resp, err := router(context.Background(), a2a.ToolRequest{TaskID: "t1", Tool: "x__post"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
	assert.Equal(t, 1, post.calls, "post route hit once")
	assert.Equal(t, 0, photo.calls, "photo route not hit")
	assert.Equal(t, "t1", resp.TaskID)
}

func TestNewRouter_UnknownTool_ReturnsError(t *testing.T) {
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__known": (&recordingExec{resp: &a2a.ToolResponse{Success: true}}).Exec,
	}, nil, nil)

	resp, err := router(context.Background(), a2a.ToolRequest{Tool: "x__missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "unknown tool: x__missing")
}

func TestNewRouter_NilDispatcher_AppliesFallbackClassifier(t *testing.T) {
	rawErr := errors.New("transient telegram error")
	wrappedErr := a2a.NewNonRetryableError(errors.New("permanent telegram error"))
	classify := func(err error) error {
		if err == nil {
			return nil
		}
		return wrappedErr
	}
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__post": (&recordingExec{err: rawErr}).Exec,
	}, nil, agentbase.FuncClassifier(classify))

	_, err := router(context.Background(), a2a.ToolRequest{Tool: "x__post"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, &a2a.NonRetryableError{}),
		"classifier must wrap permanent errors when dispatcher is nil")
}

func TestNewRouter_NilDispatcher_NilClassifier_PropagatesErrorUnchanged(t *testing.T) {
	rawErr := errors.New("network blip")
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__post": (&recordingExec{err: rawErr}).Exec,
	}, nil, nil)

	_, err := router(context.Background(), a2a.ToolRequest{Tool: "x__post"})
	require.Error(t, err)
	assert.Same(t, rawErr, err, "with nil classifier, error must pass through identity-equal")
}

func TestNewRouter_NonNilDispatcher_ForwardsThroughDispatch(t *testing.T) {
	exec := &recordingExec{resp: &a2a.ToolResponse{Success: true}}
	dispatcher := &stubDispatcher{
		invokeExec: true,
	}
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__post": exec.Exec,
	}, dispatcher, nil)

	resp, err := router(context.Background(), a2a.ToolRequest{TaskID: "tx", Tool: "x__post"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "tx", resp.TaskID)
	assert.Equal(t, 1, exec.calls, "dispatcher must invoke the routed exec")
	assert.Equal(t, "x__post", dispatcher.gotReq.Tool, "dispatcher must see the original request")
	assert.NotNil(t, dispatcher.gotExec, "dispatcher must receive a routeTool callback")
}

func TestNewRouter_NonNilDispatcher_IgnoresFallbackClassifier(t *testing.T) {
	rawErr := errors.New("transient")
	exec := &recordingExec{err: rawErr}
	dispatcher := &stubDispatcher{
		invokeExec: true,
	}

	wrapEverything := func(_ error) error {
		return a2a.NewNonRetryableError(errors.New("WRAPPED BY ROUTER"))
	}
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__post": exec.Exec,
	}, dispatcher, agentbase.FuncClassifier(wrapEverything))

	_, err := router(context.Background(), a2a.ToolRequest{Tool: "x__post"})
	require.Error(t, err)
	assert.Same(t, rawErr, err, "router must NOT apply fallbackClassifier when dispatcher is non-nil")
}

func TestNewRouter_NonNilDispatcher_UnknownToolErrorReachesDispatcher(t *testing.T) {
	dispatcher := &stubDispatcher{invokeExec: true}
	router := agentbase.NewRouter(map[string]agentbase.ToolExec{
		"x__known": (&recordingExec{resp: &a2a.ToolResponse{Success: true}}).Exec,
	}, dispatcher, nil)

	_, err := router(context.Background(), a2a.ToolRequest{Tool: "x__missing"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown tool: x__missing")
}

func TestNewRouter_RoutesMapIsCapturedNotShared(t *testing.T) {
	original := map[string]agentbase.ToolExec{
		"x__a": (&recordingExec{resp: &a2a.ToolResponse{Success: true}}).Exec,
	}
	router := agentbase.NewRouter(original, nil, nil)

	original["x__b"] = (&recordingExec{resp: &a2a.ToolResponse{Success: true}}).Exec

	resp, err := router(context.Background(), a2a.ToolRequest{Tool: "x__b"})
	require.NoError(t, err, "routes map is referenced (not copied); mutations after construction take effect")
	require.NotNil(t, resp)
}
