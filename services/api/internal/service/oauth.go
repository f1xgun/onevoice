package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type OAuthStateData struct {
	UserID     uuid.UUID `json:"user_id"`
	BusinessID uuid.UUID `json:"business_id"`
	Platform   string    `json:"platform"`
}

type OAuthService struct {
	redis *redis.Client
}

func NewOAuthService(redisClient *redis.Client) *OAuthService {
	return &OAuthService{redis: redisClient}
}

const oauthStateTTL = 10 * time.Minute
const oauthStatePrefix = "oauth:state:"

func (s *OAuthService) GenerateState(ctx context.Context, data OAuthStateData) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	state := hex.EncodeToString(b)

	payload, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("marshal state data: %w", err)
	}

	if err := s.redis.Set(ctx, oauthStatePrefix+state, payload, oauthStateTTL).Err(); err != nil {
		return "", fmt.Errorf("store state: %w", err)
	}

	return state, nil
}

func (s *OAuthService) ValidateState(ctx context.Context, state string) (*OAuthStateData, error) {
	key := oauthStatePrefix + state
	payload, err := s.redis.GetDel(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, fmt.Errorf("invalid or expired oauth state")
		}
		return nil, fmt.Errorf("get state: %w", err)
	}

	var data OAuthStateData
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil, fmt.Errorf("unmarshal state data: %w", err)
	}

	return &data, nil
}
