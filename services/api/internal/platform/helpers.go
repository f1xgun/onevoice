package platform

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/f1xgun/onevoice/pkg/domain"
)

// maxTelegramDescription is Telegram's setChatDescription character limit.
const maxTelegramDescription = 255

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

	result := strings.Join(parts, "\n\n")

	// Truncate to Telegram's limit.
	runes := []rune(result)
	if len(runes) > maxTelegramDescription {
		result = string(runes[:maxTelegramDescription-1]) + "…"
	}

	return result
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

// formatSchedule converts the schedule stored in Settings["schedule"] into a
// compact string. Groups consecutive days with identical hours:
// "Пн-Пт 09:00-21:00, Сб 10:00-18:00".
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

	// Index by day key.
	type slot struct{ open, close string }
	byDay := make(map[string]slot)
	for _, d := range days {
		if !d.Closed {
			byDay[d.Day] = slot{d.Open, d.Close}
		}
	}

	// Walk dayOrder and group consecutive days with identical hours.
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
