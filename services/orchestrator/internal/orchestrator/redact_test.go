package orchestrator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/security"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/orchestrator"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/prompt"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

// TestStepRun_RedactsOutboundPDn asserts that with RedactOutboundPDn enabled the
// request reaching the LLM has personal data scrubbed from both the history and
// the per-business prompt block, while the cached platform block is untouched.
func TestStepRun_RedactsOutboundPDn(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(capLLM, reg, orchestrator.Options{
		MaxIterations:     10,
		RedactOutboundPDn: true,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Acme", Phone: "+7 999 123-45-67"},
		Messages: []llm.Message{
			{Role: "user", Content: "клиент оставил телефон +78120000000 и почту a@b.com"},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req, "LLM must be called")

	require.NotEmpty(t, req.Messages)
	assert.Contains(t, req.Messages[0].Content, "[Скрыто]")
	assert.NotContains(t, req.Messages[0].Content, "+78120000000")
	assert.NotContains(t, req.Messages[0].Content, "a@b.com")

	require.Len(t, req.SystemBlocks, 2)
	assert.True(t, req.SystemBlocks[0].CacheBoundary)
	assert.NotContains(t, req.SystemBlocks[0].Text, "[Скрыто]",
		"the cached platform block carries no business state and must not change")
}

// TestStepRun_KeepsBusinessOwnContactUnderRedaction pins CHAT-ORCH-03: the
// business's own registered phone is its public contact data, not third-party
// personal data, so it must survive the outbound scrub in the business prompt
// block AND wherever the owner spells it out in the conversation (in any
// format) — while an unrelated third-party number in the same message is still
// redacted.
func TestStepRun_KeepsBusinessOwnContactUnderRedaction(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.NewWithOptions(capLLM, reg, orchestrator.Options{
		MaxIterations:     10,
		RedactOutboundPDn: true,
	})

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Кофейня «Утро»", Phone: "+7 (843) 555-12-34"},
		Messages: []llm.Message{
			{Role: "user", Content: "Наш телефон для заказов: 8 843 555-12-34. Клиент оставил свой +78120000000."},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req, "LLM must be called")

	require.NotEmpty(t, req.Messages)
	assert.Contains(t, req.Messages[0].Content, "8 843 555-12-34",
		"the owner's own business number must reach the model verbatim")
	assert.NotContains(t, req.Messages[0].Content, "+78120000000",
		"a third-party number in the same message is still third-party personal data")

	require.Len(t, req.SystemBlocks, 2)
	assert.Contains(t, req.SystemBlocks[1].Text, "+7 (843) 555-12-34",
		"the business block must still carry the business's own contact phone")
	assert.NotContains(t, req.SystemBlocks[1].Text, "Телефон: [Скрыто]")
}

// TestStepRun_PassesPDnWhenRedactionDisabled is the regression guard that the
// mechanism is a clean no-op when transborder transfer is explicitly allowed
// (the default constructor leaves RedactOutboundPDn false).
func TestStepRun_PassesPDnWhenRedactionDisabled(t *testing.T) {
	capLLM := &captureLLM{}
	reg := toolregistry.NewRegistry()
	orch := orchestrator.New(capLLM, reg)

	events, err := orch.Run(context.Background(), orchestrator.RunRequest{
		BusinessContext: prompt.BusinessContext{Name: "Acme", Phone: "+79991234567"},
		Messages: []llm.Message{
			{Role: "user", Content: "телефон +79991234567"},
		},
	})
	require.NoError(t, err)
	for range events {
	}

	req := capLLM.firstRequest()
	require.NotNil(t, req)
	assert.Contains(t, req.Messages[0].Content, "+79991234567")
	assert.NotContains(t, req.Messages[0].Content, "[Скрыто]")
	require.Len(t, req.SystemBlocks, 2)
	assert.Contains(t, req.SystemBlocks[1].Text, "+79991234567")
}

func TestResume_PDnAllowlistSurvivesPauseRoundTrip(t *testing.T) {
	for _, verdict := range []string{"approve", "edit", "reject"} {
		t.Run(verdict, func(t *testing.T) {
			ctx := context.Background()
			var dispatched atomic.Int32
			rec := &instrumentedExecutor{onDispatch: func() { dispatched.Add(1) }}
			reg := newRegistryWithFloor("manual_tool", domain.ToolFloorManual, rec)
			repo := newMockPendingRepo()
			options := orchestrator.Options{MaxIterations: 5, RedactOutboundPDn: true}
			stub := &stubLLM{responses: []*llm.ChatResponse{{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{{
					ID: "contact-call", Type: llm.ToolCallTypeFunction,
					Function: llm.FunctionCall{Name: "manual_tool", Arguments: "{}"},
				}},
			}}}
			orch := orchestrator.NewWithHITL(stub, reg, repo, options)
			events, err := orch.Run(ctx, orchestrator.RunRequest{
				BusinessContext: prompt.BusinessContext{Name: "Acme", Phone: "+7 (843) 555-12-34", Website: "ab.com"},
				Messages:        []llm.Message{{Role: "user", Content: "наш 8 843 555-12-34, клиент +78120000000, customer a@b.com"}},
				ConversationID:  "contact-conversation",
			})
			require.NoError(t, err)
			paused := drainEvents(events)
			require.Empty(t, findEvents(paused, orchestrator.EventError))
			require.Len(t, findEvents(paused, orchestrator.EventToolApprovalRequired), 1)
			require.Len(t, repo.insertedBatches, 1)
			batch := repo.insertedBatches[0]
			var snapshot struct {
				PDnAllowlist []string      `json:"pdn_allowlist"`
				Messages     []llm.Message `json:"messages"`
			}
			require.NoError(t, json.Unmarshal(batch.ModelMessages, &snapshot))
			assert.Equal(t, []string{"+7 (843) 555-12-34", "ab.com"}, snapshot.PDnAllowlist)
			require.NotEmpty(t, snapshot.Messages)
			assert.Contains(t, snapshot.Messages[0].Content, "+78120000000")
			batch.Calls[0].Verdict = verdict

			capLLM := &captureLLM{}
			resumed := orchestrator.NewWithHITL(capLLM, reg, repo, options)
			events, err = resumed.Resume(ctx, orchestrator.ResumeRequest{BatchID: batch.ID})
			require.NoError(t, err)
			completed := drainEvents(events)
			require.Empty(t, findEvents(completed, orchestrator.EventError))
			require.Len(t, findEvents(completed, orchestrator.EventDone), 1)
			if verdict == "reject" {
				assert.Zero(t, dispatched.Load())
				assert.Empty(t, findEvents(completed, orchestrator.EventToolCall))
				assert.Empty(t, findEvents(completed, orchestrator.EventToolResult))
				rejected := findEvents(completed, orchestrator.EventToolRejected)
				require.Len(t, rejected, 1)
				assert.Equal(t, "contact-call", rejected[0].ToolCallID)
				assert.Equal(t, "user_rejected", rejected[0].Content)
			} else {
				assert.Equal(t, int32(1), dispatched.Load())
				assert.Empty(t, findEvents(completed, orchestrator.EventToolRejected))
				calls := findEvents(completed, orchestrator.EventToolCall)
				require.Len(t, calls, 1)
				assert.Equal(t, "contact-call", calls[0].ToolCallID)
				results := findEvents(completed, orchestrator.EventToolResult)
				require.Len(t, results, 1)
				assert.Equal(t, "contact-call", results[0].ToolCallID)
				assert.Empty(t, results[0].ToolError)
				assert.Equal(t, map[string]interface{}{"ok": true}, results[0].ToolResult)
			}

			req := capLLM.firstRequest()
			require.NotNil(t, req)
			require.NotEmpty(t, req.Messages)
			assert.Equal(t, "наш 8 843 555-12-34, клиент [Скрыто], customer [Скрыто]", req.Messages[0].Content)
			require.Len(t, req.SystemBlocks, 2)
			assert.Contains(t, req.SystemBlocks[1].Text, "+7 (843) 555-12-34")
		})
	}
}

func TestResume_UnregisteredContactsRemainRedacted(t *testing.T) {
	for _, verdict := range []string{"approve", "edit", "reject"} {
		t.Run(verdict, func(t *testing.T) {
			ctx := context.Background()
			var dispatched atomic.Int32
			var executedArgs map[string]interface{}
			rec := toolregistry.ExecutorFunc(func(_ context.Context, args map[string]interface{}) (interface{}, error) {
				dispatched.Add(1)
				executedArgs = args
				return map[string]interface{}{"text": args["text"]}, nil
			})
			reg := newRegistryWithFloor("manual_tool", domain.ToolFloorManual, rec)
			repo := newMockPendingRepo()
			options := orchestrator.Options{MaxIterations: 5, RedactOutboundPDn: true}
			stub := &stubLLM{responses: []*llm.ChatResponse{{
				FinishReason: "tool_calls",
				ToolCalls: []llm.ToolCall{{
					ID: "contact-call", Type: llm.ToolCallTypeFunction,
					Function: llm.FunctionCall{Name: "manual_tool", Arguments: `{"text":"8 (916) 123-45-77; +78120000000"}`},
				}},
			}}}
			orch := orchestrator.NewWithHITL(stub, reg, repo, options)
			events, err := orch.Run(ctx, orchestrator.RunRequest{
				BusinessContext: prompt.BusinessContext{Name: "Acme", Phone: "+7 (843) 555-12-34", Website: "ab.com"},
				Messages:        []llm.Message{{Role: "user", Content: "Сделай пост: запись по телефону +7 916 123-45-77"}},
				ConversationID:  "contact-conversation",
			})
			require.NoError(t, err)
			paused := drainEvents(events)
			require.Empty(t, findEvents(paused, orchestrator.EventError))
			require.Len(t, findEvents(paused, orchestrator.EventToolApprovalRequired), 1)
			require.Len(t, repo.insertedBatches, 1)
			batch := repo.insertedBatches[0]
			var snapshot struct {
				PDnAllowlist []string      `json:"pdn_allowlist"`
				Messages     []llm.Message `json:"messages"`
			}
			require.NoError(t, json.Unmarshal(batch.ModelMessages, &snapshot))
			assert.Equal(t, []string{"+7 (843) 555-12-34", "ab.com"}, snapshot.PDnAllowlist)
			require.NotEmpty(t, snapshot.Messages)
			assert.Contains(t, snapshot.Messages[0].Content, "+7 916 123-45-77")
			batch.Calls[0].Verdict = verdict
			if verdict == "edit" {
				batch.Calls[0].EditedArgs = map[string]interface{}{"text": "Запись: +7 916 123-45-88; +78120000000"}
			}

			capLLM := &captureLLM{}
			resumed := orchestrator.NewWithHITL(capLLM, reg, repo, options)
			events, err = resumed.Resume(ctx, orchestrator.ResumeRequest{BatchID: batch.ID})
			require.NoError(t, err)
			completed := drainEvents(events)
			require.Empty(t, findEvents(completed, orchestrator.EventError))
			require.Len(t, findEvents(completed, orchestrator.EventDone), 1)
			if verdict == "reject" {
				assert.Zero(t, dispatched.Load())
				assert.Empty(t, findEvents(completed, orchestrator.EventToolCall))
				assert.Empty(t, findEvents(completed, orchestrator.EventToolResult))
				rejected := findEvents(completed, orchestrator.EventToolRejected)
				require.Len(t, rejected, 1)
				assert.Equal(t, "contact-call", rejected[0].ToolCallID)
				assert.Equal(t, "user_rejected", rejected[0].Content)
			} else {
				assert.Equal(t, int32(1), dispatched.Load())
				assert.Empty(t, findEvents(completed, orchestrator.EventToolRejected))
				calls := findEvents(completed, orchestrator.EventToolCall)
				require.Len(t, calls, 1)
				assert.Equal(t, "contact-call", calls[0].ToolCallID)
				if verdict == "edit" {
					assert.Equal(t, batch.Calls[0].EditedArgs, calls[0].ToolArgs)
					assert.Equal(t, batch.Calls[0].EditedArgs, executedArgs)
				}
				results := findEvents(completed, orchestrator.EventToolResult)
				require.Len(t, results, 1)
				assert.Equal(t, "contact-call", results[0].ToolCallID)
				assert.Empty(t, results[0].ToolError)
				assert.Equal(t, map[string]interface{}{"text": executedArgs["text"]}, results[0].ToolResult)
			}

			req := capLLM.firstRequest()
			require.NotNil(t, req)
			require.NotEmpty(t, req.Messages)
			assert.Equal(t, "Сделай пост: запись по телефону [Скрыто]", req.Messages[0].Content)
			require.Len(t, req.Messages[1].ToolCalls, 1)
			assert.JSONEq(t, `{"text":"[Скрыто]; [Скрыто]"}`, req.Messages[1].ToolCalls[0].Function.Arguments)
			assert.NotContains(t, req.Messages[1].ToolCalls[0].Function.Arguments, "+78120000000")
			switch verdict {
			case "edit":
				require.Len(t, req.Messages, 3)
				assert.JSONEq(t, `{"text":"Запись: [Скрыто]; [Скрыто]"}`, req.Messages[2].Content)
			case "approve":
				require.Len(t, req.Messages, 3)
				assert.JSONEq(t, `{"text":"[Скрыто]; [Скрыто]"}`, req.Messages[2].Content)
			}
			require.Len(t, req.SystemBlocks, 2)
			assert.Contains(t, req.SystemBlocks[1].Text, "+7 (843) 555-12-34")
		})
	}
}

func TestRun_OnlyProfileContactsAreExempt(t *testing.T) {
	for _, tt := range []struct {
		name, role, text string
	}{
		{"restriction_after", "user", "Сделай пост: запись по телефону +7 916 123-45-77 — это личный телефон клиента, не публикуй его."},
		{"quoted_copy", "user", "Опубликуй точный текст: «Запись по телефону +7 916 123-45-77»"},
		{"quoted_bare", "user", "Опубликуй: «+7 916 123-45-77»"},
		{"quoted_ascii", "user", "Напиши пост, вот текст: \"+7 916 123-45-77\""},
		{"quoted_unintroduced", "user", "Сделай пост: «Запись по телефону +7 916 123-45-77»"},
		{"unclosed_quote", "user", "Опубликуй: «+7 916 123-45-77"},
		{"dash", "user", "Размести: запись по телефону —\n+7 916 123-45-77"},
		{"intent_after", "user", "Наш телефон +7 916 123-45-77. Опубликуй."},
		{"veto_клиента", "user", "Опубликуй: «+7 916 123-45-77». клиента"},
		{"veto_посетителя", "user", "Опубликуй: «+7 916 123-45-77». посетителя"},
		{"veto_гостя", "user", "Опубликуй: «+7 916 123-45-77». гостя"},
		{"veto_личный", "user", "Опубликуй: «+7 916 123-45-77». личный"},
		{"veto_не публикуй", "user", "Опубликуй: «+7 916 123-45-77». не публикуй"},
		{"veto_не указывай", "user", "Опубликуй: «+7 916 123-45-77». не указывай"},
		{"veto_скрой", "user", "Опубликуй: «+7 916 123-45-77». скрой"},
		{"veto_перескажи", "user", "Опубликуй: «+7 916 123-45-77». перескажи"},
		{"veto_сообщение от", "user", "Опубликуй: «+7 916 123-45-77». сообщение от"},
		{"veto_пишет", "user", "Опубликуй: «+7 916 123-45-77». пишет"},
		{"booking", "user", "Сделай пост… запись по телефону +7 916 123-45-77"},
		{"formatted", "user", "Добавь в пост телефон: +7 (916) 123-45-77"},
		{"trunk", "user", "Опубликуй: телефон для публикации: 8 916 123 45 77"},
		{"english", "user", "Write a post. Booking phone: +79161234577"},
		{"visitor_summary", "user", "Перескажи сообщение посетителя: запись по телефону +7 916 123-45-77"},
		{"clients_post", "user", "Сделай пост для клиентов: запись по телефону +7 916 123-45-77"},
		{"salon", "user", "Сделай пост: в салоне запись по телефону +7 916 123-45-77"},
		{"newline", "user", "Сделай пост: запись по телефону:\n+7 916 123-45-77"},
		{"call", "user", "Опубликуй: звоните +7 916 123-45-77"},
		{"orders", "user", "Сделай пост: заказ по +7 916 123-45-77"},
		{"our_reply", "user", "Ответь: наш телефон +7 916 123-45-77"},
		{"quoted_owner_gap", "user", "Сделай пост: запись по телефону «посетителя» +7 916 123-45-77"},
		{"visitor_label", "user", "Сделай пост. Посетитель: запись по телефону +7 916 123-45-77"},
		{"review_relay", "user", "Опубликуй отзыв клиента: запись по телефону +7 916 123-45-77"},
		{"polite", "user", "Пожалуйста, сделай пост: запись по телефону +7 916 123-45-77"},
		{"unrelated_contact_negative", "user", "Не добавляй старый номер. Сделай пост: запись по телефону +7 916 123-45-77"},
		{"english_noun", "user", "The post mentions a booking phone: +7 916 123-45-77"},
		{"no_intent", "user", "Запись по телефону +7 916 123-45-77"},
		{"our_without_intent", "user", "Наш телефон +7 916 123-45-77"},
		{"relayed_publish", "user", "Перескажи сообщение посетителя: сделай пост, запись по телефону +7 916 123-45-77"},
		{"publish_relay", "user", "Сделай пост, перескажи сообщение посетителя: запись по телефону +7 916 123-45-77"},
		{"publish_quote", "user", "Сделай пост: посетитель написал «запись по телефону +7 916 123-45-77»"},
		{"negated_purpose", "user", "Сделай пост: не добавляй запись по телефону +7 916 123-45-77"},
		{"unrelated_negative", "user", "Не используй смайлики. Сделай пост: запись по телефону +7 916 123-45-77"},
		{"private", "user", "Сделай пост: личный телефон для записи +7 916 123-45-77"},
		{"word_boundary", "user", "Сделай пост: перезапись по телефону +7 916 123-45-77"},
		{"intent_boundary", "user", "Несделай пост: запись по телефону +7 916 123-45-77"},
		{"unscoped", "user", "Позвони +79161234577"},
		{"negative", "user", "Не добавь в пост телефон +79161234577"},
		{"customer", "user", "Отзыв клиента: запись по телефону +79161234577"},
		{"quoted", "user", "Он написал: «запись по телефону +79161234577»"},
		{"tool", "tool", "Запись по телефону +79161234577"},
		{"assistant", "assistant", "Запись по телефону +79161234577"},
		{"organization", "user", "Сделай пост: пишите на booking@example.org"},
		{"quoted", "user", "Опубликуй точный текст: «booking@example.org»"},
		{"quoted_ascii", "user", `Опубликуй: "booking@example.org"`},
		{"no_intent", "user", "Пишите на booking@example.org"},
		{"no_label", "user", "Сделай пост: booking@example.org"},
		{"third_party_after", "user", "Сделай пост: пишите на booking@example.org — пишет клиент"},
		{"restriction_after", "user", "Опубликуй: «booking@example.org», не указывай адрес"},
		{"mixed", "user", "Сделай пост: пишите на booking@example.org. Личный телефон +79161234577"},
		{"mixed_purposes", "user", "Сделай пост для клиентов: наш телефон +7 916 123-45-77. Перескажи сообщение посетителя: запись по телефону +7 812 000-00-00."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, registered := range []bool{false, true} {
				t.Run(fmt.Sprintf("registered=%t", registered), func(t *testing.T) {
					provider := &captureLLM{}
					business := prompt.BusinessContext{Phone: "+7 (843) 555-12-34", Website: "example.org"}
					if registered {
						business.Phone = "+7 (916) 123-45-77"
					}
					orch := orchestrator.NewWithOptions(provider, toolregistry.NewRegistry(), orchestrator.Options{MaxIterations: 2, RedactOutboundPDn: true})
					events, err := orch.Run(context.Background(), orchestrator.RunRequest{
						BusinessContext: business,
						Messages:        []llm.Message{{Role: tt.role, Content: tt.text}},
					})
					require.NoError(t, err)
					require.Empty(t, findEvents(drainEvents(events), orchestrator.EventError))
					req := provider.firstRequest()
					require.NotNil(t, req)
					require.NotEmpty(t, req.Messages)
					expected := security.RedactPIIExcept(tt.text, []string{business.Phone})
					assert.Equal(t, expected, req.Messages[0].Content)
					if !registered {
						assert.NotContains(t, req.Messages[0].Content, "916")
						assert.Contains(t, req.Messages[0].Content, "[Скрыто]")
					}
					assert.NotContains(t, req.Messages[0].Content, "booking@example.org")
					require.Len(t, req.SystemBlocks, 2)
					assert.Contains(t, req.SystemBlocks[1].Text, "контакт скрыт для конфиденциальности")
					assert.Contains(t, req.SystemBlocks[1].Text, "Настройки → Организация")
					assert.Contains(t, req.SystemBlocks[1].Text, "Не выдумывай контакт")
				})
			}
		})
	}
}
