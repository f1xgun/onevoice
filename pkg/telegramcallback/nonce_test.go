package telegramcallback

import (
	"errors"
	"strings"
	"testing"
)

const (
	testSecret  = "super-secret-hmac-key-for-tests"
	testBatchID = "550e8400-e29b-41d4-a716-446655440000"
)

func TestBuildCallbackData_RoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		action string
	}{
		{"approve", ActionApprove},
		{"reject", ActionReject},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := BuildCallbackData(testBatchID, tc.action, testSecret)
			if err != nil {
				t.Fatalf("BuildCallbackData: unexpected error: %v", err)
			}
			if len(data) > 64 {
				t.Fatalf("callback_data %q is %d bytes, exceeds Telegram's 64-byte cap", data, len(data))
			}
			gotBatch, gotAction, err := ParseAndVerify(data, testSecret)
			if err != nil {
				t.Fatalf("ParseAndVerify: unexpected error: %v", err)
			}
			if gotBatch != testBatchID {
				t.Errorf("batchID = %q, want %q", gotBatch, testBatchID)
			}
			if gotAction != tc.action {
				t.Errorf("action = %q, want %q", gotAction, tc.action)
			}
		})
	}
}

func TestParseAndVerify_Rejections(t *testing.T) {
	validApprove, err := BuildCallbackData(testBatchID, ActionApprove, testSecret)
	if err != nil {
		t.Fatalf("setup BuildCallbackData: %v", err)
	}

	tamperedBatchID := mutateField(t, validApprove, 1, "550e8400-e29b-41d4-a716-446655440099")
	tamperedAction := mutateField(t, validApprove, 2, "r")
	truncatedMAC := mutateField(t, validApprove, 3, "00")

	tests := []struct {
		name    string
		data    string
		secret  string
		wantErr error
	}{
		{"tampered_batch_id", tamperedBatchID, testSecret, ErrBadNonce},
		{"tampered_action_byte", tamperedAction, testSecret, ErrBadNonce},
		{"tampered_mac", truncatedMAC, testSecret, ErrBadNonce},
		{"wrong_secret", validApprove, "a-different-secret", ErrBadNonce},
		{"garbage", "not-a-valid-callback", testSecret, ErrBadFormat},
		{"empty", "", testSecret, ErrBadFormat},
		{"wrong_version", "v2:" + testBatchID + ":a:deadbeef", testSecret, ErrBadFormat},
		{"too_few_fields", "v1:" + testBatchID + ":a", testSecret, ErrBadFormat},
		{"too_many_fields", validApprove + ":extra", testSecret, ErrBadFormat},
		{"unknown_action_byte", "v1:" + testBatchID + ":x:deadbeef", testSecret, ErrBadFormat},
		{"empty_batch_id", "v1::a:deadbeef", testSecret, ErrBadFormat},
		{"empty_secret", validApprove, "", ErrEmptySecret},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ParseAndVerify(tc.data, tc.secret)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("ParseAndVerify(%q) error = %v, want %v", tc.data, err, tc.wantErr)
			}
		})
	}
}

func TestBuildCallbackData_Rejections(t *testing.T) {
	tests := []struct {
		name    string
		batchID string
		action  string
		secret  string
		wantErr error
	}{
		{"empty_secret", testBatchID, ActionApprove, "", ErrEmptySecret},
		{"empty_batch", "", ActionApprove, testSecret, ErrBadFormat},
		{"unknown_action", testBatchID, "edit", testSecret, ErrBadFormat},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildCallbackData(tc.batchID, tc.action, tc.secret)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("BuildCallbackData error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestParseAndVerify_ActionBoundToMAC proves the MAC binds the action: a valid
// approve token cannot be turned into a valid reject by only flipping the wire
// action byte, because the MAC is computed over the canonical action string.
func TestParseAndVerify_ActionBoundToMAC(t *testing.T) {
	approve, err := BuildCallbackData(testBatchID, ActionApprove, testSecret)
	if err != nil {
		t.Fatalf("build approve: %v", err)
	}
	reject, err := BuildCallbackData(testBatchID, ActionReject, testSecret)
	if err != nil {
		t.Fatalf("build reject: %v", err)
	}
	approveParts := strings.Split(approve, ":")
	rejectParts := strings.Split(reject, ":")
	if approveParts[3] == rejectParts[3] {
		t.Fatalf("approve and reject MACs are identical (%q) — action is not bound", approveParts[3])
	}
	// Splice the reject action byte onto the approve MAC: must fail.
	spliced := strings.Join([]string{approveParts[0], approveParts[1], "r", approveParts[3]}, ":")
	if _, _, err := ParseAndVerify(spliced, testSecret); !errors.Is(err, ErrBadNonce) {
		t.Fatalf("spliced action token verified or wrong error: %v", err)
	}
}

// mutateField replaces the idx-th colon-separated field of a v1 callback string
// and returns the reassembled string.
func mutateField(t *testing.T, data string, idx int, replacement string) string {
	t.Helper()
	parts := strings.Split(data, ":")
	if idx < 0 || idx >= len(parts) {
		t.Fatalf("mutateField: idx %d out of range for %q", idx, data)
	}
	parts[idx] = replacement
	return strings.Join(parts, ":")
}
