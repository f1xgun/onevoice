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

var publicationContact = regexp.MustCompile(`(?i)` + contactWordStart + `(?:запись\s+по\s+телефону|звоните(?:\s+по(?:\s+телефону)?)?|заказ(?:ы)?\s+по(?:\s+телефону)?|наш\s+телефон|телефон\s+для\s+(?:записи|заказов|публикации)|(?:укажи|добавь)\s+в\s+(?:пост|публикацию)\s+телефон|booking\s+phone|phone\s+for\s+(?:bookings|orders|publication))[\s:—-]*((?:\+7|8)[ \-(]*\d{3}[ \-)]*\d{3}[ \-]*\d{2}[ \-]*\d{2})` + contactWordEnd)
var publicationIntent = regexp.MustCompile(`(?i)(?:^|[.!?;:\n])\s*(?:(?:пожалуйста|please)[,\s]+)?(?:(?:сделай|напиши|подготовь|составь)\s+(?:пост|публикацию|ответ)|(?:добавь|укажи)\s+в\s+(?:пост|публикацию|ответ)|опубликуй|размести|ответь|(?:write|make|draft)\s+(?:a\s+)?(?:post|reply)|publish|post|reply)` + contactWordEnd)
var relayedContactContext = regexp.MustCompile(`(?i)` + contactWordStart + `(?:перескажи|перешли|процитируй|(?:посетитель|клиент|сотрудник)\s*:|цитата|цитату|(?:сообщение|отзыв)\s+(?:посетителя|клиента|сотрудника)|(?:посетитель|клиент|сотрудник|он|она)\s+(?:написал[аи]?|сообщил[аи]?|сказал[аи]?)|summarize|forward|quote|(?:visitor|customer)\s+(?:message|said|wrote))` + contactWordEnd)
var privateContactContext = regexp.MustCompile(`(?i)` + contactWordStart + `(?:личный|чужой|телефон\s+(?:клиента|посетителя|сотрудника)|private|personal)` + contactWordEnd)
var negatedContactContext = regexp.MustCompile(`(?i)` + contactWordStart + `(?:не|нельзя|do\s+not|don't)\s+(?:добав\p{L}*|указ\p{L}*|публик\p{L}*|опублик\p{L}*|размещ\p{L}*|размест\p{L}*|звон\p{L}*|запись|наш|телефон|сделай|напиши|подготовь|составь|ответь|publish|post|reply|write|add|include)` + contactWordEnd)
var quotedContactText = regexp.MustCompile("(?s)«[^»]*(?:»|$)|\"[^\"]*(?:\"|$)|`[^`]*(?:`|$)|“[^”]*(?:”|$)|<[^>]*(?:>|$)")

func publicationContactAllowlist(messages []llm.Message) []string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role != "user" {
			continue
		}
		text := quotedContactText.ReplaceAllString(messages[i].Content, " [quoted] ")
		var allow []string
		for _, match := range publicationContact.FindAllStringSubmatchIndex(text, -1) {
			prefix := text[:match[2]]
			if relayedContactContext.MatchString(prefix) {
				continue
			}
			intent := publicationIntent.FindStringIndex(prefix)
			if intent == nil {
				continue
			}
			clause := prefix[strings.LastIndexAny(prefix, ".!?;")+1:]
			if privateContactContext.MatchString(clause) || negatedContactContext.MatchString(clause) {
				continue
			}
			allow = append(allow, text[match[2]:match[3]])
		}
		return allow
	}
	return nil
}
