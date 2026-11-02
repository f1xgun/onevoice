package platform

import (
	"strconv"
	"strings"
	"time"
)

// YandexReviewPublishedAtKey records a parsed publication date, never ingestion time.
const YandexReviewPublishedAtKey = "published_at"

const yandexReviewUTCOffset = 3 * time.Hour

var yandexReviewLocation = time.FixedZone("MSK", int(yandexReviewUTCOffset/time.Second))

// YandexReviewPublishedAt accepts the Russian calendar dates returned by the RPA
// reader and ISO timestamps. Unknown dates cannot establish auto-reply eligibility.
func YandexReviewPublishedAt(source map[string]interface{}, now time.Time) (time.Time, bool) {
	for _, key := range []string{"created_at", "date"} {
		value, _ := source[key].(string)
		value = strings.TrimSpace(value)
		if parsed, err := time.Parse(time.RFC3339, value); err == nil {
			return parsed, true
		}
		for _, layout := range []string{"2006-01-02", "02.01.2006"} {
			if parsed, err := time.ParseInLocation(layout, value, yandexReviewLocation); err == nil {
				return parsed, true
			}
		}
	}
	value, _ := source["date"].(string)
	value = strings.ToLower(strings.Join(strings.Fields(value), " "))
	today := now.In(yandexReviewLocation)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, yandexReviewLocation)
	if value == "сегодня" {
		return today, true
	}
	if value == "вчера" {
		return today.AddDate(0, 0, -1), true
	}
	parts := strings.Fields(value)
	if len(parts) != 2 && len(parts) != 3 {
		return time.Time{}, false
	}
	months := []string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"}
	month := time.Month(0)
	for index, name := range months {
		if parts[1] == name {
			month = time.Month(index + 1)
		}
	}
	day, err := strconv.Atoi(parts[0])
	if err != nil || month == 0 {
		return time.Time{}, false
	}
	year := today.Year()
	if len(parts) == 3 {
		year, err = strconv.Atoi(parts[2])
		if err != nil || year < 2000 || year > 9999 {
			return time.Time{}, false
		}
	}
	parsed := time.Date(year, month, day, 0, 0, 0, 0, yandexReviewLocation)
	if parsed.Day() != day || parsed.Month() != month {
		return time.Time{}, false
	}
	if len(parts) == 2 && parsed.After(today) {
		parsed = parsed.AddDate(-1, 0, 0)
	}
	return parsed, true
}
