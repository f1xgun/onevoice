package domain

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSpecialDateHoursValidation(t *testing.T) {
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{"date-only closed", `[{"date":"2026-11-04","closed":true}]`, true},
		{"hours", `[{"date":"2026-11-04","open":"09:00","close":"18:00"}]`, true},
		{"invalid calendar date", `[{"date":"2026-02-30","closed":true}]`, false},
		{"timestamp", `[{"date":"2026-11-04T00:00:00Z","closed":true}]`, false},
		{"duplicate date", `[{"date":"2026-11-04","closed":true},{"date":"2026-11-04","closed":true}]`, false},
		{"missing closing time", `[{"date":"2026-11-04","open":"09:00"}]`, false},
		{"invalid clock", `[{"date":"2026-11-04","open":"25:00","close":"26:00"}]`, false},
		{"reversed interval", `[{"date":"2026-11-04","open":"18:00","close":"09:00"}]`, false},
		{"empty removes owned dates", `[]`, true},
		{"null is not removal", `null`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSpecialDateHours([]byte(tt.raw))
			if tt.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestSpecialDateChangeRequiresExplicitLists(t *testing.T) {
	for _, raw := range []string{`{}`, `{"previous":[]}`, `{"current":[]}`, `{"previous":[],"current":null}`, `{"previous":[{"date":"2026-02-30","closed":true}],"current":[]}`} {
		_, err := ParseSpecialDateHoursChange([]byte(raw))
		require.Error(t, err)
	}
	_, err := ParseSpecialDateHoursChange([]byte(`{"previous":[],"current":[]}`))
	require.NoError(t, err)
	dates := make([]SpecialDateHours, 101)
	raw, err := json.Marshal(dates)
	require.NoError(t, err)
	_, err = ParseSpecialDateHours(raw)
	require.Error(t, err)
}
