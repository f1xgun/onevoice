package natsexec

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestViolatesDenyList(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantHit bool
		wantKey string
	}{
		{
			name:    "top-level denied key",
			args:    map[string]interface{}{"cookies": "x"},
			wantHit: true,
			wantKey: "cookies",
		},
		{
			name:    "case-insensitive match preserves original key",
			args:    map[string]interface{}{"COOKIES": "x"},
			wantHit: true,
			wantKey: "COOKIES",
		},
		{
			name:    "nested denied key one level deep",
			args:    map[string]interface{}{"user": map[string]interface{}{"session_id": "x"}},
			wantHit: true,
			wantKey: "session_id",
		},
		{
			name: "nested denied key two levels deep",
			args: map[string]interface{}{
				"user": map[string]interface{}{
					"profile": map[string]interface{}{"token": "x"},
				},
			},
			wantHit: true,
			wantKey: "token",
		},
		{
			name:    "prefix is not a substring match",
			args:    map[string]interface{}{"oauth_method": "x"},
			wantHit: false,
		},
		{
			name:    "substring is not a match",
			args:    map[string]interface{}{"webhook_authorization_header": "x"},
			wantHit: false,
		},
		{
			name:    "empty map",
			args:    map[string]interface{}{},
			wantHit: false,
		},
		{
			name:    "nil map",
			args:    nil,
			wantHit: false,
		},
		{
			name: "only safe fields including nested",
			args: map[string]interface{}{
				"safe_field": "value",
				"nested":     map[string]interface{}{"another_safe": 1},
			},
			wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hit, key := violatesDenyList(tt.args)
			assert.Equal(t, tt.wantHit, hit)
			if tt.wantHit {
				assert.Equal(t, tt.wantKey, key)
			}
		})
	}
}

func TestViolatesDenyList_EachDeniedKeyTriggers(t *testing.T) {
	keys := []string{
		"cookies",
		"session_id",
		"token",
		"auth",
		"authorization",
		"password",
		"api_key",
	}
	for _, k := range keys {
		t.Run(k, func(t *testing.T) {
			hit, key := violatesDenyList(map[string]interface{}{k: "x"})
			require.True(t, hit, "expected %q to be denied", k)
			assert.Equal(t, k, key)
		})
	}
}

func TestWalkDeny_NestedTakesPrecedenceOverSafeSiblings(t *testing.T) {
	hit, key := walkDeny(map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": map[string]interface{}{"password": "x"},
		},
	})
	require.True(t, hit)
	assert.Equal(t, "password", key)
}
