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
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/domain"
)

// defaultTelegramAPIBase is the public Bot API root. Overridable via
// constructor for tests pointing at a mock server.
const defaultTelegramAPIBase = "https://api.telegram.org"

// defaultHTTPClientTimeout matches the prior Syncer default.
const defaultHTTPClientTimeout = 10 * time.Second

// TelegramSyncer pushes business updates to Telegram channels via the Bot API.
// Implements TitleSyncer + DescriptionSyncer + PhotoSyncer; Telegram has no
// schedule API so ScheduleSyncer is intentionally not implemented.
type TelegramSyncer struct {
	integrations integrationProvider
	httpClient   *http.Client
	telegramBase string
	publicURL    string
}

// Compile-time interface assertions.
var _ TitleSyncer = (*TelegramSyncer)(nil)
var _ DescriptionSyncer = (*TelegramSyncer)(nil)
var _ PhotoSyncer = (*TelegramSyncer)(nil)

// NewTelegramSyncer wires a TelegramSyncer. integrations is required;
// httpClient defaults to a 10s client; telegramBase defaults to the public
// Bot API root; publicURL is the API's public base URL used to resolve
// relative logo paths to absolute URLs for setChatPhoto.
func NewTelegramSyncer(integrations integrationProvider, httpClient *http.Client, telegramBase, publicURL string) *TelegramSyncer {
	if integrations == nil {
		panic("platform.NewTelegramSyncer: integrations cannot be nil")
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPClientTimeout}
	}
	if telegramBase == "" {
		telegramBase = defaultTelegramAPIBase
	}
	return &TelegramSyncer{
		integrations: integrations,
		httpClient:   httpClient,
		telegramBase: telegramBase,
		publicURL:    publicURL,
	}
}

// SyncTitle updates the Telegram channel's title via setChatTitle.
func (t *TelegramSyncer) SyncTitle(ctx context.Context, b *domain.Business, integ domain.Integration) error {
	return t.syncTelegramTitle(ctx, b.ID, integ.ExternalID, b.Name)
}

// SyncDescription updates the Telegram channel's description via
// setChatDescription. The description is built (and truncated to 255 chars)
// by formatTelegramDescription so all business contact fields ship together.
func (t *TelegramSyncer) SyncDescription(ctx context.Context, b *domain.Business, integ domain.Integration) error {
	return t.syncTelegramDescription(ctx, b.ID, integ.ExternalID, formatTelegramDescription(b))
}

// SyncPhoto updates the Telegram channel's photo via setChatPhoto. Caller
// must guarantee b.LogoURL != "" — the dispatcher already filters that case.
func (t *TelegramSyncer) SyncPhoto(ctx context.Context, b *domain.Business, integ domain.Integration) error {
	return t.syncTelegramPhoto(ctx, b.ID, integ.ExternalID, b.LogoURL)
}

func (t *TelegramSyncer) syncTelegramTitle(ctx context.Context, businessID uuid.UUID, channelID, title string) error {
	botToken, err := t.integrations.GetDecryptedToken(ctx, businessID, a2a.AgentTelegram, channelID, reasonTelegramSyncChats)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("get token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatTitle?chat_id=%s&title=%s",
		t.telegramBase,
		botToken,
		url.QueryEscape(channelID),
		url.QueryEscape(title),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatTitle build request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := t.httpClient.Do(httpReq)
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

func (t *TelegramSyncer) syncTelegramDescription(ctx context.Context, businessID uuid.UUID, channelID, description string) error {
	botToken, err := t.integrations.GetDecryptedToken(ctx, businessID, a2a.AgentTelegram, channelID, reasonTelegramSyncChannelMeta)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("get token: %w", err)
	}

	apiURL := fmt.Sprintf("%s/bot%s/setChatDescription?chat_id=%s&description=%s",
		t.telegramBase,
		botToken,
		url.QueryEscape(channelID),
		url.QueryEscape(description),
	)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram setChatDescription build request failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := t.httpClient.Do(httpReq)
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

func (t *TelegramSyncer) syncTelegramPhoto(ctx context.Context, businessID uuid.UUID, channelID, logoURL string) error {
	botToken, err := t.integrations.GetDecryptedToken(ctx, businessID, a2a.AgentTelegram, channelID, reasonTelegramSyncUsers)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: get token failed", "channel_id", channelID, "error", err)
		return fmt.Errorf("get token: %w", err)
	}

	fullURL := logoURL
	if logoURL != "" && logoURL[0] == '/' {
		fullURL = t.publicURL + logoURL
	}

	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, http.NoBody)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: build image download request failed", "error", err)
		return fmt.Errorf("build image download request: %w", err)
	}
	imgResp, err := t.httpClient.Do(imgReq)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: download image failed", "url", fullURL, "error", err)
		return fmt.Errorf("download image: %w", err)
	}
	defer func() { _ = imgResp.Body.Close() }()

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

	apiURL := fmt.Sprintf("%s/bot%s/setChatPhoto", t.telegramBase, botToken)
	photoReq, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, &body)
	if err != nil {
		slog.ErrorContext(ctx, "platform sync: telegram: build setChatPhoto request failed", "error", err)
		return fmt.Errorf("build setChatPhoto request: %w", err)
	}
	photoReq.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := t.httpClient.Do(photoReq)
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
