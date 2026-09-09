package platform

const DriftAlertSettingsKey = "driftAlerts"

type DriftAlertSettings struct {
	Enabled bool   `json:"enabled"`
	Locale  string `json:"locale"`
}

// DriftAlertFromSettings is default-off. Invalid or absent locales use Russian,
// matching the product's primary locale without enabling delivery.
func DriftAlertFromSettings(settings map[string]interface{}) DriftAlertSettings {
	out := DriftAlertSettings{Locale: "ru"}
	if settings == nil {
		return out
	}
	raw, ok := settings[DriftAlertSettingsKey].(map[string]interface{})
	if !ok {
		return out
	}
	out.Enabled, _ = raw["enabled"].(bool)
	if locale, _ := raw["locale"].(string); locale == "ru" || locale == "en" {
		out.Locale = locale
	}
	return out
}
