package domain

// Platform registry exposed via GET /api/v1/platforms. IDs are wire-stable and
// match a2a.AgentID for platforms that have an agent. See
// docs/domain/platforms.md for the full catalog and adding-a-platform steps.

// PlatformStatus is the runtime status of an integration platform.
type PlatformStatus string

const (
	// PlatformStatusActive — agent implemented and credentials present.
	PlatformStatusActive PlatformStatus = "active"
	// PlatformStatusComingSoon — declared in the registry but held back from MVP.
	PlatformStatusComingSoon PlatformStatus = "coming_soon"
	// PlatformStatusOAuthNotConfigured — agent exists but credentials are missing.
	PlatformStatusOAuthNotConfigured PlatformStatus = "oauth_not_configured"
)

// Platform is the canonical descriptor for a third-party integration.
//
// Name and Description are intentionally not serialized: the frontend
// resolves both from its i18n bundles (platforms.fullLabel.<id>,
// platforms.description.<id>). They remain on the struct as empty values so
// in-process callers keep compiling; `json:"-"` ensures the wire payload
// only ships id + status.
type Platform struct {
	ID          string         `json:"id"`
	Name        string         `json:"-"`
	Description string         `json:"-"`
	Status      PlatformStatus `json:"status"`
}

// Platforms returns the canonical platform registry in display order. All
// real platforms are returned as PlatformStatusActive; callers that know
// which credentials are present (typically the API platforms handler)
// downgrade entries to PlatformStatusOAuthNotConfigured before surfacing
// the list to clients.
func Platforms() []Platform {
	return []Platform{
		{ID: "telegram", Status: PlatformStatusActive},
		{ID: "vk", Status: PlatformStatusActive},
		{ID: "yandex_business", Status: PlatformStatusActive},
		{ID: "google_business", Status: PlatformStatusComingSoon},
		{ID: "2gis", Status: PlatformStatusComingSoon},
		{ID: "avito", Status: PlatformStatusComingSoon},
		{ID: "whatsapp", Status: PlatformStatusComingSoon},
	}
}
