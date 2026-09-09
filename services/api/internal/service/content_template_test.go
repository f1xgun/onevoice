package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

type contentTemplateRepoStub struct{ created *domain.ContentTemplate }

func (s *contentTemplateRepoStub) ListByBusinessID(context.Context, uuid.UUID) ([]domain.ContentTemplate, error) {
	return nil, nil
}
func (s *contentTemplateRepoStub) GetByID(context.Context, uuid.UUID, uuid.UUID) (*domain.ContentTemplate, error) {
	return nil, domain.ErrContentTemplateNotFound
}
func (s *contentTemplateRepoStub) Create(_ context.Context, template *domain.ContentTemplate, _ int) error {
	s.created = template
	template.CreatedAt = time.Now()
	template.UpdatedAt = template.CreatedAt
	return nil
}
func (s *contentTemplateRepoStub) Update(context.Context, *domain.ContentTemplate) error { return nil }
func (s *contentTemplateRepoStub) Delete(context.Context, uuid.UUID, uuid.UUID) error    { return nil }

type postRepoStub struct {
	post *domain.Post
	err  error
}

func (s postRepoStub) Create(context.Context, *domain.Post) error { return nil }
func (s postRepoStub) ListByBusinessID(context.Context, string, domain.PostFilter) ([]domain.Post, int, error) {
	return nil, 0, nil
}
func (s postRepoStub) GetByID(context.Context, string) (*domain.Post, error) { return s.post, s.err }

func TestRenderContentTemplateDoesNotExpandInsertedPlaceholderText(t *testing.T) {
	got, err := renderContentTemplate("{a} {b}", map[string]string{"a": "{b}", "b": "X"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "{b} X" {
		t.Fatalf("got %q, want %q", got, "{b} X")
	}
}

func TestLiteralBraceEscapingRoundTrip(t *testing.T) {
	source := `JSON {"enabled": true} and } stray {`
	escaped := escapeLiteralBraces(source)
	references, err := referencedPlaceholders(escaped)
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 0 {
		t.Fatalf("unexpected placeholders: %#v", references)
	}
	got, err := renderContentTemplate(escaped, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if got != source {
		t.Fatalf("got %q, want %q", got, source)
	}
}

func TestNormalizeContentTemplateNilPlaceholders(t *testing.T) {
	got, err := normalizeContentTemplateInput(ContentTemplateInput{Name: "Name", Kind: "post", Body: "Text"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Placeholders == nil || len(got.Placeholders) != 0 {
		t.Fatalf("placeholders = %#v", got.Placeholders)
	}
}

func TestRenderContentTemplateBoundsDuringConstruction(t *testing.T) {
	_, err := renderContentTemplate("{value}{value}", map[string]string{"value": strings.Repeat("x", MaxContentTemplateBodyBytes)})
	if !errors.Is(err, ErrInvalidContentTemplate) {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateFromPostEscapesLiteralBraces(t *testing.T) {
	businessID := uuid.New()
	repo := &contentTemplateRepoStub{}
	svc := NewContentTemplateService(repo, postRepoStub{post: &domain.Post{BusinessID: businessID.String(), Content: `Use {JSON}`}}, nil)
	created, err := svc.CreateFromPost(context.Background(), businessID, uuid.New(), "post-1", "Source")
	if err != nil {
		t.Fatal(err)
	}
	if created.Body != `Use {{JSON}}` {
		t.Fatalf("body = %q", created.Body)
	}
}

func TestCreateFromPostPropagatesOperationalReadError(t *testing.T) {
	want := errors.New("mongo unavailable")
	svc := NewContentTemplateService(&contentTemplateRepoStub{}, postRepoStub{err: want}, nil)
	_, err := svc.CreateFromPost(context.Background(), uuid.New(), uuid.New(), "post-1", "Source")
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped %v", err, want)
	}
}

func TestCreateFromPostNilResultIsNotFound(t *testing.T) {
	svc := NewContentTemplateService(&contentTemplateRepoStub{}, postRepoStub{}, nil)
	_, err := svc.CreateFromPost(context.Background(), uuid.New(), uuid.New(), "post-1", "Source")
	if !errors.Is(err, domain.ErrPostNotFound) {
		t.Fatalf("error = %v", err)
	}
}
