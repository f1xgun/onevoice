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
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

const maxTelegramDescription = 255

// integrationProvider fetches integration data for a business.
type integrationProvider interface {
	ListByBusinessID(ctx context.Context, businessID uuid.UUID) ([]domain.Integration, error)
	GetDecryptedToken(ctx context.Context, businessID uuid.UUID, platform, externalID string) (accessToken string, err error)
}

// taskRecorder creates AgentTask records for sync operations.
type taskRecorder interface {
	Create(ctx context.Context, task *domain.AgentTask) error
}

// Syncer pushes business data updates to connected platform channels.
type Syncer struct {
	integrations integrationProvider
	tasks        taskRecorder // optional; may be nil
	hub          *taskhub.Hub // optional; may be nil
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

// SetTaskRecorder sets the optional AgentTask recorder for tracking sync operations.
func (s *Syncer) SetTaskRecorder(tasks taskRecorder) {
	s.tasks = tasks
}

// SetTaskHub sets the optional TaskHub that fans out task lifecycle events
// to SSE subscribers on the Tasks page.
func (s *Syncer) SetTaskHub(hub *taskhub.Hub) {
	s.hub = hub
}

// recordTask creates an AgentTask record (if a recorder is configured) for a
// sync operation that has already completed. startedAt is captured before the
// operation so the stored duration is meaningful. displayName is the human
// label shown on the Tasks page — callers pass the Russian string directly.
func (s *Syncer) recordTask(ctx context.Context, businessID uuid.UUID, platform, taskType, displayName, status string, input interface{}, errMsg string, startedAt time.Time) {
	if s.tasks == nil {
		return
	}
	completedAt := time.Now()
	task := &domain.AgentTask{
		BusinessID:  businessID.String(),
		Type:        taskType,
		DisplayName: displayName,
		Status:      status,
		Platform:    platform,
		Input:       input,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
		CreatedAt:   completedAt,
		Error:       errMsg,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		slog.ErrorContext(ctx, "platform sync: failed to record task", "error", err)
		return
	}
	if s.hub != nil {
		s.hub.Publish(businessID.String(), taskhub.Event{Kind: taskhub.KindCreated, Task: *task})
	}
}

// SyncBusiness pushes the updated business info to all active connected platforms.
// Designed to run in a goroutine (fire-and-forget); errors are only logged.
func (s *Syncer) SyncBusiness(business *domain.Business) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	integrations, err := s.integrations.ListByBusinessID(ctx, business.ID)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: failed to list integrations", "business_id", business.ID, "error", err)
		return
	}

	for _, integ := range integrations {
		if integ.Status != "active" {
			continue
		}
		switch integ.Platform {
		case "telegram":
			titleStart := time.Now()
			if err := s.syncTelegramTitle(ctx, business.ID, integ.ExternalID, business.Name); err != nil {
				s.recordTask(ctx, business.ID, "telegram", "sync_title", "Синхронизация названия", "error",
					map[string]string{"channel_id": integ.ExternalID}, err.Error(), titleStart)
			} else {
				s.recordTask(ctx, business.ID, "telegram", "sync_title", "Синхронизация названия", "done",
					map[string]string{"channel_id": integ.ExternalID, "name": business.Name}, "", titleStart)
			}

			descStart := time.Now()
			if err := s.syncTelegramDescription(ctx, business.ID, integ.ExternalID, formatTelegramDescription(business)); err != nil {
				s.recordTask(ctx, business.ID, "telegram", "sync_description", "Синхронизация описания", "error",
					map[string]string{"channel_id": integ.ExternalID}, err.Error(), descStart)
			} else {
				s.recordTask(ctx, business.ID, "telegram", "sync_description", "Синхронизация описания", "done",
					map[string]string{"channel_id": integ.ExternalID}, "", descStart)
			}

			if business.LogoURL != "" {
				photoStart := time.Now()
				if err := s.syncTelegramPhoto(ctx, business.ID, integ.ExternalID, business.LogoURL); err != nil {
					s.recordTask(ctx, business.ID, "telegram", "sync_photo", "Синхронизация фото", "error",
						map[string]string{"channel_id": integ.ExternalID}, err.Error(), photoStart)
				} else {
					s.recordTask(ctx, business.ID, "telegram", "sync_photo", "Синхронизация фото", "done",
						map[string]string{"channel_id": integ.ExternalID}, "", photoStart)
				}
			}
		case "vk":
			s.syncVKInfo(ctx, business, integ.ExternalID)
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

func (s *Syncer) syncTelegramTitle(ctx context.Context, businessID uuid.UUID, channelID, title string) error {
	botToken, err := s.integrations.GetDecryptedToken(ctx, businessID, "telegram", channelID)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("get token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatTitle?chat_id=%s&title=%s",
		s.telegramBase,
		botToken,
		url.QueryEscape(channelID),
		url.QueryEscape(title),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatTitle build request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatTitle request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatTitle response parse failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		slog.WarnContext(ctx, "platform sync: telegram setChatTitle returned not ok", "channel_id", channelID)
		return fmt.Errorf("setChatTitle returned not ok")
	}
	slog.Info("platform sync: telegram title updated", "channel_id", channelID)
	return nil
}

func (s *Syncer) syncTelegramDescription(ctx context.Context, businessID uuid.UUID, channelID, description string) error {
	botToken, err := s.integrations.GetDecryptedToken(ctx, businessID, "telegram", channelID)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("get token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatDescription?chat_id=%s&description=%s",
		s.telegramBase,
		botToken,
		url.QueryEscape(channelID),
		url.QueryEscape(description),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatDescription build request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatDescription request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatDescription response parse failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		slog.WarnContext(ctx, "platform sync: telegram setChatDescription returned not ok", "channel_id", channelID)
		return fmt.Errorf("setChatDescription returned not ok")
	}
	slog.Info("platform sync: telegram description updated", "channel_id", channelID)
	return nil
}

func (s *Syncer) syncTelegramPhoto(ctx context.Context, businessID uuid.UUID, channelID, logoURL string) error {
	botToken, err := s.integrations.GetDecryptedToken(ctx, businessID, "telegram", channelID)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("get token: %w", err)
	}

	// Resolve relative paths to absolute URL using publicURL
	fullURL := logoURL
	if logoURL != "" && logoURL[0] == '/' {
		fullURL = s.publicURL + logoURL
	}

	// Download the image
	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: build image download request failed", "error", err)
		return fmt.Errorf("build image download request: %w", err)
	}
	imgResp, err := s.httpClient.Do(imgReq)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: download image failed", "url", fullURL, "error", err)
		return fmt.Errorf("download image: %w", err)
	}
	defer func() { _ = imgResp.Body.Close() }()

	// Build multipart body with the image
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("chat_id", channelID); err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: write chat_id field failed", "error", err)
		return fmt.Errorf("write chat_id field: %w", err)
	}
	fw, err := mw.CreateFormFile("photo", path.Base(logoURL))
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: create form file failed", "error", err)
		return fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, imgResp.Body); err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: copy image data failed", "error", err)
		return fmt.Errorf("copy image data: %w", err)
	}
	if err := mw.Close(); err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: close multipart writer failed", "error", err)
		return fmt.Errorf("close multipart writer: %w", err)
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatPhoto", s.telegramBase, botToken)
	photoReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &body)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: build setChatPhoto request failed", "error", err)
		return fmt.Errorf("build setChatPhoto request: %w", err)
	}
	photoReq.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := s.httpClient.Do(photoReq)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatPhoto request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("setChatPhoto request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result struct {
		OK bool `json:"ok"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatPhoto response parse failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("parse response: %w", err)
	}
	if !result.OK {
		slog.WarnContext(ctx, "platform sync: telegram setChatPhoto returned not ok", "channel_id", channelID)
		return fmt.Errorf("setChatPhoto returned not ok")
	}
	slog.Info("platform sync: telegram photo updated", "channel_id", channelID)
	return nil
}

// syncVKInfo pushes business data to VK community using dedicated API fields.
func (s *Syncer) syncVKInfo(ctx context.Context, business *domain.Business, groupID string) {
	started := time.Now()
	token, err := s.integrations.GetDecryptedToken(ctx, business.ID, "vk", groupID)
	if err != nil {
		slog.Error("platform sync: vk: get token failed", "group_id", groupID, "error", err)
		s.recordTask(ctx, business.ID, "vk", "sync_info", "Синхронизация данных", "error", map[string]string{"group_id": groupID}, "token fetch failed: "+err.Error(), started)
		return
	}

	// groups.edit supports: description, phone, website
	params := url.Values{
		"group_id":     {groupID},
		"access_token": {token},
		"v":            {"5.199"},
	}
	params.Set("description", business.Description)
	if business.Phone != "" {
		params.Set("phone", business.Phone)
	}
	if business.Website != nil && *business.Website != "" {
		params.Set("website", *business.Website)
	}

	input := map[string]string{
		"group_id":    groupID,
		"description": business.Description,
		"phone":       business.Phone,
	}
	if business.Website != nil {
		input["website"] = *business.Website
	}

	apiErr := s.callVKAPI(ctx, "groups.edit", params, groupID)
	if apiErr != "" {
		s.recordTask(ctx, business.ID, "vk", "sync_info", "Синхронизация данных", "error", input, apiErr, started)
	} else {
		s.recordTask(ctx, business.ID, "vk", "sync_info", "Синхронизация данных", "done", input, "", started)
	}
}

// callVKAPI makes a VK API request and logs the result. Returns error message or empty string on success.
func (s *Syncer) callVKAPI(ctx context.Context, method string, params url.Values, groupID string) string {
	apiURL := "https://api.vk.com/method/" + method + "?" + params.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.Error("platform sync: vk "+method+" build request failed", "group_id", groupID, "error", err)
		return err.Error()
	}
	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		slog.Error("platform sync: vk "+method+" request failed", "group_id", groupID, "error", err)
		return err.Error()
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Response interface{} `json:"response"`
		Error    *struct {
			ErrorCode int    `json:"error_code"`
			ErrorMsg  string `json:"error_msg"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		slog.Error("platform sync: vk "+method+" response parse failed", "group_id", groupID, "error", err, "body", string(respBody))
		return err.Error()
	}
	if result.Error != nil {
		slog.Error("platform sync: vk "+method+" API error", "group_id", groupID, "code", result.Error.ErrorCode, "msg", result.Error.ErrorMsg)
		return result.Error.ErrorMsg
	}
	slog.Info("platform sync: vk "+method+" success", "group_id", groupID)
	return ""
}
