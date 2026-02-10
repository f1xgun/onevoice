package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID `json:"id" db:"id"`
	Email        string    `json:"email" db:"email"`
	PasswordHash string    `json:"-" db:"password_hash"`
	Role         Role      `json:"role" db:"role"`
	CreatedAt    time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt    time.Time `json:"updatedAt" db:"updated_at"`
}

type Business struct {
	ID          uuid.UUID              `json:"id" db:"id"`
	UserID      uuid.UUID              `json:"userId" db:"user_id"`
	Name        string                 `json:"name" db:"name"`
	Category    string                 `json:"category" db:"category"`
	Address     string                 `json:"address" db:"address"`
	Phone       string                 `json:"phone" db:"phone"`
	Description string                 `json:"description" db:"description"`
	LogoURL     string                 `json:"logoUrl" db:"logo_url"`
	Settings    map[string]interface{} `json:"settings" db:"settings"`
	CreatedAt   time.Time              `json:"createdAt" db:"created_at"`
	UpdatedAt   time.Time              `json:"updatedAt" db:"updated_at"`
}

type BusinessSchedule struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	BusinessID  uuid.UUID  `json:"businessId" db:"business_id"`
	DayOfWeek   int        `json:"dayOfWeek" db:"day_of_week"`
	OpenTime    string     `json:"openTime" db:"open_time"`
	CloseTime   string     `json:"closeTime" db:"close_time"`
	IsClosed    bool       `json:"isClosed" db:"is_closed"`
	SpecialDate *time.Time `json:"specialDate,omitempty" db:"special_date"`
}

type Integration struct {
	ID                    uuid.UUID              `json:"id" db:"id"`
	BusinessID            uuid.UUID              `json:"businessId" db:"business_id"`
	Platform              string                 `json:"platform" db:"platform"`
	Status                string                 `json:"status" db:"status"`
	EncryptedAccessToken  []byte                 `json:"-" db:"encrypted_access_token"`
	EncryptedRefreshToken []byte                 `json:"-" db:"encrypted_refresh_token"`
	ExternalID            string                 `json:"externalId" db:"external_id"`
	Metadata              map[string]interface{} `json:"metadata" db:"metadata"`
	TokenExpiresAt        *time.Time             `json:"tokenExpiresAt,omitempty" db:"token_expires_at"`
	CreatedAt             time.Time              `json:"createdAt" db:"created_at"`
	UpdatedAt             time.Time              `json:"updatedAt" db:"updated_at"`
}

type Subscription struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	Plan      string    `json:"plan" db:"plan"`
	Status    string    `json:"status" db:"status"`
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
	UpdatedAt time.Time `json:"updatedAt" db:"updated_at"`
}

type AuditLog struct {
	ID        uuid.UUID              `json:"id" db:"id"`
	UserID    uuid.UUID              `json:"userId" db:"user_id"`
	Action    string                 `json:"action" db:"action"`
	Resource  string                 `json:"resource" db:"resource"`
	Details   map[string]interface{} `json:"details" db:"details"`
	CreatedAt time.Time              `json:"createdAt" db:"created_at"`
}

type RefreshToken struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    uuid.UUID `json:"userId" db:"user_id"`
	TokenHash string    `json:"-" db:"token_hash"`
	ExpiresAt time.Time `json:"expiresAt" db:"expires_at"`
	CreatedAt time.Time `json:"createdAt" db:"created_at"`
}
