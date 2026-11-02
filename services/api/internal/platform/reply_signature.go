package platform

import "strings"

const ReplySignatureSettingsKey = "replySignature"

var legacySignaturePrefixes = []string{
	"Как подписывать ответы:", "How to sign replies:", "Подпись ответа:", "Reply signature:",
}

// SplitLegacyVoiceProfile separates the signature previously saved by first launch.
func SplitLegacyVoiceProfile(profile string) (body, signature string) {
	lines := strings.Split(profile, "\n")
	kept := make([]string, 0, len(lines))
	found := false
	for _, line := range lines {
		matched := false
		for _, prefix := range legacySignaturePrefixes {
			if value, ok := strings.CutPrefix(strings.TrimSpace(line), prefix); ok {
				signature, matched = strings.TrimSpace(value), true
				break
			}
		}
		if !matched {
			kept = append(kept, line)
		} else {
			found = true
		}
	}
	if !found {
		return profile, ""
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), signature
}

// ReplySignatureFromSettings prefers the separately saved value, including a deliberate clear.
func ReplySignatureFromSettings(settings map[string]interface{}) string {
	if value, ok := settings[ReplySignatureSettingsKey].(string); ok {
		return strings.TrimSpace(value)
	}
	_, signature := SplitLegacyVoiceProfile(VoiceProfileFromSettings(settings))
	return signature
}

// VoiceInstructionsFromSettings keeps legacy prompts intact and includes the new reply signature.
func VoiceInstructionsFromSettings(settings map[string]interface{}) string {
	profile := VoiceProfileFromSettings(settings)
	if _, stored := settings[ReplySignatureSettingsKey].(string); !stored {
		return profile
	}
	body, _ := SplitLegacyVoiceProfile(profile)
	if signature := ReplySignatureFromSettings(settings); signature != "" {
		return strings.TrimSpace(body + "\nКак подписывать ответы: " + signature)
	}
	return body
}
