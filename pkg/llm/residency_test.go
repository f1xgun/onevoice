package llm

import (
	"strings"
	"testing"
)

func TestEnforceResidency(t *testing.T) {
	ru := []SelfHostedEndpoint{{URL: "https://llm.example.internal/v1", Model: "gpt://folder/deepseek-v4-flash/latest"}}

	tests := []struct {
		name             string
		production       bool
		allowTransborder bool
		hosted           []string
		models           []string
		endpoints        []SelfHostedEndpoint
		wantErr          string
	}{
		{
			name:      "non-production is a no-op even with hosted keys",
			hosted:    []string{"OPENAI_API_KEY"},
			models:    []string{"openai/gpt-5-mini"},
			endpoints: nil,
		},
		{
			name:             "production with explicit transborder allowance passes",
			production:       true,
			allowTransborder: true,
			hosted:           []string{"ANTHROPIC_API_KEY"},
			models:           []string{"anthropic/claude-sonnet-4-6"},
		},
		{
			name:       "production refuses hosted provider keys",
			production: true,
			hosted:     []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY"},
			models:     []string{"gpt://folder/deepseek-v4-flash/latest"},
			endpoints:  ru,
			wantErr:    "OPENROUTER_API_KEY, OPENAI_API_KEY",
		},
		{
			name:       "production with every model on a self-hosted endpoint passes",
			production: true,
			models:     []string{"gpt://folder/deepseek-v4-flash/latest", "", "gpt://folder/deepseek-v4-flash/latest"},
			endpoints:  ru,
		},
		{
			name:       "production refuses a model no endpoint serves",
			production: true,
			models:     []string{"gpt://folder/deepseek-v4-flash/latest", "anthropic/claude-haiku-4-5"},
			endpoints:  ru,
			wantErr:    `"anthropic/claude-haiku-4-5"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := EnforceResidency(tc.production, tc.allowTransborder, tc.hosted, tc.models, tc.endpoints)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestHostedKeysSet(t *testing.T) {
	if got := HostedKeysSet("", "", ""); len(got) != 0 {
		t.Fatalf("expected empty set, got %v", got)
	}
	got := HostedKeysSet("k", "", "k")
	want := "OPENROUTER_API_KEY,ANTHROPIC_API_KEY"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}
