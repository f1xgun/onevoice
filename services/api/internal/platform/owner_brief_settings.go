package platform

// OwnerBriefSettingsKey is the businesses.settings JSONB sub-key holding the
// per-business weekly-owner-brief preferences: whether the proactive brief DM is
// enabled, and the weekday/hour window it should fire in. It is DEFAULT-ON: when
// the key (or its enabled flag) is absent the brief is treated as enabled, so no
// migration/backfill is needed to opt every business in — opting out is the
// explicit write (enabled=false).
const OwnerBriefSettingsKey = "ownerBrief"

// OwnerBriefLastSentSettingsKey is the businesses.settings JSONB sub-key holding
// the ISO year-week ("<year>-W<week>") the last brief was successfully sent in.
// It is stamped only after a confirmed dispatch and read to skip a business that
// already received a brief this week, so a process restart mid-week never
// double-sends.
const OwnerBriefLastSentSettingsKey = "ownerBriefLastSent"

// DefaultOwnerBriefWeekday is the weekday the brief fires on when the business
// never picked one (0=Sunday … 6=Saturday). Monday keeps the brief a
// start-of-week ritual.
const DefaultOwnerBriefWeekday = 1

// DefaultOwnerBriefHour is the hour-of-day (server TZ, 0-23) the brief fires at
// when the business never picked one.
const DefaultOwnerBriefHour = 9

// OwnerBriefSettings is the typed view of the ownerBrief sub-key. Enabled
// defaults true (default-on); Weekday/Hour default to Monday 09:00 when unset or
// out of range.
type OwnerBriefSettings struct {
	Enabled bool
	Weekday int
	Hour    int
}

// OwnerBriefFromSettings reads the ownerBrief sub-key into a typed struct.
// Enabled is true unless the stored object explicitly sets "enabled": false, so
// a business with no ownerBrief key (or a key that omits enabled) is opted in.
// Weekday/Hour fall back to the Monday-09:00 defaults when absent or out of
// range. Shared by the read handler and the weekly worker so the two resolve the
// preference identically.
func OwnerBriefFromSettings(settings map[string]interface{}) OwnerBriefSettings {
	out := OwnerBriefSettings{
		Enabled: true,
		Weekday: DefaultOwnerBriefWeekday,
		Hour:    DefaultOwnerBriefHour,
	}
	if settings == nil {
		return out
	}
	raw, ok := settings[OwnerBriefSettingsKey].(map[string]interface{})
	if !ok {
		return out
	}
	if enabled, ok := raw["enabled"].(bool); ok {
		out.Enabled = enabled
	}
	if wd, ok := settingsInt(raw["weekday"]); ok && wd >= 0 && wd <= 6 {
		out.Weekday = wd
	}
	if hr, ok := settingsInt(raw["hour"]); ok && hr >= 0 && hr <= 23 {
		out.Hour = hr
	}
	return out
}

// OwnerBriefLastSentFromSettings reads the ownerBriefLastSent ISO-week stamp,
// returning "" when the key is absent, blank, or not a string (never sent).
func OwnerBriefLastSentFromSettings(settings map[string]interface{}) string {
	if settings == nil {
		return ""
	}
	s, _ := settings[OwnerBriefLastSentSettingsKey].(string)
	return s
}

// settingsInt coerces a JSONB-decoded numeric value (float64 from encoding/json,
// or a native int) into an int. Returns (0,false) for any non-numeric value.
func settingsInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	default:
		return 0, false
	}
}
