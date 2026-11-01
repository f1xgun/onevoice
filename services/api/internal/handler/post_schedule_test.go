package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestSchedulePostHTTP(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			business := uuid.New()
			at := time.Now().Add(time.Hour).UTC()
			called := false
			svc := &mockPostService{scheduleFn: func(_ context.Context, id uuid.UUID, target, content string, media []string, when time.Time) (*domain.Post, error) {
				called = true
				require.Equal(t, business, id)
				require.Equal(t, "@own", target)
				require.Equal(t, "caption", content)
				require.Equal(t, []string{"https://example.org/photo.png"}, media)
				require.Equal(t, at.Format(time.RFC3339), when.Format(time.RFC3339))
				return &domain.Post{ID: "queued", BusinessID: id.String(), Status: "scheduled", ScheduledAt: &when}, nil
			}}
			h, err := NewPostHandler(svc)
			require.NoError(t, err)
			perms := []authz.Permission{}
			if allowed {
				perms = []authz.Permission{authz.PermContentCreate}
			}
			ctx := authz.WithBusinessContext(context.Background(), authz.BusinessContext{BusinessID: business, Permissions: perms})
			body := fmt.Sprintf(`{"platform":"telegram","externalId":"@own","content":"caption","mediaUrls":["https://example.org/photo.png"],"scheduledAt":%q}`, at.Format(time.RFC3339))
			request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(body)).WithContext(ctx)
			response := httptest.NewRecorder()
			h.SchedulePost(response, request)
			want := http.StatusForbidden
			if allowed {
				want = http.StatusCreated
			}
			require.Equal(t, want, response.Code)
			require.Equal(t, allowed, called)
		})
	}
}
