package domain

import "testing"

func TestValidToolFloor(t *testing.T) {
	cases := []struct {
		name string
		in   ToolFloor
		want bool
	}{
		{name: "auto is valid", in: ToolFloorAuto, want: true},
		{name: "manual is valid", in: ToolFloorManual, want: true},
		{name: "forbidden is valid", in: ToolFloorForbidden, want: true},
		{name: "empty string is invalid", in: ToolFloor(""), want: false},
		{name: "garbage value is invalid", in: ToolFloor("banana"), want: false},
		{name: "mixed-case rejected (enum is lowercase)", in: ToolFloor("Auto"), want: false},
		{name: "arbitrary string rejected", in: ToolFloor("inherit"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidToolFloor(tc.in)
			if got != tc.want {
				t.Fatalf("ValidToolFloor(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestToolFloorRank(t *testing.T) {
	auto := ToolFloorRank(ToolFloorAuto)
	manual := ToolFloorRank(ToolFloorManual)
	forbidden := ToolFloorRank(ToolFloorForbidden)
	invalid := ToolFloorRank(ToolFloor("banana"))

	if auto != 0 {
		t.Fatalf("ToolFloorRank(auto) = %d, want 0", auto)
	}
	if manual != 1 {
		t.Fatalf("ToolFloorRank(manual) = %d, want 1", manual)
	}
	if forbidden != 2 {
		t.Fatalf("ToolFloorRank(forbidden) = %d, want 2", forbidden)
	}
	if !(forbidden > manual && manual > auto) {
		t.Fatalf("rank order broken: forbidden=%d manual=%d auto=%d", forbidden, manual, auto)
	}
	if invalid >= auto {
		t.Fatalf("invalid rank (%d) must be below auto (%d) so unknown values never dominate a valid floor", invalid, auto)
	}
}
