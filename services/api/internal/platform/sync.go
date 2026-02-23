package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
)

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
}

// NewSyncer creates a Syncer. httpClient defaults to a 10-second client if nil.
func NewSyncer(integrations integrationProvider, httpClient *http.Client) *Syncer {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Syncer{
		integrations: integrations,
		httpClient:   httpClient,
		telegramBase: "https://api.telegram.org",
	}
}

// SyncDescription pushes the updated business description to all active connected platforms.
// Designed to run in a goroutine (fire-and-forget); errors are only logged.
func (s *Syncer) SyncDescription(businessID uuid.UUID, description string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	integrations, err := s.integrations.ListByBusinessID(ctx, businessID)
	if err != nil {
		slog.Error("platform sync: failed to list integrations", "business_id", businessID, "error", err)
		return
	}

	for _, integ := range integrations {
		if integ.Status != "active" {
			continue
		}
		switch integ.Platform {
		case "telegram":
			s.syncTelegram(ctx, businessID, integ.ExternalID, description)
		// VK: external_id is "default" (no real group_id stored at connect time), skip for now
		}
	}
}

func (s *Syncer) syncTelegram(ctx context.Context, businessID uuid.UUID, channelID, description string) {
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
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
		OK          bool   `json:"ok"`
		Description string `json:"description"`
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
