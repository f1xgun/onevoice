package service

import (
	"regexp"
	"sort"
	"strings"

	"github.com/f1xgun/onevoice/services/api/internal/platform"
)

// computeDrift compares the STORED snapshot (built by platform.SyncedSnapshot
// through the writer's own formatters) against the REMOTE snapshot read back
// from the platform, and returns the sorted set of synced fields that differ.
//
// Only fields present in BOTH maps are compared: stored carries exactly the
// fields OneVoice writes to this platform, and a field the remote fetch did not
// return is treated as "unknown", never drift. Each field is normalized through
// the rule that matches how the writer serializes it, so a value we just pushed
// never reads back as drift (the core false-positive guard).
func computeDrift(_ string, stored, remote map[string]string) []string {
	var drift []string
	for field, storedVal := range stored {
		remoteVal, ok := remote[field]
		if !ok {
			continue
		}
		if fieldDrifted(field, storedVal, remoteVal) {
			drift = append(drift, field)
		}
	}
	sort.Strings(drift)
	return drift
}

// fieldDrifted reports whether one synced field differs after per-field
// normalization.
func fieldDrifted(field, stored, remote string) bool {
	switch field {
	case platform.FieldWebsite:
		return normalizeWebsite(stored) != normalizeWebsite(remote)
	case platform.FieldSchedule:
		return scheduleDrift(stored, remote)
	default:
		return normalizeText(stored) != normalizeText(remote)
	}
}

// normalizeText collapses all whitespace runs (incl. newlines) to single
// spaces, trims, and case-folds. Case + surrounding/interstitial whitespace are
// deliberately ignored so cosmetic differences never surface as drift.
func normalizeText(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// URL scheme prefixes stripped during website normalization (named consts so
// the URL-literal linter is satisfied — these are prefixes, not endpoints).
const (
	schemePrefixHTTPS = "https://"
	schemePrefixHTTP  = "http://"
)

// normalizeWebsite strips the scheme and a trailing slash and case-folds so
// "https://example.com/" and "example.com" compare equal — VK echoes the
// canonicalized form back from groups.getById.
func normalizeWebsite(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, schemePrefixHTTPS)
	s = strings.TrimPrefix(s, schemePrefixHTTP)
	s = strings.TrimSuffix(s, "/")
	return s
}

// timeToken matches an HH:MM clock time in either a schedule JSON or a rendered
// hours string.
var timeToken = regexp.MustCompile(`\b(\d{1,2}):(\d{2})\b`)

// extractTimes returns the set of HH:MM tokens (hour zero-padded) found in s.
func extractTimes(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range timeToken.FindAllStringSubmatch(s, -1) {
		h := m[1]
		if len(h) == 1 {
			h = "0" + h
		}
		out[h+":"+m[2]] = true
	}
	return out
}

// scheduleDrift compares the stored Yandex schedule JSON (scheduleToYandexJSON
// output) against the RENDERED hours string the RPA agent reads back. The two
// formats are structurally different (English-day JSON vs a localized rendered
// string), so a literal compare would always false-positive. Instead it compares
// the set of HH:MM boundary times and flags drift only when both sides carry
// times AND share no boundary at all — a clear contradiction (e.g. stored
// 09:00–21:00 vs remote 10:00–18:00), tolerant of day-grouping and
// localization differences.
//
// It returns false (no drift) whenever the stored schedule is empty (OneVoice
// pushed nothing, so there is nothing to drift from) or the remote hours are
// empty/unreadable (unknown, never drift). This is the flagged hours-format
// tolerance trade-off: it favors zero false positives over catching partial
// hour changes.
func scheduleDrift(storedJSON, remoteRendered string) bool {
	storedTimes := extractTimes(storedJSON)
	if len(storedTimes) == 0 {
		return false
	}
	remoteTimes := extractTimes(remoteRendered)
	if len(remoteTimes) == 0 {
		return false
	}
	for t := range storedTimes {
		if remoteTimes[t] {
			return false
		}
	}
	return true
}
