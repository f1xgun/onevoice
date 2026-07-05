// Package telegramcallback builds and verifies the opaque callback_data carried
// by the Telegram inline "Approve"/"Reject" buttons on an owner HITL approval
// notification.
//
// The callback_data is NOT a bearer token: it is one of three independent
// factors the api-side consumer requires before it resolves a pending approval.
// The other two — (1) the tapper's Telegram user-id equals the batch business's
// verified owner id, and (2) the referenced batch is still pending for THAT
// business — are enforced server-side against durable state. This package owns
// factor (3): a stateless HMAC that ties (batchID, action) together so a forged
// or guessed callback_data fails verification before any state lookup. There is
// no stored nonce and no separate TTL — a nonce is only ever honored against a
// still-pending batch, whose own 24h TTL bounds the window.
//
// Wire format (<=64 bytes, Telegram's callback_data cap):
//
//	v1:<batchID>:<a|r>:<macHex>
//
// where batchID is the batch UUID, the action byte is 'a' (approve) or 'r'
// (reject), and macHex is the first macBytes of HMAC-SHA256(secret,
// "<batchID>|<action>") in lowercase hex. A batch UUID is 36 bytes, so the whole
// string is 3+36+1+1+1+8 = 50 bytes — comfortably within the cap.
package telegramcallback

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
)

// ActionApprove and ActionReject are the two decision actions a callback can
// carry. They map to the batch-wide all-approve / all-reject verdict the
// consumer applies over the batch's calls.
const (
	ActionApprove = "approve"
	ActionReject  = "reject"
)

// version is the single supported callback_data schema tag. A future format
// change bumps this; ParseAndVerify rejects any other prefix so an old client
// (or a crafted string) cannot smuggle a differently-shaped payload past the
// parser.
const version = "v1"

// actionApproveByte and actionRejectByte are the compact on-wire action bytes.
// Keeping the wire byte to a single character conserves callback_data budget.
const (
	actionApproveByte = "a"
	actionRejectByte  = "r"
)

// fieldCount is the number of colon-separated fields in the v1 wire format:
// version, batchID, action byte, mac.
const fieldCount = 4

// macBytes is how many leading bytes of the HMAC-SHA256 digest are carried in
// callback_data. 4 bytes (8 hex chars) is a 32-bit tag: combined with the
// server-side owner-id binding and the still-pending-batch requirement (a forged
// data can only ever act on a real, still-pending batch owned by the tapper),
// it makes online forgery infeasible while keeping the payload inside the 64-byte
// cap. The MAC is never the sole authorization factor.
const macBytes = 4

// ErrBadFormat signals callback_data that does not parse as the v1 shape
// (wrong version, wrong field count, unknown action byte). It is deliberately
// indistinct from ErrBadNonce to callers logging the failure, but both map to a
// single "reject, no state change" outcome.
var ErrBadFormat = errors.New("telegramcallback: malformed callback_data")

// ErrBadNonce signals a well-formed callback_data whose MAC does not verify
// against the secret (forged or tampered). Verification is constant-time.
var ErrBadNonce = errors.New("telegramcallback: nonce verification failed")

// ErrEmptySecret signals an empty HMAC secret. Building or verifying with an
// empty secret is a wiring bug (the approval plane must be disabled fail-closed
// upstream when the secret is unset), so it is surfaced rather than producing a
// forgeable all-zero-keyed MAC.
var ErrEmptySecret = errors.New("telegramcallback: empty hmac secret")

// BuildCallbackData returns the opaque callback_data for an approval button.
// action must be ActionApprove or ActionReject; secret must be non-empty. The
// returned string is <=64 bytes for any UUID batchID. A caller MUST treat the
// result as opaque and never parse it except via ParseAndVerify.
func BuildCallbackData(batchID, action, secret string) (string, error) {
	if secret == "" {
		return "", ErrEmptySecret
	}
	actionByte, err := actionToByte(action)
	if err != nil {
		return "", err
	}
	if batchID == "" {
		return "", ErrBadFormat
	}
	mac := computeMAC(batchID, action, secret)
	return strings.Join([]string{version, batchID, actionByte, mac}, ":"), nil
}

// ParseAndVerify parses callback_data and verifies its MAC in constant time. On
// success it returns the batchID and the canonical action (ActionApprove /
// ActionReject). It returns ErrBadFormat for a malformed string and ErrBadNonce
// for a well-formed string whose MAC does not verify — in BOTH cases the caller
// must reject the callback with no state change. secret must be non-empty.
func ParseAndVerify(data, secret string) (batchID, action string, err error) {
	if secret == "" {
		return "", "", ErrEmptySecret
	}
	parts := strings.Split(data, ":")
	if len(parts) != fieldCount {
		return "", "", ErrBadFormat
	}
	ver, id, actionByte, gotMAC := parts[0], parts[1], parts[2], parts[3]
	if ver != version || id == "" {
		return "", "", ErrBadFormat
	}
	action, err = byteToAction(actionByte)
	if err != nil {
		return "", "", err
	}
	wantMAC := computeMAC(id, action, secret)
	if subtle.ConstantTimeCompare([]byte(gotMAC), []byte(wantMAC)) != 1 {
		return "", "", ErrBadNonce
	}
	return id, action, nil
}

// computeMAC returns the lowercase-hex leading macBytes of
// HMAC-SHA256(secret, "<batchID>|<action>"). action is the canonical
// ActionApprove/ActionReject value so an attacker cannot swap the wire action
// byte and keep a valid MAC.
func computeMAC(batchID, action, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(batchID))
	h.Write([]byte("|"))
	h.Write([]byte(action))
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:macBytes])
}

// actionToByte maps a canonical action to its compact wire byte, rejecting
// anything outside {approve, reject}.
func actionToByte(action string) (string, error) {
	switch action {
	case ActionApprove:
		return actionApproveByte, nil
	case ActionReject:
		return actionRejectByte, nil
	default:
		return "", ErrBadFormat
	}
}

// byteToAction maps a wire action byte back to its canonical action, rejecting
// any other byte.
func byteToAction(b string) (string, error) {
	switch b {
	case actionApproveByte:
		return ActionApprove, nil
	case actionRejectByte:
		return ActionReject, nil
	default:
		return "", ErrBadFormat
	}
}
