package hitl

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestResolve(t *testing.T) {
	type args struct {
		floor   domain.ToolFloor
		biz     map[string]domain.ToolFloor
		proj    map[string]domain.ToolFloor
		toolKey string
	}
	cases := []struct {
		name string
		in   args
		want domain.ToolFloor
	}{
		{
			name: "auto floor with empty maps stays auto",
			in:   args{floor: domain.ToolFloorAuto, biz: nil, proj: nil, toolKey: "telegram__send_channel_post"},
			want: domain.ToolFloorAuto,
		},
		{
			name: "manual floor with empty maps stays manual",
			in:   args{floor: domain.ToolFloorManual, biz: nil, proj: nil, toolKey: "telegram__send_channel_post"},
			want: domain.ToolFloorManual,
		},
		{
			name: "forbidden floor cannot be lowered by anything",
			in: args{
				floor:   domain.ToolFloorForbidden,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorAuto},
				proj:    map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorAuto},
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorForbidden,
		},
		{
			name: "business cannot lower below floor (manual floor, biz=auto stays manual)",
			in: args{
				floor:   domain.ToolFloorManual,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorAuto},
				proj:    nil,
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "business raises auto to manual",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual},
				proj:    nil,
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "project raises above business (biz auto + proj manual)",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorAuto},
				proj:    map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual},
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "project cannot lower below business (biz manual + proj auto)",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual},
				proj:    map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorAuto},
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "project raises to forbidden over biz manual",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual},
				proj:    map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorForbidden},
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorForbidden,
		},
		{
			name: "tool missing from both maps keeps floor",
			in: args{
				floor:   domain.ToolFloorManual,
				biz:     map[string]domain.ToolFloor{"vk__send_post": domain.ToolFloorForbidden},
				proj:    map[string]domain.ToolFloor{"other__tool": domain.ToolFloorForbidden},
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "empty tool name with empty maps keeps floor (no panic)",
			in:   args{floor: domain.ToolFloorAuto, biz: nil, proj: nil, toolKey: ""},
			want: domain.ToolFloorAuto,
		},
		{
			name: "empty tool name matching an empty-key entry is resolved (strictness still dominates)",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{"": domain.ToolFloorManual},
				proj:    nil,
				toolKey: "",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "biz only — manual applied when key matches",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual},
				proj:    map[string]domain.ToolFloor{}, // empty non-nil
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "project only (biz empty) — auto raised to manual",
			in: args{
				floor:   domain.ToolFloorAuto,
				biz:     map[string]domain.ToolFloor{},
				proj:    map[string]domain.ToolFloor{"telegram__send_channel_post": domain.ToolFloorManual},
				toolKey: "telegram__send_channel_post",
			},
			want: domain.ToolFloorManual,
		},
		{
			name: "forbidden floor + no overrides stays forbidden",
			in:   args{floor: domain.ToolFloorForbidden, biz: nil, proj: nil, toolKey: "telegram__send_channel_post"},
			want: domain.ToolFloorForbidden,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolve(tc.in.floor, tc.in.biz, tc.in.proj, tc.in.toolKey)
			if got != tc.want {
				t.Fatalf("Resolve(floor=%q, biz=%v, proj=%v, tool=%q) = %q, want %q",
					tc.in.floor, tc.in.biz, tc.in.proj, tc.in.toolKey, got, tc.want)
			}
		})
	}
}
