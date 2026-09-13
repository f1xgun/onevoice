package wire

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/services/api/internal/config"
)

func TestHandlers_RegistrationConfigurationFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cfg       *config.Config
		repos     *Repos
		wantError string
	}{
		{name: "missing config", wantError: "registration mode"},
		{name: "unset mode", cfg: &config.Config{}, wantError: "registration mode"},
		{name: "unknown mode", cfg: &config.Config{RegistrationMode: "typo"}, wantError: "registration mode"},
		{name: "no repositories", cfg: &config.Config{RegistrationMode: config.RegistrationModeInviteOnly}, wantError: "landing repository"},
		{name: "missing access repository", cfg: &config.Config{RegistrationMode: config.RegistrationModeInviteOnly}, repos: &Repos{}, wantError: "landing repository"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handlers, err := Handlers(tc.cfg, nil, tc.repos, nil)
			require.ErrorContains(t, err, tc.wantError)
			require.Nil(t, handlers)
		})
	}
}
