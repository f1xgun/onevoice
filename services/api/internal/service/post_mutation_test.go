package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	natslib "github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

type mutationPostRepo struct {
	domain.PostRepository
	post      domain.Post
	claimed   bool
	completed *domain.Post
}

func (r *mutationPostRepo) GetByID(context.Context, string) (*domain.Post, error) {
	p := r.post
	p.PlatformResults = map[string]domain.PlatformResult{}
	for k, v := range r.post.PlatformResults {
		p.PlatformResults[k] = v
	}
	return &p, nil
}
func (r *mutationPostRepo) ClaimOperation(context.Context, string, string, string, string) (bool, error) {
	if r.claimed {
		return false, nil
	}
	r.claimed = true
	return true, nil
}
func (r *mutationPostRepo) CompleteOperation(_ context.Context, p *domain.Post, _ string) error {
	r.completed = p
	return nil
}

type mutationRequester struct {
	calls    int
	req      a2a.ToolRequest
	err      error
	response *a2a.ToolResponse
}

func (n *mutationRequester) RequestMsgWithContext(_ context.Context, msg *natslib.Msg) (*natslib.Msg, error) {
	n.calls++
	if err := json.Unmarshal(msg.Data, &n.req); err != nil {
		return nil, err
	}
	if n.err != nil {
		return nil, n.err
	}
	response := n.response
	if response == nil {
		response = &a2a.ToolResponse{Success: true, Result: map[string]any{"message_id": 42}}
	}
	data, _ := json.Marshal(response)
	return &natslib.Msg{Data: data}, nil
}

func TestPostMutationsPreserveIdentityAndBlockDuplicateClaim(t *testing.T) {
	biz := uuid.New()
	for _, action := range []string{actionEdit, "delete", "retry"} {
		t.Run(action, func(t *testing.T) {
			status, postID := "published", "42"
			if action == "retry" {
				status, postID = "error", ""
			}
			repo := &mutationPostRepo{post: domain.Post{ID: "p", BusinessID: biz.String(), Content: "old", MediaURLs: []string{"https://example.org/photo.png"}, Status: status, PlatformResults: map[string]domain.PlatformResult{"telegram": {PostID: postID, URL: "https://t.me/own/42", ExternalID: "@own", Status: status}}}}
			requester := &mutationRequester{}
			svc := WithPostDispatcher(NewPostService(repo, nil), requester)
			ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: biz, RoleID: uuid.MustParse(domain.SystemRoleOwnerID), Permissions: []authz.Permission{authz.PermContentUpdate, authz.PermContentDelete, authz.PermContentCreate}})
			text := "new"
			got, err := svc.Mutate(ctx, biz, "p", action, &text)
			require.NoError(t, err)
			require.Equal(t, "p", got.ID)
			require.Equal(t, repo.post.MediaURLs, got.MediaURLs)
			require.Equal(t, "42", got.PlatformResults["telegram"].PostID)
			require.Equal(t, "https://t.me/own/42", got.PlatformResults["telegram"].URL)
			require.Equal(t, "@own", requester.req.Args["channel_id"])
			require.NotEmpty(t, requester.req.ApprovalID)
			if action == actionEdit {
				require.Equal(t, true, requester.req.Args["is_caption"])
				require.Equal(t, "new", got.Content)
			}
			_, err = svc.Mutate(ctx, biz, "p", action, &text)
			require.ErrorIs(t, err, domain.ErrPostConflict)
			require.Equal(t, 1, requester.calls)
		})
	}
}

func TestPostMutationTimeoutIsUncertain(t *testing.T) {
	biz := uuid.New()
	repo := &mutationPostRepo{post: domain.Post{ID: "p", BusinessID: biz.String(), Status: "error", PlatformResults: map[string]domain.PlatformResult{"telegram": {ExternalID: "@own", Status: "error"}}}}
	requester := &mutationRequester{err: errors.New("request timeout")}
	svc := WithPostDispatcher(NewPostService(repo, nil), requester)
	ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: biz, Permissions: []authz.Permission{authz.PermContentCreate}})
	got, err := svc.Mutate(ctx, biz, "p", "retry", nil)
	require.NoError(t, err)
	require.Equal(t, "uncertain", got.Status)
	require.Equal(t, "uncertain", got.PlatformResults["telegram"].Status)
}

func TestPostMutationGuards(t *testing.T) {
	business := uuid.New()
	for _, tc := range []struct {
		name, action, status, target, postID string
		owner, permission                    bool
		foreign                              bool
	}{
		{name: "legacy target", action: actionEdit, status: "published", postID: "42", permission: true},
		{name: "successful retry", action: "retry", status: "published", target: "@own", postID: "42", permission: true},
		{name: "uncertain retry", action: "retry", status: "uncertain", target: "@own", permission: true},
		{name: "nonowner delete", action: "delete", status: "published", target: "@own", postID: "42", permission: true},
		{name: "permission missing", action: actionEdit, status: "published", target: "@own", postID: "42"},
		{name: "foreign business", action: actionEdit, status: "published", target: "@own", postID: "42", permission: true, foreign: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			savedBusiness := business.String()
			if tc.foreign {
				savedBusiness = uuid.NewString()
			}
			repo := &mutationPostRepo{post: domain.Post{ID: "p", BusinessID: savedBusiness, Status: tc.status, PlatformResults: map[string]domain.PlatformResult{"telegram": {ExternalID: tc.target, PostID: tc.postID, Status: tc.status}}}}
			requester := &mutationRequester{}
			svc := WithPostDispatcher(NewPostService(repo, nil), requester)
			permissions := []authz.Permission{}
			if tc.permission {
				permissions = []authz.Permission{authz.PermContentUpdate, authz.PermContentCreate, authz.PermContentDelete}
			}
			ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, RoleID: uuid.New(), Permissions: permissions})
			text := "new"
			_, err := svc.Mutate(ctx, business, "p", tc.action, &text)
			require.Error(t, err)
			require.Zero(t, requester.calls)
			require.False(t, repo.claimed)
		})
	}
}

func TestPostMutationDefiniteRejectionKeepsPublished(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rejected bool
		want     string
	}{
		{name: "48 hour deletion refusal", rejected: true, want: "published"},
		{name: "message not found", rejected: true, want: "published"},
		{name: "ambiguous failure", want: "uncertain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			biz := uuid.New()
			repo := &mutationPostRepo{post: domain.Post{ID: "p", BusinessID: biz.String(), Status: "published", PlatformResults: map[string]domain.PlatformResult{"telegram": {ExternalID: "@own", PostID: "42", Status: "published"}}}}
			response := &a2a.ToolResponse{Success: false, Code: "transient", Error: tc.name, Result: map[string]interface{}{"mutation_rejected": tc.rejected}}
			svc := WithPostDispatcher(NewPostService(repo, nil), &mutationRequester{response: response})
			ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: biz, RoleID: uuid.MustParse(domain.SystemRoleOwnerID), Permissions: []authz.Permission{authz.PermContentDelete}})
			got, err := svc.Mutate(ctx, biz, "p", "delete", nil)
			require.NoError(t, err)
			require.Equal(t, tc.want, got.Status)
			require.Equal(t, tc.want, got.PlatformResults["telegram"].Status)
			require.Contains(t, got.PlatformResults["telegram"].Error, tc.name)
		})
	}
}
