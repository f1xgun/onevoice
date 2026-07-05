// Package service — owner_brief.go.
//
// OwnerBriefService composes and dispatches a proactive weekly brief DM to each
// eligible business owner's private Telegram chat. It reads only existing
// aggregates (review reputation numbers), composes the brief in the org brand
// voice through the EXISTING metered llm.Router (degrading to a deterministic
// ru/en template on any error), and dispatches telegram__send_notification over
// NATS. It writes ZERO new billing code: metering fires automatically because
// the compose path sets ChatRequest.BusinessID on the WithBilling router.
package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

// ownerBriefConcurrency bounds how many businesses a single RunOnce pass
// composes+dispatches at once, so a fleet-wide weekly tick never fans out an
// unbounded burst of LLM calls and NATS dispatches at the platform agents.
const ownerBriefConcurrency = 4

// ownerBriefDispatchTimeout caps the per-business NATS request budget for the
// send_notification dispatch.
const ownerBriefDispatchTimeout = 30 * time.Second

// ownerBriefTelemetryEventType and ownerBriefTelemetryAction name the server-
// side telemetry event emitted on a successful send, powering the
// brief_sent → session → action funnel.
const (
	ownerBriefTelemetryEventType = "owner_brief"
	ownerBriefTelemetryAction    = "owner_brief_sent"
)

// ownerBriefStatsFetcher loads the aggregate-only reputation stats for a
// business. *OwnerBriefStatsRepo satisfies it; a fake satisfies it in tests.
type ownerBriefStatsFetcher interface {
	FetchStats(ctx context.Context, businessID string, now time.Time) (OwnerBriefStats, error)
}

// ownerBriefTelemetrySink is the narrow slice of TelemetryService the brief
// worker emits through. *TelemetryService satisfies it.
type ownerBriefTelemetrySink interface {
	Ingest(ctx context.Context, userID uuid.UUID, events []TelemetryEvent) error
}

// OwnerBriefService is the weekly-owner-brief worker's business logic. It is a
// no-op (RunOnce returns nil) when NATS is unavailable, so a Mongo-only deploy
// simply never sends.
type OwnerBriefService struct {
	integRepo    domain.IntegrationRepository
	businessRepo domain.BusinessRepository
	stats        ownerBriefStatsFetcher
	router       OwnerBriefRouter // nil = always use the templated fallback
	model        string
	nc           natsRequester // nil = no dispatch (Mongo-only mode)
	telemetry    ownerBriefTelemetrySink
	now          func() time.Time // injectable clock for deterministic tests
}

// NewOwnerBriefService constructs an OwnerBriefService. A nil router (or empty
// model) leaves the worker on the templated-fallback path; a nil nc disables
// dispatch. The clock defaults to time.Now.
func NewOwnerBriefService(
	integRepo domain.IntegrationRepository,
	businessRepo domain.BusinessRepository,
	stats ownerBriefStatsFetcher,
	router OwnerBriefRouter,
	model string,
	nc natsRequester,
	telemetry ownerBriefTelemetrySink,
) *OwnerBriefService {
	return &OwnerBriefService{
		integRepo:    integRepo,
		businessRepo: businessRepo,
		stats:        stats,
		router:       router,
		model:        model,
		nc:           nc,
		telemetry:    telemetry,
		now:          time.Now,
	}
}

// RunOnce runs one weekly-brief pass over the fleet: it enumerates every active
// Telegram integration, due-selects the businesses whose brief is enabled, has a
// private owner recipient, matches the configured weekday/hour window, and has
// not already been sent this ISO week, then composes+dispatches+stamps each in
// bounded parallel. Per-business errors are logged and never abort the pass.
func (s *OwnerBriefService) RunOnce(ctx context.Context) error {
	if s == nil || s.nc == nil {
		return nil
	}

	integrations, err := s.integRepo.ListAllActiveByPlatforms(ctx, []string{a2a.AgentTelegram})
	if err != nil {
		return fmt.Errorf("owner brief: list integrations: %w", err)
	}

	targets := dedupeOwnerBriefTargets(integrations)

	sem := make(chan struct{}, ownerBriefConcurrency)
	var wg sync.WaitGroup
	for i := range targets {
		t := targets[i]
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.processBusiness(ctx, t); err != nil {
				slog.ErrorContext(ctx, "owner brief: business pass failed",
					"business_id", t.businessID, "error", err)
			}
		}()
	}
	wg.Wait()
	return nil
}

// ownerBriefTarget carries the trusted per-integration context the pass needs:
// the acting business id and the owner's private Telegram chat id (from the
// integration metadata). Both travel together from one trusted DB row, so there
// is no cross-tenant bleed of chat id vs aggregate.
type ownerBriefTarget struct {
	businessID uuid.UUID
	chatID     string
}

// dedupeOwnerBriefTargets collapses the active Telegram integrations to one
// target per business, keeping only those that carry a usable private
// telegram_user_id in metadata. A business whose owner never supplied their
// private Telegram id is silently ineligible — the brief must never fall back to
// the public channel external_id as the DM target (owner-private content leak).
func dedupeOwnerBriefTargets(integrations []domain.Integration) []ownerBriefTarget {
	seen := make(map[uuid.UUID]bool, len(integrations))
	out := make([]ownerBriefTarget, 0, len(integrations))
	for i := range integrations {
		integ := integrations[i]
		if seen[integ.BusinessID] {
			continue
		}
		chatID := telegramUserIDFromMetadata(integ.Metadata)
		if chatID == "" {
			continue
		}
		seen[integ.BusinessID] = true
		out = append(out, ownerBriefTarget{businessID: integ.BusinessID, chatID: chatID})
	}
	return out
}

// telegramUserIDFromMetadata extracts the owner's private Telegram numeric id
// from integration metadata, returning "" when absent or blank.
func telegramUserIDFromMetadata(meta map[string]interface{}) string {
	if meta == nil {
		return ""
	}
	id, _ := meta["telegram_user_id"].(string)
	return strings.TrimSpace(id)
}

// processBusiness runs the full per-business pipeline: load the business, apply
// due-selection (enabled + weekday/hour window + not-sent-this-week), compose or
// fall back, dispatch the DM, stamp the ISO week on success, and emit telemetry.
// A business that is not due returns nil without any side effect.
func (s *OwnerBriefService) processBusiness(ctx context.Context, t ownerBriefTarget) error {
	now := s.now()

	biz, err := s.businessRepo.GetByID(ctx, t.businessID)
	if err != nil {
		return fmt.Errorf("load business: %w", err)
	}

	pref := platform.OwnerBriefFromSettings(biz.Settings)
	if !pref.Enabled {
		return nil
	}

	isoWeek := isoYearWeek(now)
	lastSent := platform.OwnerBriefLastSentFromSettings(biz.Settings)
	if lastSent == isoWeek {
		return nil
	}

	if !briefWindowMatches(now, pref.Weekday, pref.Hour) {
		return nil
	}

	stats, err := s.stats.FetchStats(ctx, t.businessID.String(), now)
	if err != nil {
		return fmt.Errorf("fetch stats: %w", err)
	}

	tag := briefLocale(biz)
	firstBrief := lastSent == ""
	text, usedLLM := composeBrief(ctx, s.router, s.model, biz, stats, tag, firstBrief)

	if err := s.dispatch(ctx, t, isoWeek, text); err != nil {
		return fmt.Errorf("dispatch: %w", err)
	}

	if err := s.stampSent(ctx, biz, isoWeek); err != nil {
		return fmt.Errorf("stamp last-sent: %w", err)
	}

	s.emitTelemetry(ctx, t.businessID, usedLLM)
	return nil
}

// dispatch sends the composed brief as a private Telegram DM through
// telegram__send_notification. The approvalID is stable per (business, week) so
// a NATS retry dedupes at the agent and never double-DMs the owner within a
// week.
func (s *OwnerBriefService) dispatch(ctx context.Context, t ownerBriefTarget, isoWeek, text string) error {
	args := map[string]interface{}{
		"text":    text,
		"chat_id": t.chatID,
	}
	approvalID := fmt.Sprintf("owner-brief-%s-%s", t.businessID, isoWeek)
	_, err := dispatchToolWithApproval(ctx, s.nc, a2a.AgentTelegram, tools.TelegramSendNotification,
		args, t.businessID.String(), approvalID, ownerBriefDispatchTimeout)
	return err
}

// stampSent persists the current ISO week as ownerBriefLastSent so a restart or
// a later tick within the same week skips this business. It writes only the one
// sub-key via the concurrency-safe jsonb_set path (no migration). actorUserID is
// uuid.Nil — this is a system write, not a user action.
func (s *OwnerBriefService) stampSent(ctx context.Context, biz *domain.Business, isoWeek string) error {
	return s.businessRepo.UpdateSettingsKeys(ctx, biz.ID,
		map[string]interface{}{platform.OwnerBriefLastSentSettingsKey: isoWeek})
}

// emitTelemetry records a best-effort owner_brief_sent event. business_id lives
// in the metadata (the telemetry row's business_id column is NULL on the
// server-emitted path); mode distinguishes an AI-composed brief from the
// templated fallback for the funnel. A telemetry error never fails the send.
func (s *OwnerBriefService) emitTelemetry(ctx context.Context, businessID uuid.UUID, usedLLM bool) {
	if s.telemetry == nil {
		return
	}
	mode := "template"
	if usedLLM {
		mode = "llm"
	}
	if err := s.telemetry.Ingest(ctx, uuid.Nil, []TelemetryEvent{{
		EventType: ownerBriefTelemetryEventType,
		Action:    ownerBriefTelemetryAction,
		Metadata: map[string]string{
			"business_id": businessID.String(),
			"mode":        mode,
		},
	}}); err != nil {
		slog.WarnContext(ctx, "owner brief: telemetry emit failed",
			"business_id", businessID, "error", err)
	}
}

// briefWindowMatches reports whether now falls in the configured weekday+hour
// window. The hourly poll fires this once per week per business: weekday matches
// the day and hour matches the hour-of-day, so a business is dispatched in the
// single hour of its chosen day.
func briefWindowMatches(now time.Time, weekday, hour int) bool {
	return int(now.Weekday()) == weekday && now.Hour() == hour
}

// isoYearWeek renders now as the "<ISO year>-W<week>" stamp used for weekly
// idempotency. ISO week rules put the last days of December in week 1 of the
// next year (and vice versa), so the ISO year — not the calendar year — is the
// correct pairing to avoid a year-boundary double-send.
func isoYearWeek(now time.Time) string {
	year, week := now.ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// briefLocale resolves the brief language for a business. There is no per-
// business locale field, so v1 defaults to Russian (i18n.DefaultTag). The
// composer and template accept any tag, so an owner-locale enrichment can be
// added later without touching the compose path.
func briefLocale(_ *domain.Business) language.Tag {
	return language.Russian
}
