package platform

import (
	"encoding/json"
	"fmt"
	"time"
)

// ScheduleFromYandexSnapshot converts the writer's weekly shape back to local hours.
// An incomplete or malformed snapshot cannot replace the owner's schedule.
func ScheduleFromYandexSnapshot(raw string) ([]byte, error) {
	var source map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &source); err != nil {
		return nil, fmt.Errorf("decode remote hours: %w", err)
	}
	type day struct {
		Day    string `json:"day"`
		Open   string `json:"open,omitempty"`
		Close  string `json:"close,omitempty"`
		Closed bool   `json:"closed"`
	}
	days := []string{"mon", "tue", "wed", "thu", "fri", "sat", "sun"}
	result := make([]day, 0, len(days))
	for _, key := range days {
		value, ok := source[dayKeyToEnglish[key]]
		if !ok {
			return nil, fmt.Errorf("incomplete remote hours")
		}
		var closed string
		if json.Unmarshal(value, &closed) == nil && closed == "closed" {
			result = append(result, day{Day: key, Closed: true})
			continue
		}
		var hours struct{ Open, Close string }
		if err := json.Unmarshal(value, &hours); err != nil {
			return nil, fmt.Errorf("decode remote day: %w", err)
		}
		open, err := time.Parse("15:04", hours.Open)
		if err != nil {
			return nil, fmt.Errorf("invalid opening time: %w", err)
		}
		closeTime, err := time.Parse("15:04", hours.Close)
		if err != nil {
			return nil, fmt.Errorf("invalid closing time: %w", err)
		}
		result = append(result, day{Day: key, Open: open.Format("15:04"), Close: closeTime.Format("15:04")})
	}
	return json.Marshal(result)
}
