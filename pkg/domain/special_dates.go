package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// SpecialDateHours replaces the weekly hours for one calendar date. Dates are
// ISO date-only strings; they must never be converted through a time zone.
type SpecialDateHours struct {
	Date   string `json:"date"`
	Closed bool   `json:"closed"`
	Open   string `json:"open,omitempty"`
	Close  string `json:"close,omitempty"`
}

// SpecialDateHoursChange carries the dates owned before an explicit save and
// their desired values. Providers may remove only Previous dates missing from
// Current, preserving dates created outside OneVoice.
type SpecialDateHoursChange struct {
	Previous []SpecialDateHours `json:"previous"`
	Current  []SpecialDateHours `json:"current"`
}

// ParseSpecialDateHours validates the bounded date overrides accepted by the
// profile editor and the platform tool. An empty array means explicit removal.
func ParseSpecialDateHours(raw []byte) ([]SpecialDateHours, error) {
	var dates []SpecialDateHours
	if err := json.Unmarshal(raw, &dates); err != nil {
		return nil, fmt.Errorf("decode special dates: %w", err)
	}
	if dates == nil || len(dates) > 100 {
		return nil, fmt.Errorf("special dates must be an array of at most 100 dates")
	}
	seen := make(map[string]bool, len(dates))
	for _, d := range dates {
		parsed, err := time.Parse("2006-01-02", d.Date)
		if err != nil || parsed.Format("2006-01-02") != d.Date {
			return nil, fmt.Errorf("invalid special date %q", d.Date)
		}
		if seen[d.Date] {
			return nil, fmt.Errorf("duplicate special date %q", d.Date)
		}
		seen[d.Date] = true
		if d.Closed {
			continue
		}
		open, err := time.Parse("15:04", d.Open)
		if err != nil || open.Format("15:04") != d.Open {
			return nil, fmt.Errorf("invalid opening time for %s", d.Date)
		}
		closeTime, err := time.Parse("15:04", d.Close)
		if err != nil || closeTime.Format("15:04") != d.Close || !closeTime.After(open) {
			return nil, fmt.Errorf("invalid closing time for %s", d.Date)
		}
	}
	return dates, nil
}

// ParseSpecialDateHoursChange requires both lists so a missing current value
// cannot accidentally be interpreted as permission to delete owned dates.
func ParseSpecialDateHoursChange(raw []byte) (SpecialDateHoursChange, error) {
	var fields struct {
		Previous json.RawMessage `json:"previous"`
		Current  json.RawMessage `json:"current"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return SpecialDateHoursChange{}, fmt.Errorf("decode special-date change: %w", err)
	}
	previous, err := ParseSpecialDateHours(fields.Previous)
	if err != nil {
		return SpecialDateHoursChange{}, fmt.Errorf("previous: %w", err)
	}
	current, err := ParseSpecialDateHours(fields.Current)
	if err != nil {
		return SpecialDateHoursChange{}, fmt.Errorf("current: %w", err)
	}
	return SpecialDateHoursChange{Previous: previous, Current: current}, nil
}
