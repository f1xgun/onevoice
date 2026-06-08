package yandex

import "testing"

func TestFormatHoursForYandex(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			// The format that triggered "could not parse hours from" in prod:
			// each day maps to an array containing one range string.
			name: "array of range strings, all days equal",
			in:   `{"monday":["00:00-22:00"],"tuesday":["00:00-22:00"],"wednesday":["00:00-22:00"],"thursday":["00:00-22:00"],"friday":["00:00-22:00"],"saturday":["00:00-22:00"],"sunday":["00:00-22:00"]}`,
			want: "Пн-Вс 00:00-22:00",
		},
		{
			name: "array of range strings, split shift",
			in:   `{"monday":["09:00-13:00","14:00-18:00"]}`,
			want: "Пн 09:00-13:00, 14:00-18:00",
		},
		{
			name: "plain range strings per day with weekend variation",
			in:   `{"monday":"09:00-18:00","tuesday":"09:00-18:00","saturday":"10:00-15:00"}`,
			want: "Пн-Вт 09:00-18:00, Сб 10:00-15:00",
		},
		{
			name: "open/close objects",
			in:   `{"monday":{"open":"09:00","close":"22:00"}}`,
			want: "Пн 09:00-22:00",
		},
		{
			name: "array of open/close objects (unchanged behavior)",
			in:   `{"monday":[{"open":"09:00","close":"22:00"}]}`,
			want: "Пн 09:00-22:00",
		},
		{
			name: "closed string is skipped",
			in:   `{"monday":"09:00-18:00","sunday":"closed"}`,
			want: "Пн 09:00-18:00",
		},
		{
			name: "empty array yields no entry",
			in:   `{"monday":[]}`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatHoursForYandex(tt.in); got != tt.want {
				t.Errorf("formatHoursForYandex(%s) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
