package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
)

func TestPublicDriftValues(t *testing.T) {
	state := domain.SyncState{Platform: "vk", DriftFields: []string{"title", "token", "phone"}, LastRemoteSnapshot: map[string]string{"title": "Old", "token": "secret", "phone": "private"}}
	remote, stored := publicDriftValues(state, &domain.Business{Name: "New"})
	require.Equal(t, map[string]string{"title": "Old"}, remote)
	require.Equal(t, map[string]string{"title": "New"}, stored)
	remote, stored = publicDriftValues(state, nil)
	require.Empty(t, remote)
	require.Empty(t, stored)
}
