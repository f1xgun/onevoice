package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

func TestCompareVerificationRequiresEveryFieldAndExactValue(t *testing.T) {
	tests := []struct {
		name             string
		expected, remote map[string]string
		status           string
		mismatch         []string
	}{
		{name: "exact", expected: map[string]string{"title": "Cafe", "description": "Line 1\nLine 2"}, remote: map[string]string{"title": "Cafe", "description": "Line 1\nLine 2"}, status: domain.VerificationVerified},
		{name: "missing is unverifiable", expected: map[string]string{"title": "Cafe", "phone": "123"}, remote: map[string]string{"title": "Cafe"}, status: domain.VerificationUnverifiable},
		{name: "case remains mismatch", expected: map[string]string{"title": "Cafe"}, remote: map[string]string{"title": "cafe"}, status: domain.VerificationMismatch, mismatch: []string{"title"}},
		{name: "url is exact", expected: map[string]string{"website": "https://example.test/"}, remote: map[string]string{"website": "example.test"}, status: domain.VerificationMismatch, mismatch: []string{"website"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, mismatch := CompareVerification(tt.expected, tt.remote)
			assert.Equal(t, tt.status, status)
			assert.Equal(t, tt.mismatch, mismatch)
		})
	}
}

func TestVerificationIntentKeepsUnreadableVKPhone(t *testing.T) {
	expected, target, status := verificationIntent("vk", "sync_info", map[string]interface{}{"group_id": "42", "title": "Cafe", "phone": "+7 900"})
	assert.Equal(t, domain.VerificationPending, status)
	assert.Equal(t, "42", target)
	assert.Equal(t, "+7 900", expected[platform.FieldPhone])
}
