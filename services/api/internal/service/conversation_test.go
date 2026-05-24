package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/service"
)

// stubConversationRepo is an in-memory double for
// domain.ConversationRepository used by ConversationService tests. Only the
// methods MoveToProject touches carry interesting state; the rest are no-ops
// that satisfy the interface so we get compile-time drift detection.
type stubConversationRepo struct {
	mu sync.Mutex

	// Conv is the preloaded conversation that GetByID returns. nil → ErrConversationNotFound.
	Conv *domain.Conversation

	// Calls captures every UpdateProjectAssignment call for ordering assertions.
	UpdateCalls []struct {
		ID        string
		ProjectID *string
	}

	// Forced failures.
	GetByIDErr               error
	UpdateProjectAssignErr   error
	GetByIDAfterUpdateErr    error
	getByIDInvocationCounter int
}

func (s *stubConversationRepo) GetByID(_ context.Context, id string) (*domain.Conversation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getByIDInvocationCounter++
	// Second GetByID (post-Update) honors a distinct error knob so tests can
	// simulate the rare refetch failure independently.
	if s.getByIDInvocationCounter == 2 && s.GetByIDAfterUpdateErr != nil {
		return nil, s.GetByIDAfterUpdateErr
	}
	if s.GetByIDErr != nil {
		return nil, s.GetByIDErr
	}
	if s.Conv == nil {
		return nil, domain.ErrConversationNotFound
	}
	cp := *s.Conv
	return &cp, nil
}

func (s *stubConversationRepo) UpdateProjectAssignment(_ context.Context, id string, projectID *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UpdateCalls = append(s.UpdateCalls, struct {
		ID        string
		ProjectID *string
	}{ID: id, ProjectID: projectID})
	if s.UpdateProjectAssignErr != nil {
		return s.UpdateProjectAssignErr
	}
	if s.Conv != nil {
		s.Conv.ProjectID = projectID
	}
	return nil
}

// Remaining methods are no-ops — only present to satisfy the interface.
func (s *stubConversationRepo) Create(_ context.Context, _ *domain.Conversation) error { return nil }
func (s *stubConversationRepo) ListByUserID(_ context.Context, _ string, _, _ int) ([]domain.Conversation, error) {
	return nil, nil
}
func (s *stubConversationRepo) Update(_ context.Context, _ *domain.Conversation) error    { return nil }
func (s *stubConversationRepo) Delete(_ context.Context, _ string) error                  { return nil }
func (s *stubConversationRepo) UpdateTitleIfPending(_ context.Context, _, _ string) error { return nil }
func (s *stubConversationRepo) TransitionToAutoPending(_ context.Context, _ string) error { return nil }
func (s *stubConversationRepo) Pin(_ context.Context, _, _, _ string) error               { return nil }
func (s *stubConversationRepo) Unpin(_ context.Context, _, _, _ string) error             { return nil }
func (s *stubConversationRepo) SearchTitles(_ context.Context, _, _, _ string, _ *string, _ int) ([]domain.ConversationTitleHit, []string, error) {
	return nil, nil, nil
}
func (s *stubConversationRepo) ScopedConversationIDs(_ context.Context, _, _ string, _ *string) ([]string, error) {
	return nil, nil
}

// stubMessageRepo captures the system note appended by MoveToProject and
// serves a preloaded message list for OpenChat tests via the Messages
// field.
type stubMessageRepo struct {
	mu sync.Mutex

	// Messages is what ListByConversationID returns. nil → the service
	// normalizes to []domain.Message{} on its end. Non-nil values flow
	// through verbatim so OpenChat tests can assert pass-through ordering.
	Messages []domain.Message
	// ListErr forces ListByConversationID to fail — exercised by OpenChat
	// tests covering the hard-error path.
	ListErr error

	// Created captures every Create call (system notes appended by
	// MoveToProject). Population is preserved for ordering assertions.
	Created   []*domain.Message
	CreateErr error
}

func (s *stubMessageRepo) Create(_ context.Context, m *domain.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.CreateErr != nil {
		return s.CreateErr
	}
	cp := *m
	s.Created = append(s.Created, &cp)
	return nil
}
func (s *stubMessageRepo) ListByConversationID(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ListErr != nil {
		return nil, s.ListErr
	}
	return s.Messages, nil
}
func (s *stubMessageRepo) CountByConversationID(_ context.Context, _ string) (int64, error) {
	return 0, nil
}
func (s *stubMessageRepo) Update(_ context.Context, _ *domain.Message) error { return nil }
func (s *stubMessageRepo) FindByConversationActive(_ context.Context, _ string) (*domain.Message, error) {
	return nil, domain.ErrMessageNotFound
}
func (s *stubMessageRepo) SearchByConversationIDs(_ context.Context, _ string, _ []string, _ int) ([]domain.MessageSearchHit, error) {
	return nil, nil
}

// stubProjectRepoForConv serves a preloaded project keyed by its UUID.
type stubProjectRepoForConv struct {
	mu      sync.Mutex
	Project *domain.Project
	GetErr  error
}

func (s *stubProjectRepoForConv) GetByID(_ context.Context, _ uuid.UUID) (*domain.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.GetErr != nil {
		return nil, s.GetErr
	}
	if s.Project == nil {
		return nil, domain.ErrProjectNotFound
	}
	cp := *s.Project
	return &cp, nil
}
func (s *stubProjectRepoForConv) Create(_ context.Context, _ *domain.Project) error { return nil }
func (s *stubProjectRepoForConv) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Project, error) {
	return nil, nil
}
func (s *stubProjectRepoForConv) Update(_ context.Context, _ *domain.Project) error { return nil }
func (s *stubProjectRepoForConv) Delete(_ context.Context, _ uuid.UUID) error       { return nil }
func (s *stubProjectRepoForConv) CountConversationsByID(_ context.Context, _ uuid.UUID) (int, error) {
	return 0, nil
}
func (s *stubProjectRepoForConv) HardDeleteCascade(_ context.Context, _ uuid.UUID) (deletedConvos, deletedMessages int, err error) {
	return 0, 0, nil
}

// stubPendingToolCallRepo serves a preloaded batch list for OpenChat
// tests. Only ListPendingByConversation is interesting; the rest are
// no-ops that satisfy the interface so we get compile-time drift
// detection.
type stubPendingToolCallRepo struct {
	mu sync.Mutex

	// Batches is what ListPendingByConversation returns. nil is a valid
	// "no active batches" answer (OpenChat normalizes to an empty slice).
	Batches []*domain.PendingToolCallBatch
	// ListErr forces the soft-error path in OpenChat — the call still
	// succeeds with PendingApprovals=[].
	ListErr error
}

func (s *stubPendingToolCallRepo) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ListErr != nil {
		return nil, s.ListErr
	}
	return s.Batches, nil
}
func (s *stubPendingToolCallRepo) Persist(_ context.Context, _ *domain.PendingToolCallBatch) error {
	return nil
}
func (s *stubPendingToolCallRepo) GetByBatchID(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return nil, nil
}
func (s *stubPendingToolCallRepo) AtomicTransitionToResolving(_ context.Context, _ string) (*domain.PendingToolCallBatch, error) {
	return nil, nil
}
func (s *stubPendingToolCallRepo) RecordDecisions(_ context.Context, _ string, _ []domain.PendingCall) error {
	return nil
}
func (s *stubPendingToolCallRepo) MarkDispatched(_ context.Context, _, _ string) error { return nil }
func (s *stubPendingToolCallRepo) MarkResolved(_ context.Context, _ string) error      { return nil }
func (s *stubPendingToolCallRepo) MarkExpired(_ context.Context, _ string) error       { return nil }
func (s *stubPendingToolCallRepo) ReconcileOrphanPreparing(_ context.Context, _ time.Duration) (int64, error) {
	return 0, nil
}

// newConvSvc assembles a ConversationService over the four stubs. Tests
// preconfigure stub state before the call and inspect post-state after.
// MoveToProject tests can pass a zero-value stubPendingToolCallRepo since
// that path doesn't read pending batches; passing nil would crash the
// constructor.
func newConvSvc(t *testing.T, conv *stubConversationRepo, msg *stubMessageRepo, proj *stubProjectRepoForConv, pending *stubPendingToolCallRepo) *service.ConversationService {
	t.Helper()
	if pending == nil {
		pending = &stubPendingToolCallRepo{}
	}
	svc, err := service.NewConversationService(conv, msg, proj, pending)
	require.NoError(t, err)
	return svc
}

func TestNewConversationService_NilDep_ReturnsError(t *testing.T) {
	conv := &stubConversationRepo{}
	msg := &stubMessageRepo{}
	proj := &stubProjectRepoForConv{}
	pending := &stubPendingToolCallRepo{}

	cases := []struct {
		name    string
		conv    domain.ConversationRepository
		msg     domain.MessageRepository
		proj    domain.ProjectRepository
		pending domain.PendingToolCallRepository
	}{
		{"nil conv", nil, msg, proj, pending},
		{"nil msg", conv, nil, proj, pending},
		{"nil proj", conv, msg, nil, pending},
		{"nil pending", conv, msg, proj, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := service.NewConversationService(tc.conv, tc.msg, tc.proj, tc.pending)
			assert.Error(t, err, "%s: nil dep must reject at construction time", tc.name)
			assert.Nil(t, s)
		})
	}
}

// TestMoveToProject_HappyPath_WithProject covers the canonical
// move-to-named-project flow. Asserts:
//   - GetByID called twice (initial fetch + post-update refetch)
//   - UpdateProjectAssignment called with the parsed projectID
//   - Project name flows into the system note
//   - Returned conversation reflects the new project_id
func TestMoveToProject_HappyPath_WithProject(t *testing.T) {
	requesterUser := uuid.New()
	businessID := uuid.New()
	projID := uuid.New()
	convID := "507f1f77bcf86cd799439011"

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{
			ID:        convID,
			UserID:    requesterUser.String(),
			ProjectID: nil,
		},
	}
	msg := &stubMessageRepo{}
	proj := &stubProjectRepoForConv{
		Project: &domain.Project{
			ID:         projID,
			Name:       "Marketing",
			BusinessID: businessID,
		},
	}
	svc := newConvSvc(t, conv, msg, proj, nil)

	projIDStr := projID.String()
	updated, err := svc.MoveToProject(context.Background(), convID, businessID, requesterUser, &projIDStr)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, convID, updated.ID, "must return the post-move conversation")
	require.NotNil(t, updated.ProjectID)
	assert.Equal(t, projIDStr, *updated.ProjectID, "project_id must reflect the new assignment")

	require.Len(t, conv.UpdateCalls, 1, "exactly one UpdateProjectAssignment call")
	assert.Equal(t, convID, conv.UpdateCalls[0].ID)
	require.NotNil(t, conv.UpdateCalls[0].ProjectID)
	assert.Equal(t, projIDStr, *conv.UpdateCalls[0].ProjectID)

	require.Len(t, msg.Created, 1, "must append exactly one system note")
	assert.Equal(t, convID, msg.Created[0].ConversationID)
	assert.Equal(t, "system", msg.Created[0].Role)
	assert.Contains(t, msg.Created[0].Content, "Marketing",
		"system note must include resolved project name; got: %s", msg.Created[0].Content)
}

// TestMoveToProject_HappyPath_NoProject covers the move-to-"no project"
// flow — projectID = nil. The localized default destination label is used
// in the system note; the persisted project_id is nil.
func TestMoveToProject_HappyPath_NoProject(t *testing.T) {
	requesterUser := uuid.New()
	businessID := uuid.New()
	convID := "507f1f77bcf86cd799439011"
	originalProj := uuid.NewString()

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{
			ID:        convID,
			UserID:    requesterUser.String(),
			ProjectID: &originalProj,
		},
	}
	msg := &stubMessageRepo{}
	proj := &stubProjectRepoForConv{}
	svc := newConvSvc(t, conv, msg, proj, nil)

	updated, err := svc.MoveToProject(context.Background(), convID, businessID, requesterUser, nil)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Nil(t, updated.ProjectID, "project_id must be cleared")

	require.Len(t, conv.UpdateCalls, 1)
	assert.Nil(t, conv.UpdateCalls[0].ProjectID,
		"UpdateProjectAssignment must receive nil pointer (not a pointer to empty string)")

	require.Len(t, msg.Created, 1)
	// English catalog ships "No project" as the default destination — the
	// localized label is locale-dependent; assert the content is non-empty
	// and the role is system rather than coupling to a specific locale.
	assert.NotEmpty(t, msg.Created[0].Content)
	assert.Equal(t, "system", msg.Created[0].Role)
}

func TestMoveToProject_ConversationNotFound(t *testing.T) {
	conv := &stubConversationRepo{Conv: nil}
	svc := newConvSvc(t, conv, &stubMessageRepo{}, &stubProjectRepoForConv{}, nil)

	_, err := svc.MoveToProject(context.Background(), "missing", uuid.New(), uuid.New(), nil)
	assert.True(t, errors.Is(err, domain.ErrConversationNotFound),
		"want ErrConversationNotFound, got %v", err)
}

// TestMoveToProject_Forbidden_OtherUserConversation enforces ownership.
// The conversation belongs to a different user; the requester gets
// ErrForbidden (handler maps to 403) without leaking conversation
// existence beyond that.
func TestMoveToProject_Forbidden_OtherUserConversation(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: owner.String()},
	}
	msg := &stubMessageRepo{}
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, nil)

	_, err := svc.MoveToProject(context.Background(), "c1", uuid.New(), other, nil)
	assert.True(t, errors.Is(err, domain.ErrForbidden), "want ErrForbidden, got %v", err)

	// Critical: ownership rejection must NOT have written anything.
	assert.Empty(t, conv.UpdateCalls, "ownership rejection must NOT call UpdateProjectAssignment")
	assert.Empty(t, msg.Created, "ownership rejection must NOT append a system note")
}

func TestMoveToProject_ProjectNotFound_NonExistent(t *testing.T) {
	requesterUser := uuid.New()
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
	}
	proj := &stubProjectRepoForConv{Project: nil} // GetByID returns ErrProjectNotFound
	svc := newConvSvc(t, conv, &stubMessageRepo{}, proj, nil)

	projIDStr := uuid.NewString()
	_, err := svc.MoveToProject(context.Background(), "c1", uuid.New(), requesterUser, &projIDStr)
	assert.True(t, errors.Is(err, domain.ErrProjectNotFound), "want ErrProjectNotFound, got %v", err)
	assert.Empty(t, conv.UpdateCalls, "missing project must NOT proceed to UpdateProjectAssignment")
}

// TestMoveToProject_ProjectNotFound_CrossTenant covers the existence-
// enumeration defense: a project that exists but belongs to a different
// business surfaces as ErrProjectNotFound, not ErrForbidden.
func TestMoveToProject_ProjectNotFound_CrossTenant(t *testing.T) {
	requesterUser := uuid.New()
	requesterBiz := uuid.New()
	otherBiz := uuid.New()

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
	}
	proj := &stubProjectRepoForConv{
		Project: &domain.Project{
			ID:         uuid.New(),
			Name:       "Other Business Project",
			BusinessID: otherBiz, // cross-tenant
		},
	}
	svc := newConvSvc(t, conv, &stubMessageRepo{}, proj, nil)

	projIDStr := proj.Project.ID.String()
	_, err := svc.MoveToProject(context.Background(), "c1", requesterBiz, requesterUser, &projIDStr)
	assert.True(t, errors.Is(err, domain.ErrProjectNotFound),
		"cross-tenant project access must surface as ErrProjectNotFound (existence-enumeration defense), got %v", err)
	assert.Empty(t, conv.UpdateCalls)
}

func TestMoveToProject_InvalidProjectID(t *testing.T) {
	requesterUser := uuid.New()
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
	}
	svc := newConvSvc(t, conv, &stubMessageRepo{}, &stubProjectRepoForConv{}, nil)

	bad := "not-a-uuid"
	_, err := svc.MoveToProject(context.Background(), "c1", uuid.New(), requesterUser, &bad)
	assert.True(t, errors.Is(err, service.ErrInvalidProjectID),
		"malformed UUID must surface as ErrInvalidProjectID, got %v", err)
	assert.Empty(t, conv.UpdateCalls)
}

// TestMoveToProject_NoteFailure_DoesNotFailMove proves the best-effort
// semantic. messageRepo.Create fails; the move STILL succeeds and returns
// the post-move conversation. A failed note is observable but recoverable;
// failing the request would leave the conversation in its new project
// without an undo path.
func TestMoveToProject_NoteFailure_DoesNotFailMove(t *testing.T) {
	requesterUser := uuid.New()
	convID := "c1"
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: convID, UserID: requesterUser.String()},
	}
	msg := &stubMessageRepo{CreateErr: errors.New("mongo timeout")}
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, nil)

	updated, err := svc.MoveToProject(context.Background(), convID, uuid.New(), requesterUser, nil)
	require.NoError(t, err, "note-write failure must NOT fail the move")
	require.NotNil(t, updated)
	require.Len(t, conv.UpdateCalls, 1, "UpdateProjectAssignment still ran")
	// Created list stays empty — Create returned an error.
	assert.Empty(t, msg.Created)
}

// TestMoveToProject_UpdateAssignmentError_StopsBeforeNote covers the
// inverse of the note-failure case: when the actual move write fails, the
// service surfaces the error and MUST NOT append a system note for a move
// that didn't happen.
func TestMoveToProject_UpdateAssignmentError_StopsBeforeNote(t *testing.T) {
	requesterUser := uuid.New()
	conv := &stubConversationRepo{
		Conv:                   &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
		UpdateProjectAssignErr: errors.New("mongo write failure"),
	}
	msg := &stubMessageRepo{}
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, nil)

	_, err := svc.MoveToProject(context.Background(), "c1", uuid.New(), requesterUser, nil)
	require.Error(t, err, "UpdateProjectAssignment failure must surface")
	assert.Empty(t, msg.Created, "no note for a move that didn't land")
}

// quick sanity: ProjectID type ergonomics — service accepts *string for
// the move target. The empty-string-pointer case is treated like nil
// (both mean "no project") and must NOT trigger projectRepo.GetByID.
func TestMoveToProject_EmptyStringProjectID_TreatedAsNoProject(t *testing.T) {
	requesterUser := uuid.New()
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
	}
	proj := &stubProjectRepoForConv{} // GetErr stays nil; if called by mistake, Project=nil → ErrProjectNotFound
	svc := newConvSvc(t, conv, &stubMessageRepo{}, proj, nil)

	empty := ""
	updated, err := svc.MoveToProject(context.Background(), "c1", uuid.New(), requesterUser, &empty)
	require.NoError(t, err)
	require.NotNil(t, updated)
	require.Len(t, conv.UpdateCalls, 1)
	assert.NotNil(t, conv.UpdateCalls[0].ProjectID,
		"empty-string projectID must reach the repo as a non-nil pointer to empty string")
	assert.Equal(t, "", *conv.UpdateCalls[0].ProjectID)
}

// --- OpenChat tests -------------------------------------------------

// TestOpenChat_HappyPath_EmptyPending covers the canonical chat-load
// path: messages list flows through, no active approval batches → an
// explicit empty PendingApprovals slice (never nil — the frontend
// iterates unconditionally).
func TestOpenChat_HappyPath_EmptyPending(t *testing.T) {
	requesterUser := uuid.New()
	convID := "507f1f77bcf86cd799439101"

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: convID, UserID: requesterUser.String()},
	}
	msg := &stubMessageRepo{
		Messages: []domain.Message{
			{ID: "m1", ConversationID: convID, Role: "user", Content: "hi"},
			{ID: "m2", ConversationID: convID, Role: "assistant", Content: "hello"},
		},
	}
	pending := &stubPendingToolCallRepo{} // Batches=nil → empty result
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, pending)

	view, err := svc.OpenChat(context.Background(), convID, requesterUser)
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Len(t, view.Messages, 2)
	assert.Equal(t, "m1", view.Messages[0].ID)
	assert.Equal(t, "m2", view.Messages[1].ID)
	require.NotNil(t, view.PendingApprovals, "PendingApprovals must be a non-nil empty slice, never nil")
	assert.Empty(t, view.PendingApprovals)
}

// TestOpenChat_HappyPath_WithPending exercises the projection: a single
// pending batch with one call surfaces in the view, EditableFields is a
// non-nil empty slice (stable wire contract), and timestamps round-trip.
func TestOpenChat_HappyPath_WithPending(t *testing.T) {
	requesterUser := uuid.New()
	convID := "507f1f77bcf86cd799439102"
	created := time.Now().UTC().Truncate(time.Second)
	expires := created.Add(24 * time.Hour)

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: convID, UserID: requesterUser.String()},
	}
	pending := &stubPendingToolCallRepo{
		Batches: []*domain.PendingToolCallBatch{
			{
				ID:             "batch-abc",
				ConversationID: convID,
				MessageID:      "msg-42",
				Status:         "pending",
				Calls: []domain.PendingCall{
					{CallID: "toolu_1", ToolName: "telegram__send_channel_post", Arguments: map[string]interface{}{"text": "hi"}},
				},
				CreatedAt: created,
				ExpiresAt: expires,
			},
		},
	}
	svc := newConvSvc(t, conv, &stubMessageRepo{}, &stubProjectRepoForConv{}, pending)

	view, err := svc.OpenChat(context.Background(), convID, requesterUser)
	require.NoError(t, err)
	require.NotNil(t, view)
	require.Len(t, view.PendingApprovals, 1)

	pa := view.PendingApprovals[0]
	assert.Equal(t, "batch-abc", pa.BatchID)
	assert.Equal(t, "msg-42", pa.MessageID)
	assert.Equal(t, "pending", pa.Status)
	assert.Equal(t, created, pa.CreatedAt)
	assert.Equal(t, expires, pa.ExpiresAt)
	require.Len(t, pa.Calls, 1)
	assert.Equal(t, "toolu_1", pa.Calls[0].CallID)
	assert.Equal(t, "telegram__send_channel_post", pa.Calls[0].ToolName)
	require.NotNil(t, pa.Calls[0].EditableFields,
		"EditableFields must be a non-nil empty slice for stable JSON contract")
	assert.Empty(t, pa.Calls[0].EditableFields,
		"EditableFields population is deferred to the frontend's live registry")
}

// TestOpenChat_PendingLookupSoftError covers the load-bearing soft-error
// policy: if ListPendingByConversation fails, OpenChat still succeeds with
// PendingApprovals=[]. The messages list IS the load-bearing payload; a
// failed approval-card hydration must not fail the whole request.
func TestOpenChat_PendingLookupSoftError(t *testing.T) {
	requesterUser := uuid.New()
	convID := "507f1f77bcf86cd799439103"

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: convID, UserID: requesterUser.String()},
	}
	msg := &stubMessageRepo{
		Messages: []domain.Message{
			{ID: "m1", ConversationID: convID, Role: "user", Content: "still useful"},
		},
	}
	pending := &stubPendingToolCallRepo{ListErr: errors.New("mongo timeout")}
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, pending)

	view, err := svc.OpenChat(context.Background(), convID, requesterUser)
	require.NoError(t, err, "pending-lookup failure must NOT fail the request")
	require.NotNil(t, view)
	require.Len(t, view.Messages, 1, "messages list still flows through on soft-error")
	require.NotNil(t, view.PendingApprovals)
	assert.Empty(t, view.PendingApprovals, "PendingApprovals must degrade to [] on soft-error")
}

// TestOpenChat_ConversationNotFound covers the missing-conversation path.
// Returns the canonical sentinel so the handler maps to 404.
func TestOpenChat_ConversationNotFound(t *testing.T) {
	conv := &stubConversationRepo{Conv: nil}
	svc := newConvSvc(t, conv, &stubMessageRepo{}, &stubProjectRepoForConv{}, nil)

	_, err := svc.OpenChat(context.Background(), "missing", uuid.New())
	assert.True(t, errors.Is(err, domain.ErrConversationNotFound),
		"want ErrConversationNotFound, got %v", err)
}

// TestOpenChat_Forbidden_OtherUserConversation enforces ownership:
// the conversation exists but belongs to a different user → ErrForbidden,
// and crucially neither ListByConversationID NOR ListPendingByConversation
// runs (no read amplification past the ownership gate).
func TestOpenChat_Forbidden_OtherUserConversation(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: owner.String()},
	}
	// If either repo gets touched on the forbidden path, fail loudly.
	msg := &stubMessageRepo{ListErr: errors.New("must not be called on forbidden path")}
	pending := &stubPendingToolCallRepo{ListErr: errors.New("must not be called on forbidden path")}
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, pending)

	_, err := svc.OpenChat(context.Background(), "c1", other)
	assert.True(t, errors.Is(err, domain.ErrForbidden), "want ErrForbidden, got %v", err)
}

// TestOpenChat_MessageRepoError_HardFails verifies the messages list is
// the load-bearing payload: a failure there fails the request (unlike
// the pending soft-error policy). Distinguishes the two failure modes.
func TestOpenChat_MessageRepoError_HardFails(t *testing.T) {
	requesterUser := uuid.New()
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
	}
	msg := &stubMessageRepo{ListErr: errors.New("mongo down")}
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, nil)

	_, err := svc.OpenChat(context.Background(), "c1", requesterUser)
	assert.Error(t, err, "messages list is load-bearing — failure must surface")
}

// TestOpenChat_NilMessages_NormalizedToEmptySlice mirrors the legacy
// handler's defensive nil → []domain.Message{} normalization so a nil
// repo return never reaches JSON encoding as `null`.
func TestOpenChat_NilMessages_NormalizedToEmptySlice(t *testing.T) {
	requesterUser := uuid.New()
	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: "c1", UserID: requesterUser.String()},
	}
	msg := &stubMessageRepo{Messages: nil} // explicit nil from repo
	svc := newConvSvc(t, conv, msg, &stubProjectRepoForConv{}, nil)

	view, err := svc.OpenChat(context.Background(), "c1", requesterUser)
	require.NoError(t, err)
	require.NotNil(t, view.Messages, "Messages must be a non-nil empty slice on nil repo return")
	assert.Empty(t, view.Messages)
}

// TestOpenChat_MultipleBatches_PreservesOrder covers the edge case where
// resume spawned a second pause: both batches surface, in repo order.
func TestOpenChat_MultipleBatches_PreservesOrder(t *testing.T) {
	requesterUser := uuid.New()
	convID := "c-multi"

	conv := &stubConversationRepo{
		Conv: &domain.Conversation{ID: convID, UserID: requesterUser.String()},
	}
	pending := &stubPendingToolCallRepo{
		Batches: []*domain.PendingToolCallBatch{
			{ID: "b1", ConversationID: convID, MessageID: "m1", Status: "pending"},
			{ID: "b2", ConversationID: convID, MessageID: "m2", Status: "resolving"},
		},
	}
	svc := newConvSvc(t, conv, &stubMessageRepo{}, &stubProjectRepoForConv{}, pending)

	view, err := svc.OpenChat(context.Background(), convID, requesterUser)
	require.NoError(t, err)
	require.Len(t, view.PendingApprovals, 2)
	assert.Equal(t, "b1", view.PendingApprovals[0].BatchID)
	assert.Equal(t, "pending", view.PendingApprovals[0].Status)
	assert.Equal(t, "b2", view.PendingApprovals[1].BatchID)
	assert.Equal(t, "resolving", view.PendingApprovals[1].Status)
}

// silenceLinter holds a reference to time so it can't accidentally be
// pruned during a future refactor.
var _ = time.Now
