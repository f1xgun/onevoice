package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

const maxTelegramDescription = 255

// integrationProvider fetches integration data for a business.
type integrationProvider interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (accessToken string, err error)
}

// Syncer pushes business data updates to connected platform channels.
type Syncer struct {
	integrations integrationProvider
	httpClient   *http.Client
	telegramBase string
	publicURL    string
}

// NewSyncer creates a Syncer. httpClient defaults to a 10-second client if nil.
// publicURL is the API's public base URL used to construct full URLs for image downloads (e.g. "http://localhost:8080").
func NewSyncer(integrations integrationProvider, httpClient *http.Client, publicURL string) *Syncer {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Syncer{
		integrations: integrations,
		httpClient:   httpClient,
		telegramBase: "https://api.telegram.org",
		publicURL:    publicURL,
	}
}

// SyncBusiness pushes the updated business info to all active connected platforms.
// Designed to run in a goroutine (fire-and-forget); errors are only logged.
func (s *Syncer) SyncBusiness(business *domain.Business) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	integrations, err := s.integrations.ListByBusinessID(ctx, business.ID)
	if err != nil {
		slog.Error("platform sync: failed to list integrations", "business_id", business.ID, "error", err)
		return
	}

	for _, integ := range integrations {
		if integ.Status != "active" {
			continue
		}
		// VK: external_id is "default" (no real group_id stored at connect time), skip for now
		if integ.Platform == "telegram" {
			s.syncTelegramTitle(ctx, business.ID, integ.ExternalID, business.Name)
			s.syncTelegramDescription(ctx, business.ID, integ.ExternalID, formatTelegramDescription(business))
			if business.LogoURL != "" {
				s.syncTelegramPhoto(ctx, business.ID, integ.ExternalID, business.LogoURL)
			}
		}
	}
}

// formatTelegramDescription builds a compact Telegram channel description from all business fields.
// Telegram's description limit is 255 characters.
func formatTelegramDescription(b *domain.Business) string {
	var parts []string

	if b.Description != "" {
		parts = append(parts, b.Description)
	}

	var contact []string
	if b.Phone != "" {
		contact = append(contact, "📞 "+b.Phone)
	}
	if b.Website != nil && *b.Website != "" {
		contact = append(contact, "🌐 "+*b.Website)
	}
	if b.Address != "" {
		contact = append(contact, "📍 "+b.Address)
	}
	if len(contact) > 0 {
		parts = append(parts, strings.Join(contact, "\n"))
	}

	if sched := formatSchedule(b.Settings); sched != "" {
		parts = append(parts, sched)
	}

	result := strings.Join(parts, "\n\n")

	// Truncate to Telegram's limit
	runes := []rune(result)
	if len(runes) > maxTelegramDescription {
		result = string(runes[:maxTelegramDescription-1]) + "…"
	}

	return result
}

var dayOrder = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
var dayRU = map[string]string{
	"mon": "Пн", "tue": "Вт", "wed": "Ср", "thu": "Чт",
	"fri": "Пт", "sat": "Сб", "sun": "Вс",
}

// formatSchedule converts the schedule stored in Settings["schedule"] into a compact string.
// Groups consecutive days with identical hours: "Пн-Пт 09:00-21:00, Сб 10:00-18:00".
func formatSchedule(settings map[string]interface{}) string {
	if settings == nil {
		return ""
	}
	raw, ok := settings["schedule"]
	if !ok || raw == nil {
		return ""
	}

	data, err := json.Marshal(raw)
	if err != nil {
		return ""
	}

	var days []struct {
		Day    string `json:"day"`
		Open   string `json:"open"`
		Close  string `json:"close"`
		Closed bool   `json:"closed"`
	}
	if err := json.Unmarshal(data, &days); err != nil {
		return ""
	}

	// Index by day key
	type slot struct{ open, close string }
	byDay := make(map[string]slot)
	for _, d := range days {
		if !d.Closed {
			byDay[d.Day] = slot{d.Open, d.Close}
		}
	}

	// Walk dayOrder and group consecutive days with identical hours
	type group struct {
		start, end string
		open, cls  string
	}
	groups := make([]group, 0, len(dayOrder))
	for _, key := range dayOrder {
		s, open := byDay[key]
		if !open {
			continue
		}
		if len(groups) > 0 {
			last := &groups[len(groups)-1]
			if last.open == s.open && last.cls == s.close {
				last.end = key
				continue
			}
		}
		groups = append(groups, group{start: key, end: key, open: s.open, cls: s.close})
	}

	if len(groups) == 0 {
		return ""
	}

	segments := make([]string, 0, len(groups))
	for _, g := range groups {
		label := dayRU[g.start]
		if g.end != g.start {
			label += "-" + dayRU[g.end]
		}
		segments = append(segments, fmt.Sprintf("%s %s-%s", label, g.open, g.cls))
	}
	return "⏰ " + strings.Join(segments, ", ")
}

func (s *Syncer) syncTelegramTitle(ctx context.Context, businessID uuid.UUID, channelID, title string) {
	botToken, err := s.integrations.GetDecryptedToken(ctx, businessID, "telegram", channelID)
	if err != nil {
		slog.Error("platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatTitle?chat_id=%s&title=%s",
		s.telegramBase,
		botToken,
		url.QueryEscape(channelID),
		url.QueryEscape(title),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.Error("platform sync: telegram setChatTitle build request failed", "channel_id", channelID, "error", err)
		return
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		slog.Error("platform sync: telegram setChatTitle request failed", "channel_id", channelID, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("platform sync: telegram setChatTitle response parse failed", "channel_id", channelID, "error", err)
		return
	}
	if !result.OK {
		slog.Warn("platform sync: telegram setChatTitle returned not ok", "channel_id", channelID)
		return
	}
	slog.Info("platform sync: telegram title updated", "channel_id", channelID)
}

func (s *Syncer) syncTelegramDescription(ctx context.Context, businessID uuid.UUID, channelID, description string) {
	botToken, err := s.integrations.GetDecryptedToken(ctx, businessID, "telegram", channelID)
	if err != nil {
		slog.Error("platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatDescription?chat_id=%s&description=%s",
		s.telegramBase,
		botToken,
		url.QueryEscape(channelID),
		url.QueryEscape(description),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.Error("platform sync: telegram setChatDescription build request failed", "channel_id", channelID, "error", err)
		return
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		slog.Error("platform sync: telegram setChatDescription request failed", "channel_id", channelID, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("platform sync: telegram setChatDescription response parse failed", "channel_id", channelID, "error", err)
		return
	}
	if !result.OK {
		slog.Warn("platform sync: telegram setChatDescription returned not ok", "channel_id", channelID)
		return
	}
	slog.Info("platform sync: telegram description updated", "channel_id", channelID)
}

func (s *Syncer) syncTelegramPhoto(ctx context.Context, businessID uuid.UUID, channelID, logoURL string) {
	botToken, err := s.integrations.GetDecryptedToken(ctx, businessID, "telegram", channelID)
	if err != nil {
		slog.Error("platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return
	}

	// Resolve relative paths to absolute URL using publicURL
	fullURL := logoURL
	if logoURL != "" && logoURL[0] == '/' {
		fullURL = s.publicURL + logoURL
	}

	// Download the image
	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		slog.Error("platform sync: telegram: build image download request failed", "error", err)
		return
	}
	imgResp, err := s.httpClient.Do(imgReq)
	if err != nil {
		slog.Error("platform sync: telegram: download image failed", "url", fullURL, "error", err)
		return
	}
	defer func() { _ = imgResp.Body.Close() }()

	// Build multipart body with the image
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("chat_id", channelID); err != nil {
		slog.Error("platform sync: telegram: write chat_id field failed", "error", err)
		return
	}
	fw, err := mw.CreateFormFile("photo", path.Base(logoURL))
	if err != nil {
		slog.Error("platform sync: telegram: create form file failed", "error", err)
		return
	}
	if _, err := io.Copy(fw, imgResp.Body); err != nil {
		slog.Error("platform sync: telegram: copy image data failed", "error", err)
		return
	}
	if err := mw.Close(); err != nil {
		slog.Error("platform sync: telegram: close multipart writer failed", "error", err)
		return
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatPhoto", s.telegramBase, botToken)
	photoReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &body)
	if err != nil {
		slog.Error("platform sync: telegram: build setChatPhoto request failed", "error", err)
		return
	}
	photoReq.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := s.httpClient.Do(photoReq)
	if err != nil {
		slog.Error("platform sync: telegram setChatPhoto request failed", "channel_id", channelID, "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("platform sync: telegram setChatPhoto response parse failed", "channel_id", channelID, "error", err)
		return
	}
	if !result.OK {
		slog.Warn("platform sync: telegram setChatPhoto returned not ok", "channel_id", channelID)
		return
	}
	slog.Info("platform sync: telegram photo updated", "channel_id", channelID)
}
