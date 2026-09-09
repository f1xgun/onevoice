package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

const (
	MaxContentTemplates          = 50
	MaxContentTemplateNameRunes  = 80
	MaxContentTemplateBodyBytes  = 16 * 1024
	MaxContentTemplateVariables  = 10
	MaxContentTemplateValueRunes = 512
)

var contentTemplatePlaceholder = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

var ErrInvalidContentTemplate = errors.New("invalid content template")

func invalidContentTemplate(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidContentTemplate, reason)
}

type ContentTemplateInput struct {
	Name         string
	Kind         domain.ContentTemplateKind
	Body         string
	Placeholders []string
}

type ContentTemplateService struct {
	repo    domain.ContentTemplateRepository
	posts   domain.PostRepository
	reviews domain.ReviewRepository
}

func NewContentTemplateService(repo domain.ContentTemplateRepository, posts domain.PostRepository, reviews domain.ReviewRepository) *ContentTemplateService {
	return &ContentTemplateService{repo: repo, posts: posts, reviews: reviews}
}

func normalizeContentTemplateInput(input ContentTemplateInput) (ContentTemplateInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" || utf8.RuneCountInString(input.Name) > MaxContentTemplateNameRunes {
		return input, invalidContentTemplate("invalid template name")
	}
	if input.Kind != domain.ContentTemplateKindPost && input.Kind != domain.ContentTemplateKindReviewReply {
		return input, invalidContentTemplate("invalid template kind")
	}
	if strings.TrimSpace(input.Body) == "" || len(input.Body) > MaxContentTemplateBodyBytes {
		return input, invalidContentTemplate("invalid template body")
	}
	if len(input.Placeholders) > MaxContentTemplateVariables {
		return input, invalidContentTemplate("too many placeholders")
	}
	if input.Placeholders == nil {
		input.Placeholders = []string{}
	}
	declared := make(map[string]struct{}, len(input.Placeholders))
	for _, placeholder := range input.Placeholders {
		if !contentTemplatePlaceholder.MatchString(placeholder) {
			return input, invalidContentTemplate("invalid placeholder")
		}
		if _, exists := declared[placeholder]; exists {
			return input, invalidContentTemplate("duplicate placeholder")
		}
		declared[placeholder] = struct{}{}
	}
	referenced, err := referencedPlaceholders(input.Body)
	if err != nil {
		return input, err
	}
	if len(referenced) != len(declared) {
		return input, invalidContentTemplate("placeholder declaration mismatch")
	}
	for key := range referenced {
		if _, ok := declared[key]; !ok {
			return input, invalidContentTemplate("undeclared placeholder")
		}
	}
	sort.Strings(input.Placeholders)
	return input, nil
}

func referencedPlaceholders(body string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	for i := 0; i < len(body); {
		switch body[i] {
		case '{':
			if i+1 < len(body) && body[i+1] == '{' {
				i += 2
				continue
			}
			closeOffset := strings.IndexByte(body[i+1:], '}')
			if closeOffset < 0 {
				return nil, invalidContentTemplate("malformed placeholder")
			}
			closing := i + 1 + closeOffset
			key := body[i+1 : closing]
			if !contentTemplatePlaceholder.MatchString(key) || strings.ContainsAny(key, "{}") {
				return nil, invalidContentTemplate("malformed placeholder")
			}
			out[key] = struct{}{}
			i = closing + 1
		case '}':
			if i+1 < len(body) && body[i+1] == '}' {
				i += 2
				continue
			}
			return nil, invalidContentTemplate("malformed placeholder")
		default:
			i++
		}
	}
	return out, nil
}

func escapeLiteralBraces(body string) string {
	body = strings.ReplaceAll(body, "{", "{{")
	return strings.ReplaceAll(body, "}", "}}")
}

func (s *ContentTemplateService) List(ctx context.Context, businessID uuid.UUID) ([]domain.ContentTemplate, error) {
	return s.repo.ListByBusinessID(ctx, businessID)
}

func (s *ContentTemplateService) Get(ctx context.Context, businessID, id uuid.UUID) (*domain.ContentTemplate, error) {
	return s.repo.GetByID(ctx, businessID, id)
}

func (s *ContentTemplateService) Create(ctx context.Context, businessID, userID uuid.UUID, input ContentTemplateInput) (*domain.ContentTemplate, error) {
	normalized, err := normalizeContentTemplateInput(input)
	if err != nil {
		return nil, err
	}
	template := &domain.ContentTemplate{BusinessID: businessID, CreatedBy: userID, Name: normalized.Name,
		Kind: normalized.Kind, Body: normalized.Body, Placeholders: normalized.Placeholders}
	if err := s.repo.Create(ctx, template, MaxContentTemplates); err != nil {
		return nil, fmt.Errorf("create content template: %w", err)
	}
	return template, nil
}

func (s *ContentTemplateService) Update(ctx context.Context, businessID, id uuid.UUID, input ContentTemplateInput) (*domain.ContentTemplate, error) {
	normalized, err := normalizeContentTemplateInput(input)
	if err != nil {
		return nil, err
	}
	template := &domain.ContentTemplate{ID: id, BusinessID: businessID, Name: normalized.Name,
		Kind: normalized.Kind, Body: normalized.Body, Placeholders: normalized.Placeholders}
	if err := s.repo.Update(ctx, template); err != nil {
		return nil, fmt.Errorf("update content template: %w", err)
	}
	return template, nil
}

func (s *ContentTemplateService) Delete(ctx context.Context, businessID, id uuid.UUID) error {
	return s.repo.Delete(ctx, businessID, id)
}

func (s *ContentTemplateService) CreateFromPost(ctx context.Context, businessID, userID uuid.UUID, sourceID, name string) (*domain.ContentTemplate, error) {
	post, err := s.posts.GetByID(ctx, sourceID)
	if errors.Is(err, domain.ErrPostNotFound) || (err == nil && (post == nil || post.BusinessID != businessID.String())) {
		return nil, domain.ErrPostNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get source post: %w", err)
	}
	return s.Create(ctx, businessID, userID, ContentTemplateInput{Name: name, Kind: domain.ContentTemplateKindPost, Body: escapeLiteralBraces(post.Content), Placeholders: []string{}})
}

func (s *ContentTemplateService) CreateFromReview(ctx context.Context, businessID, userID uuid.UUID, sourceID, name string) (*domain.ContentTemplate, error) {
	review, err := s.reviews.GetByID(ctx, sourceID)
	if errors.Is(err, domain.ErrReviewNotFound) || (err == nil && (review == nil || review.BusinessID != businessID.String())) {
		return nil, domain.ErrReviewNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get source review: %w", err)
	}
	if strings.TrimSpace(review.ReplyText) == "" {
		return nil, invalidContentTemplate("review has no persisted reply")
	}
	return s.Create(ctx, businessID, userID, ContentTemplateInput{Name: name, Kind: domain.ContentTemplateKindReviewReply, Body: escapeLiteralBraces(review.ReplyText), Placeholders: []string{}})
}

func (s *ContentTemplateService) Render(ctx context.Context, businessID, id uuid.UUID, values map[string]string) (string, error) {
	template, err := s.repo.GetByID(ctx, businessID, id)
	if err != nil {
		return "", err
	}
	if len(values) != len(template.Placeholders) {
		return "", invalidContentTemplate("placeholder values mismatch")
	}
	for _, value := range values {
		if utf8.RuneCountInString(value) > MaxContentTemplateValueRunes {
			return "", invalidContentTemplate("invalid placeholder value")
		}
	}
	for _, key := range template.Placeholders {
		if _, ok := values[key]; !ok {
			return "", invalidContentTemplate("invalid placeholder value")
		}
	}
	return renderContentTemplate(template.Body, values)
}

func renderContentTemplate(body string, values map[string]string) (string, error) {
	var rendered strings.Builder
	for i := 0; i < len(body); {
		switch {
		case body[i] == '{' && i+1 < len(body) && body[i+1] == '{':
			rendered.WriteByte('{')
			i += 2
		case body[i] == '}' && i+1 < len(body) && body[i+1] == '}':
			rendered.WriteByte('}')
			i += 2
		case body[i] == '{':
			closeOffset := strings.IndexByte(body[i+1:], '}')
			if closeOffset < 0 {
				return "", invalidContentTemplate("malformed placeholder")
			}
			closing := i + 1 + closeOffset
			value, ok := values[body[i+1:closing]]
			if !ok {
				return "", invalidContentTemplate("invalid placeholder value")
			}
			rendered.WriteString(value)
			i = closing + 1
		default:
			rendered.WriteByte(body[i])
			i++
		}
		if rendered.Len() > MaxContentTemplateBodyBytes {
			return "", invalidContentTemplate("rendered template too large")
		}
	}
	return rendered.String(), nil
}
