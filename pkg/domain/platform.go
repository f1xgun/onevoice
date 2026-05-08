package domain

// PlatformStatus is the runtime status of an integration platform as exposed
// through the public /api/v1/platforms endpoint. The frontend uses it to decide
// whether to render a connect button, a "coming soon" placeholder, or a
// disabled card with a "not configured" hint.
type PlatformStatus string

const (
	// PlatformStatusActive — agent is implemented and OAuth credentials (or
	// equivalent: bot token, service key) are present in the API config.
	PlatformStatusActive PlatformStatus = "active"
	// PlatformStatusComingSoon — declared in the registry but held back from
	// MVP. May lack an agent (2gis, avito, whatsapp), or be implemented but
	// intentionally hidden until the product launch is approved
	// (google_business — agent exists, marketing/support not ready).
	PlatformStatusComingSoon PlatformStatus = "coming_soon"
	// PlatformStatusOAuthNotConfigured — agent exists but the deployment is
	// missing required credentials. The frontend hides connect buttons for
	// these to avoid leading the user into a broken flow.
	PlatformStatusOAuthNotConfigured PlatformStatus = "oauth_not_configured"
)

// Platform is the canonical descriptor for a third-party integration the
// product exposes. IDs match the a2a.AgentID constants for platforms that
// have an agent (snake_case, e.g. yandex_business, google_business);
// coming-soon-only entries follow the same casing so the frontend can join
// by id without normalisation.
type Platform struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Status      PlatformStatus `json:"status"`
}

// Platforms returns the canonical platform registry in display order. All
// "real" platforms are returned with PlatformStatusActive — callers that know
// which credentials are present (typically the API platforms handler) are
// expected to downgrade entries to PlatformStatusOAuthNotConfigured before
// surfacing the list to clients.
func Platforms() []Platform {
	return []Platform{
		{
			ID:          "telegram",
			Name:        "Telegram",
			Description: "Бот для канала и уведомлений",
			Status:      PlatformStatusActive,
		},
		{
			ID:          "vk",
			Name:        "ВКонтакте",
			Description: "Публикации и комментарии",
			Status:      PlatformStatusActive,
		},
		{
			ID:          "yandex_business",
			Name:        "Яндекс.Бизнес",
			Description: "Отзывы и информация",
			Status:      PlatformStatusActive,
		},
		{
			// Google Business agent exists but is held back from MVP per
			// product decision (Linen design v2 §5). Promote to Active once
			// the marketing+support pipeline for Google is ready.
			ID:          "google_business",
			Name:        "Google Business",
			Description: "Отзывы и информация о бизнесе",
			Status:      PlatformStatusComingSoon,
		},
		{
			ID:          "2gis",
			Name:        "2ГИС",
			Description: "Скоро",
			Status:      PlatformStatusComingSoon,
		},
		{
			ID:          "avito",
			Name:        "Авито",
			Description: "Скоро",
			Status:      PlatformStatusComingSoon,
		},
		{
			ID:          "whatsapp",
			Name:        "WhatsApp",
			Description: "Скоро",
			Status:      PlatformStatusComingSoon,
		},
	}
}
