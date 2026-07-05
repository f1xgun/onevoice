package platform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// maxTelegramDescription is Telegram's setChatDescription character limit.
const maxTelegramDescription = 255

// DescriptionTemplateSettingsKey is the businesses.settings JSONB sub-key
// holding a per-business description template. When absent or blank the
// platform-default formatter is used instead.
const DescriptionTemplateSettingsKey = "descriptionTemplate"

// VoiceProfileSettingsKey is the businesses.settings JSONB sub-key holding a
// per-business free-form brand-voice profile (do/don't phrases, emoji policy,
// short exemplars). It is a sibling of voiceTone, not a replacement: voiceTone
// is the tag picker, voiceProfile is authored prose. When absent or blank the
// chat and review-drafter prompts render exactly as they did before.
const VoiceProfileSettingsKey = "voiceProfile"

// ReviewAutopilotSettingsKey is the businesses.settings JSONB sub-key holding
// the per-business review-reply autopilot configuration. Absent key means the
// autopilot is disabled (default-off): a positive reply is never auto-published
// and every review stays pending for HITL. It is a sibling of voiceProfile and
// descriptionTemplate, written via UpdateSettingsKeys (jsonb_set, no migration).
const ReviewAutopilotSettingsKey = "reviewAutopilot"

// ReviewAutopilotMinRatingFloor is the lowest rating an autopilot may ever
// auto-publish. It equals ReviewNeedsReviewMaxRating+1 so the positive floor
// can be RAISED by a stricter minRating (e.g. 5-star-only) but never lowered
// below a positive review.
const ReviewAutopilotMinRatingFloor = domain.ReviewNeedsReviewMaxRating + 1

// ReviewAutopilotConfig is the stored per-business autopilot setting. Enabled is
// the single opt-in control; MinRating raises the positive floor (clamped to at
// least ReviewAutopilotMinRatingFloor at read time).
type ReviewAutopilotConfig struct {
	Enabled   bool `json:"enabled"`
	MinRating int  `json:"minRating"`
}

// ReviewAutopilotFromSettings reads the reviewAutopilot sub-key from the
// settings blob. A missing, blank, or malformed key yields the zero value
// (Enabled=false, MinRating=0), i.e. the default-off state. When the stored
// minRating is below the positive floor it is raised to
// ReviewAutopilotMinRatingFloor so a misconfigured setting can never auto-publish
// a negative or neutral review. Mirrors the marshal-roundtrip idiom
// formatScheduleCompact uses to read Settings["schedule"].
func ReviewAutopilotFromSettings(settings map[string]interface{}) ReviewAutopilotConfig {
	if settings == nil {
		return ReviewAutopilotConfig{}
	}
	raw, ok := settings[ReviewAutopilotSettingsKey]
	if !ok || raw == nil {
		return ReviewAutopilotConfig{}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return ReviewAutopilotConfig{}
	}
	var cfg ReviewAutopilotConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ReviewAutopilotConfig{}
	}
	if cfg.MinRating < ReviewAutopilotMinRatingFloor {
		cfg.MinRating = ReviewAutopilotMinRatingFloor
	}
	return cfg
}

// AllowedDescriptionPlaceholders lists the placeholder tokens a description
// template may contain, in canonical order. It is the single source of truth
// shared by the write-path validator and the render substitution, so the two
// can never drift.
var AllowedDescriptionPlaceholders = []string{
	"name", "category", "address", "phone", "website", "hours", "description",
}

// descriptionPlaceholderRE matches single-brace placeholder tokens like {name}.
// It captures any brace group with no nested braces so the write-path validator
// can reject tokens outside AllowedDescriptionPlaceholders.
var descriptionPlaceholderRE = regexp.MustCompile(`\{[^{}]*\}`)

// renderBusinessDescription composes the platform description for b, truncated
// to maxRunes. When the business has a non-empty descriptionTemplate override
// it is a full replacement: placeholders are substituted and nothing is
// auto-appended. When absent it falls back to the platform default formatter,
// byte-for-byte identical to the pre-template behavior for every business that
// never set an override.
//
//nolint:unparam // maxRunes is the platform cap; only Telegram (255) calls this today, but the helper is deliberately platform-agnostic so VK can pass its own cap later.
func renderBusinessDescription(b *domain.Business, maxRunes int) string {
	tmpl := descriptionTemplateFromSettings(b.Settings)
	if tmpl == "" {
		return formatTelegramDescription(b)
	}
	return truncateRunes(substitutePlaceholders(tmpl, b), maxRunes)
}

// descriptionTemplateFromSettings reads the descriptionTemplate override from
// the settings blob, returning "" when the key is absent, blank, or not a
// string.
func descriptionTemplateFromSettings(settings map[string]interface{}) string {
	if settings == nil {
		return ""
	}
	raw, ok := settings[DescriptionTemplateSettingsKey]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return s
}

// VoiceProfileFromSettings reads the voiceProfile override from the settings
// blob, returning "" when the key is absent, blank, or not a string. Shared by
// the read handler, the chat-turn prompt builder, and the review drafter so the
// three surfaces resolve the profile identically.
func VoiceProfileFromSettings(settings map[string]interface{}) string {
	if settings == nil {
		return ""
	}
	raw, ok := settings[VoiceProfileSettingsKey]
	if !ok {
		return ""
	}
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return s
}

// substitutePlaceholders replaces every allowed {placeholder} in tmpl with the
// business field it maps to. A field with no value (including a nil website)
// renders as an empty string. Tokens outside AllowedDescriptionPlaceholders are
// left untouched; the write path rejects them so a stored template never
// contains one. Values are inserted as plain text — the platform description
// APIs send no parse_mode, so no markup can be injected.
func substitutePlaceholders(tmpl string, b *domain.Business) string {
	values := descriptionPlaceholderValues(b)
	pairs := make([]string, 0, len(AllowedDescriptionPlaceholders)*2)
	for _, name := range AllowedDescriptionPlaceholders {
		pairs = append(pairs, "{"+name+"}", values[name])
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}

// descriptionPlaceholderValues resolves each allowed placeholder name to its
// business field value. {hours} reuses the default schedule string without the
// leading ⏰ so the template author controls all surrounding formatting.
func descriptionPlaceholderValues(b *domain.Business) map[string]string {
	website := ""
	if b.Website != nil {
		website = *b.Website
	}
	return map[string]string{
		"name":        b.Name,
		"category":    b.Category,
		"address":     b.Address,
		"phone":       b.Phone,
		"website":     website,
		"hours":       formatScheduleCompact(b.Settings),
		"description": b.Description,
	}
}

// UnknownDescriptionPlaceholders returns the brace tokens in tmpl that are not
// in AllowedDescriptionPlaceholders, de-duplicated in first-appearance order.
// An empty result means every placeholder is renderable.
func UnknownDescriptionPlaceholders(tmpl string) []string {
	allowed := make(map[string]struct{}, len(AllowedDescriptionPlaceholders))
	for _, p := range AllowedDescriptionPlaceholders {
		allowed["{"+p+"}"] = struct{}{}
	}
	var unknown []string
	seen := make(map[string]struct{})
	for _, tok := range descriptionPlaceholderRE.FindAllString(tmpl, -1) {
		if _, ok := allowed[tok]; ok {
			continue
		}
		if _, dup := seen[tok]; dup {
			continue
		}
		seen[tok] = struct{}{}
		unknown = append(unknown, tok)
	}
	return unknown
}

// truncateRunes clamps s to at most maxRunes runes, replacing the trailing rune
// with an ellipsis when it overflows (matching the platform description cap).
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes-1]) + "…"
	}
	return s
}

// formatTelegramDescription builds a compact Telegram channel description
// from all business fields. Telegram's description limit is 255 characters.
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

	return truncateRunes(strings.Join(parts, "\n\n"), maxTelegramDescription)
}

// dayOrder is the canonical Mon→Sun ordering used to group consecutive days
// when rendering schedules.
var dayOrder = []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}

// dayRU maps the frontend's 3-letter day key to its Russian abbreviation
// (used in the Telegram description block).
var dayRU = map[string]string{
	"mon": "Пн", "tue": "Вт", "wed": "Ср", "thu": "Чт",
	"fri": "Пт", "sat": "Сб", "sun": "Вс",
}

// formatSchedule renders the schedule for the default Telegram description,
// prefixed with the ⏰ marker. Empty schedule → empty string.
func formatSchedule(settings map[string]interface{}) string {
	compact := formatScheduleCompact(settings)
	if compact == "" {
		return ""
	}
	return "⏰ " + compact
}

// formatScheduleCompact converts the schedule stored in Settings["schedule"]
// into a compact string without the ⏰ marker. Groups consecutive days with
// identical hours: "Пн-Пт 09:00-21:00, Сб 10:00-18:00".
func formatScheduleCompact(settings map[string]interface{}) string {
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

	type slot struct{ open, close string }
	byDay := make(map[string]slot)
	for _, d := range days {
		if !d.Closed {
			byDay[d.Day] = slot{d.Open, d.Close}
		}
	}

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
	return strings.Join(segments, ", ")
}

// dayKeyToEnglish maps the frontend's 3-letter day key to the full English
// name expected by the Yandex.Business agent's formatHoursForYandex parser
// (see services/agent-yandex-business/internal/yandex/pool.go:919).
var dayKeyToEnglish = map[string]string{
	"mon": "monday", "tue": "tuesday", "wed": "wednesday", "thu": "thursday",
	"fri": "friday", "sat": "saturday", "sun": "sunday",
}

// scheduleToYandexJSON converts business.Settings["schedule"] into the JSON
// shape expected by the Yandex.Business RPA agent's formatHoursForYandex.
// Open days become {"open":"HH:MM","close":"HH:MM"}; closed days become
// "closed". Returns the marshaled JSON string, or "" if the schedule is
// missing/empty.
func scheduleToYandexJSON(settings map[string]interface{}) string {
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

	out := make(map[string]interface{}, len(days))
	for _, d := range days {
		enKey, ok := dayKeyToEnglish[d.Day]
		if !ok {
			continue
		}
		if d.Closed {
			out[enKey] = "closed"
			continue
		}
		if d.Open == "" || d.Close == "" {
			continue
		}
		out[enKey] = map[string]string{"open": d.Open, "close": d.Close}
	}

	if len(out) == 0 {
		return ""
	}

	encoded, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	return string(encoded)
}
