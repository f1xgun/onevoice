package orchestrator

import (
	"regexp"
	"strings"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/security"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
)

// applyOutboundRedaction scrubs third-party personal data from a ChatRequest
// before it leaves for a (possibly non-RU) LLM provider. It is a no-op unless
// RedactOutboundPDn is set — the operator opts out only with a legal basis for
// transborder personal-data transfer or when inference is routed exclusively to
// RU/self-hosted endpoints. See docs/orchestrator/config.md.
//
// allow carries registered organization contacts and owner-supplied contacts
// explicitly designated for a post or reply in the latest user instruction.
// Quoted or relayed third-party contacts do not qualify for that exemption.
func (o *Orchestrator) applyOutboundRedaction(req *llm.ChatRequest, allow []string) {
	if !o.options.RedactOutboundPDn {
		return
	}
	redactRequestPDn(req, allow)
}

// redactRequestPDn redacts personal data from req unconditionally, preserving
// the values in allow. It is split from applyOutboundRedaction so the transform
// is unit-testable without an Orchestrator.
//
// Messages shares its backing array with the live RunState, so a fresh slice is
// built (copy-on-redact) and the persisted pause snapshot keeps the original
// bytes for HITL display and audit. SystemBlocks is rebuilt per iteration by
// stepRun, so its elements are scrubbed in place — except slice index 0
// (Block 1, the locale-fixed platform prefix), which carries no business state
// and anchors the provider cache prefix.
func redactRequestPDn(req *llm.ChatRequest, allow []string) {
	if len(req.Messages) > 0 {
		scrubbed := make([]llm.Message, len(req.Messages))
		for i, m := range req.Messages {
			scrubbed[i] = m
			scrubbed[i].Content = security.RedactPIIExcept(m.Content, allow)
			if len(m.ToolCalls) > 0 {
				calls := make([]llm.ToolCall, len(m.ToolCalls))
				for j, tc := range m.ToolCalls {
					calls[j] = tc
					calls[j].Function.Arguments = security.RedactPIIExcept(tc.Function.Arguments, allow)
				}
				scrubbed[i].ToolCalls = calls
			}
		}
		req.Messages = scrubbed
	}

	for i := 1; i < len(req.SystemBlocks); i++ {
		req.SystemBlocks[i].Text = security.RedactPIIExcept(req.SystemBlocks[i].Text, allow)
	}
}

// businessContactAllowlist collects the business's own registered contact
// fields. They are entered by the owner, rendered on the business's public
// profile, and routinely load-bearing for the assistant (a post that has to
// carry the order line, a review reply that points a customer at the shop) — so
// redacting them would break the product without protecting anyone's personal
// data. Third-party contacts appearing anywhere else stay redacted.
func businessContactAllowlist(ctx prompt.BusinessContext) []string {
	out := make([]string, 0, 2)
	for _, v := range []string{ctx.Phone, ctx.Website} {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

const contactWordStart = `(?:^|[^\p{L}\p{N}_])`
const contactWordEnd = `(?:$|[^\p{L}\p{N}_])`

var publicationContact = regexp.MustCompile(`(?i)` + contactWordStart + `((?:\+7|8)[ \-(]*\d{3}[ \-)]*\d{3}[ \-]*\d{2}[ \-]*\d{2}|[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,})` + contactWordEnd)
var organizationContactLabel = regexp.MustCompile(`(?i)` + contactWordStart + `(?:запись\s+по\s+телефону|звоните(?:\s+по(?:\s+телефону)?)?|заказ(?:ы)?\s+по(?:\s+телефону)?|наш\s+телефон|пишите\s+на|booking\s+phone)[\s:—–-]*$`)
var publicationIntent = regexp.MustCompile(`(?i)(?:^|[.!?;:\n])\s*(?:(?:пожалуйста|please)[,\s]+)?(?:(?:сделай|напиши|подготовь|составь)\s+(?:пост|публикацию|ответ)|(?:добавь|укажи)\s+в\s+(?:пост|публикацию|ответ)|опубликуй|размести|ответь|(?:write|make|draft)\s+(?:a\s+)?(?:post|reply)|publish|post|reply)` + contactWordEnd)
var restrictedContactContext = regexp.MustCompile(`(?i)` + contactWordStart + `(?:клиент\p{L}*|посетител\p{L}*|гост\p{L}*|сотрудник\p{L}*|личн\p{L}*|чужой|перескажи|перешли|процитируй|скрой|пишет|написал\p{L}*|сообщение\s+от|не\s+(?:публикуй|опубликуй|указывай|добав\p{L}*|сделай|напиши|размещай|размести|ответь)|private|personal|customer|visitor|summarize|forward|quote|do\s+not|don't)` + contactWordEnd)
var quotedContactText = regexp.MustCompile("(?s)«[^»]*(?:»|$)|\"[^\"]*(?:\"|$)|`[^`]*(?:`|$)|“[^”]*(?:”|$)|<[^>]*(?:>|$)")
var publicationQuoteIntro = regexp.MustCompile(`(?i)` + contactWordStart + `(?:опубликуй|точный\s+текст|вот\s+текст)[\s:—–-]*$`)

func publicationContactAllowlist(messages []llm.Message) []string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := messages[i].Content
		if restrictedContactContext.MatchString(text) || !publicationIntent.MatchString(quotedContactText.ReplaceAllString(text, " [quoted] ")) {
			return nil
		}
		quotes := quotedContactText.FindAllStringIndex(text, -1)
		var allow []string
		for _, match := range publicationContact.FindAllStringSubmatchIndex(text, -1) {
			authorized := organizationContactLabel.MatchString(text[:match[2]])
			for _, quote := range quotes {
				if match[2] >= quote[0] && match[3] <= quote[1] {
					copy := text[quote[0]:quote[1]]
					authorized = ((strings.HasPrefix(copy, "«") && strings.HasSuffix(copy, "»")) || (strings.HasPrefix(copy, `"`) && strings.HasSuffix(copy, `"`) && len(copy) > 1)) && publicationQuoteIntro.MatchString(text[:quote[0]])
					break
				}
			}
			if authorized {
				allow = append(allow, text[match[2]:match[3]])
			}
		}
		return allow
	}
	return nil
}
