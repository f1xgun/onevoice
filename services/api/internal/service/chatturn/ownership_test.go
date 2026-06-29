package chatturn

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/orchestratorclient"
)

// ownershipStubBusiness / Integ / Project back the enrichment readers. The
// cross-tenant turn is rejected by the ownership gate before any of these run;
// the proceed-path tests rely on them returning valid data so enrichment gets
// as far as persisting the user message.
type ownershipStubBusiness struct{ BusinessReader }

func (ownershipStubBusiness) GetByID(_ context.Context, id uuid.UUID) (*domain.Business, error) {
	return &domain.Business{ID: id, Name: "Stub"}, nil
}

type ownershipStubInteg struct{ IntegrationLister }

func (ownershipStubInteg) ListByBusinessID(_ context.Context, _ uuid.UUID) ([]domain.Integration, error) {
	return nil, nil
}

type ownershipStubProject struct{ ProjectReader }

// ownershipConvRepo returns a fixed conversation (or not-found) and counts
// GetByID calls so the test can prove the gate consulted the repo.
type ownershipConvRepo struct {
	domain.ConversationRepository
	conv     *domain.Conversation
	notFound bool
	calls    int
}

func (r *ownershipConvRepo) GetByID(_ context.Context, _ string) (*domain.Conversation, error) {
	r.calls++
	if r.notFound {
		return nil, domain.ErrConversationNotFound
	}
	return r.conv, nil
}

func (r *ownershipConvRepo) BumpLastMessageAt(_ context.Context, _ string, _ time.Time) error {
	return nil
}

// ownershipMsgRepo records Create and ListByConversationID so the test can prove
// that a rejected turn neither persisted the attacker's message nor loaded the
// victim's history into the LLM prompt.
type ownershipMsgRepo struct {
	domain.MessageRepository
	created   []domain.Message
	listCalls int
}

func (r *ownershipMsgRepo) Create(_ context.Context, m *domain.Message) error {
	r.created = append(r.created, *m)
	return nil
}

func (r *ownershipMsgRepo) ListByConversationID(_ context.Context, _ string, _, _ int) ([]domain.Message, error) {
	r.listCalls++
	return nil, nil
}

func (r *ownershipMsgRepo) FindByConversationActive(_ context.Context, _ string) (*domain.Message, error) {
	return nil, domain.ErrMessageNotFound
}

func newOwnershipTurn(convRepo domain.ConversationRepository, msgRepo domain.MessageRepository) *Turn {
	return New(Deps{
		Business:      ownershipStubBusiness{},
		Integrations:  ownershipStubInteg{},
		Projects:      ownershipStubProject{},
		Conversations: convRepo,
		Messages:      msgRepo,
		Pending:       &MockPendingRepoNoBatches{},
		Orch:          orchestratorclient.New("http://127.0.0.1:1", nil),
	})
}

// MockPendingRepoNoBatches is a pending repo that reports no batches, so the
// gate (when it proceeds) sees a fresh turn.
type MockPendingRepoNoBatches struct {
	domain.PendingToolCallRepository
}

func (MockPendingRepoNoBatches) ListPendingByConversation(_ context.Context, _ string) ([]*domain.PendingToolCallBatch, error) {
	return nil, nil
}

// TestRun_CrossTenantConversation_RejectedAsNotFound is the IDOR regression: a
// member of organization A targets a conversation that belongs to user/org B by
// its ID. The router only authorized the caller against the {bizID} URL segment,
// so without the ownership gate the turn would load B's history into the prompt,
// persist A's message into B's conversation, and stream a reply. The gate must
// reject the turn as a uniform not-found BEFORE any of that happens.
//
// Reverting the gate in Turn.Run makes this fail: the turn proceeds past the
// owner check, the attacker's user message is persisted into the victim
// conversation, and the victim's history is loaded.
func TestRun_CrossTenantConversation_RejectedAsNotFound(t *testing.T) {
	victimUser := uuid.New()
	victimBiz := uuid.New()
	attackerUser := uuid.New()
	attackerBiz := uuid.New()

	convRepo := &ownershipConvRepo{conv: &domain.Conversation{
		ID:         "victim-conv",
		UserID:     victimUser.String(),
		BusinessID: victimBiz.String(),
	}}
	msgRepo := &ownershipMsgRepo{}
	turn := newOwnershipTurn(convRepo, msgRepo)

	req := TurnRequest{
		BusinessID:     attackerBiz,
		UserID:         attackerUser,
		ConversationID: "victim-conv",
		Message:        "leak the victim's data",
	}

	rr := httptest.NewRecorder()
	outcome, err := turn.Run(context.Background(), rr, req, nil)

	require.NoError(t, err)
	assert.Equal(t, OutcomeConversationNotFound, outcome,
		"cross-tenant turn must terminate as conversation-not-found")
	assert.Positive(t, convRepo.calls, "the gate must consult the conversation repo")
	assert.Empty(t, msgRepo.created,
		"no user message may be persisted into the victim conversation")
	assert.Zero(t, msgRepo.listCalls,
		"the victim's history must not be loaded into the LLM prompt")
}

// TestRun_SameUserDifferentOrg_RejectedAsNotFound covers the second arm of the
// predicate: the same user, but a conversation owned by a different organization
// (the user is a member of multiple orgs). Owner must match on BOTH user_id and
// business_id, mirroring ConversationService.
func TestRun_SameUserDifferentOrg_RejectedAsNotFound(t *testing.T) {
	user := uuid.New()
	convRepo := &ownershipConvRepo{conv: &domain.Conversation{
		ID:         "conv-x",
		UserID:     user.String(),
		BusinessID: uuid.New().String(),
	}}
	msgRepo := &ownershipMsgRepo{}
	turn := newOwnershipTurn(convRepo, msgRepo)

	req := TurnRequest{
		BusinessID:     uuid.New(),
		UserID:         user,
		ConversationID: "conv-x",
		Message:        "cross-org probe",
	}

	rr := httptest.NewRecorder()
	outcome, err := turn.Run(context.Background(), rr, req, nil)

	require.NoError(t, err)
	assert.Equal(t, OutcomeConversationNotFound, outcome)
	assert.Empty(t, msgRepo.created)
	assert.Zero(t, msgRepo.listCalls)
}

// TestRun_NotFoundConversation_ProceedsToFirstTurn proves the gate does NOT
// break the create-on-first-turn path: a brand-new conversation ID that has no
// row yet must be allowed through (the turn legitimately precedes the row). The
// gate proceeds into enrichment, which here fails on the orchestrator being
// unreachable (127.0.0.1:1) — proving we got PAST the gate. Crucially the
// attacker's message reaches persistUserMessage (Create called), the opposite of
// the cross-tenant case.
func TestRun_NotFoundConversation_ProceedsToFirstTurn(t *testing.T) {
	convRepo := &ownershipConvRepo{notFound: true}
	msgRepo := &ownershipMsgRepo{}
	turn := newOwnershipTurn(convRepo, msgRepo)

	req := TurnRequest{
		BusinessID:     uuid.New(),
		UserID:         uuid.New(),
		ConversationID: "brand-new-conv",
		Message:        "hello",
	}

	rr := httptest.NewRecorder()
	outcome, _ := turn.Run(context.Background(), rr, req, nil)

	assert.NotEqual(t, OutcomeConversationNotFound, outcome,
		"a not-yet-created conversation must not be rejected by the ownership gate")
	assert.True(t, hasUserMessage(msgRepo.created),
		"the first-turn path must persist the user message")
}

// hasUserMessage reports whether the recorded Create calls include the user's
// message. The fresh-turn lifecycle also reserves an in_progress assistant
// placeholder via Create, so the ownership proceed-path tests assert on the
// presence of the user role rather than an exact Create count.
func hasUserMessage(created []domain.Message) bool {
	for i := range created {
		if created[i].Role == domain.MessageRoleUser {
			return true
		}
	}
	return false
}

// TestResumeApproved_CrossTenantConversation_Rejected is the resume-path arm of
// the IDOR gate. ResumeApproved is reached from POST /chat/{id}/resume, which
// bypasses Turn.Run, so it needs its own ownership check. Here the pending batch
// claims a (user, org) that does NOT own the targeted conversation — the
// resume must abort as not-found and must NOT finalize / mutate the victim's
// active message.
//
// Reverting the gate in ResumeApproved makes this fail: the resume proceeds,
// streams, and Update is called on the victim's message.
func TestResumeApproved_CrossTenantConversation_Rejected(t *testing.T) {
	convRepo := &ownershipConvRepo{conv: &domain.Conversation{
		ID:         "conv-1",
		UserID:     uuid.New().String(),
		BusinessID: uuid.New().String(),
	}}
	msgRepo := &resumeMsgRepo{active: &domain.Message{
		ID:             "msg-1",
		ConversationID: "conv-1",
		Role:           domain.MessageRoleAssistant,
		Status:         domain.MessageStatusPendingApproval,
	}}
	turn := New(Deps{
		Business:      resumeStubBusiness{},
		Integrations:  resumeStubInteg{},
		Projects:      resumeStubProject{},
		Conversations: convRepo,
		Messages:      msgRepo,
		Pending: &resumePendingRepo{batch: &domain.PendingToolCallBatch{
			ID:             "batch-1",
			ConversationID: "conv-1",
			UserID:         uuid.New().String(),
			BusinessID:     uuid.New().String(),
		}},
		Orch: orchestratorclient.New("http://127.0.0.1:1", nil),
	})

	rr := httptest.NewRecorder()
	outcome, err := turn.ResumeApproved(context.Background(), rr, "conv-1", "batch-1", nil)

	require.NoError(t, err)
	assert.Equal(t, OutcomeConversationNotFound, outcome,
		"a resume whose batch does not own the conversation must abort as not-found")
	assert.Zero(t, msgRepo.updateCalls,
		"the victim's active message must not be finalized by a cross-tenant resume")
}

// TestRun_OwnerMatches_Proceeds proves the happy path: when the conversation
// exists AND the caller owns it (user_id + business_id both match), the gate
// lets the turn through to enrichment.
func TestRun_OwnerMatches_Proceeds(t *testing.T) {
	user := uuid.New()
	biz := uuid.New()
	convRepo := &ownershipConvRepo{conv: &domain.Conversation{
		ID:         "owned-conv",
		UserID:     user.String(),
		BusinessID: biz.String(),
	}}
	msgRepo := &ownershipMsgRepo{}
	turn := newOwnershipTurn(convRepo, msgRepo)

	req := TurnRequest{
		BusinessID:     biz,
		UserID:         user,
		ConversationID: "owned-conv",
		Message:        "my own message",
	}

	rr := httptest.NewRecorder()
	outcome, _ := turn.Run(context.Background(), rr, req, nil)

	assert.NotEqual(t, OutcomeConversationNotFound, outcome,
		"the owner's own conversation must not be rejected")
	assert.True(t, hasUserMessage(msgRepo.created), "the owner's message must be persisted")
}
