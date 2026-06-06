package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/f1xgun/onevoice/pkg/llm"
	"github.com/f1xgun/onevoice/pkg/metrics"
)

// AnthropicProvider implements llm.Provider using the Anthropic API.
type AnthropicProvider struct {
	client *anthropic.Client
}

// NewAnthropic creates a new Anthropic provider. Returns nil if apiKey is empty.
func NewAnthropic(apiKey string) *AnthropicProvider {
	if apiKey == "" {
		return nil
	}
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicProvider{client: &client}
}

// Name returns the provider identifier.
func (p *AnthropicProvider) Name() string { return "anthropic" }

// HealthCheck verifies the provider is reachable by sending a minimal request.
//
// Uses claude-haiku-4-5 — the cheapest current-generation model. The previous
// pin to claude-3-haiku-20240307 is in the Sonnet 4 / Opus 4 deprecation cohort
// (retires 2026-06-15 per Anthropic models overview).
func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaudeHaiku4_5,
		MaxTokens: 1,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
		},
	})
	if err != nil {
		return fmt.Errorf("anthropic health check: %w", err)
	}
	return nil
}

// ListModels returns the current Claude catalog (Sonnet 4.6, Haiku 4.5, Opus
// 4.7). The Anthropic API has no list endpoint, so this is a hand-maintained
// table whose pricing must be kept in sync with anthropic_models.go.
//
// Pricing as of 2026-05-30 per platform.claude.com/docs/en/about-claude/pricing.
// Sonnet 4.6 and Opus 4.7 do not have Go consts in anthropic-sdk-go v1.22.1; the
// raw string literals here are required until the SDK ships them (see
// anthropic_models.go ::SDK NOTE for the drop-on-SDK-bump checklist).
func (p *AnthropicProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	sonnet46In, sonnet46Out := 3.0, 15.0
	haiku45In, haiku45Out := 1.0, 5.0
	opus47In, opus47Out := 5.0, 25.0
	return []llm.ModelInfo{
		{
			ID:                 "claude-sonnet-4-6",
			Name:               "Claude Sonnet 4.6",
			Provider:           "anthropic",
			ContextLength:      claudeSonnet4_6ContextLength,
			InputCostPer1MTok:  &sonnet46In,
			OutputCostPer1MTok: &sonnet46Out,
			SupportsToolUse:    true,
			SupportsStreaming:  true,
			SupportsVision:     true,
		},
		{
			ID:                 string(anthropic.ModelClaudeHaiku4_5),
			Name:               "Claude Haiku 4.5",
			Provider:           "anthropic",
			ContextLength:      claudeHaiku4_5ContextLength,
			InputCostPer1MTok:  &haiku45In,
			OutputCostPer1MTok: &haiku45Out,
			SupportsToolUse:    true,
			SupportsStreaming:  true,
			SupportsVision:     true,
		},
		{
			ID:                 "claude-opus-4-7",
			Name:               "Claude Opus 4.7",
			Provider:           "anthropic",
			ContextLength:      claudeOpus4_7ContextLength,
			InputCostPer1MTok:  &opus47In,
			OutputCostPer1MTok: &opus47Out,
			SupportsToolUse:    true,
			SupportsStreaming:  true,
			SupportsVision:     true,
		},
	}, nil
}

// toolsToAnthropic projects OneVoice's portable ToolDefinition slice into the
// Anthropic SDK's ToolUnionParam slice. The last entry carries an ephemeral
// cache_control breakpoint so the entire tool array participates in the prompt
// prefix cache (Anthropic semantics: the cache prefix extends from the start
// of the request up to and including the block bearing cache_control).
func toolsToAnthropic(in []llm.ToolDefinition) []anthropic.ToolUnionParam {
	if len(in) == 0 {
		return nil
	}
	out := make([]anthropic.ToolUnionParam, 0, len(in))
	for _, t := range in {
		schema := anthropic.ToolInputSchemaParam{
			Properties: t.Function.Parameters["properties"],
		}
		if req, ok := t.Function.Parameters["required"].([]string); ok {
			schema.Required = req
		}
		u := anthropic.ToolUnionParamOfTool(schema, t.Function.Name)
		if t.Function.Description != "" {
			u.OfTool.Description = anthropic.String(t.Function.Description)
		}
		out = append(out, u)
	}
	if last := len(out) - 1; last >= 0 && out[last].OfTool != nil {
		out[last].OfTool.CacheControl = anthropic.NewCacheControlEphemeralParam()
	}
	return out
}

// mapStopReason projects Anthropic's StopReason enum into the FinishReason
// strings the orchestrator expects (matching OpenAI conventions). The agent
// loop branches on FinishReason == "stop" / "tool_calls" / "length"; emitting
// raw Anthropic strings ("end_turn", "tool_use") silently breaks tool dispatch.
func mapStopReason(sr anthropic.StopReason) string {
	switch sr {
	case anthropic.StopReasonToolUse:
		return "tool_calls"
	case anthropic.StopReasonMaxTokens:
		return "length"
	case anthropic.StopReasonEndTurn,
		anthropic.StopReasonStopSequence,
		anthropic.StopReasonPauseTurn,
		anthropic.StopReasonRefusal:
		return "stop"
	default:
		return string(sr)
	}
}

// buildAnthropicMessagesV2 walks ChatRequest.Messages and projects them into
// Anthropic's split (system blocks + message slice) representation.
//
// Routing rules:
//   - req.SystemBlocks → preferred channel. When non-empty, every
//     SystemBlock is mapped to a TextBlockParam (Text/Type only — cache_control
//     stamping is the caller's job, see Chat() below). req.Messages is assumed
//     system-free in this branch; any stray role:"system" entry is logged via
//     a fallback append so that wiring bugs don't silently lose system content.
//   - req.Messages role:"system"     → TextBlockParam appended to systemBlocks
//     (legacy scrub path, runs ONLY when SystemBlocks is empty).
//   - role:"user"        → MessageParam{Role:user, Content:[text]}
//   - role:"assistant"   → MessageParam{Role:assistant, Content:[text?, tool_use*]}
//     where each entry in m.ToolCalls becomes a tool_use ContentBlock
//   - role:"tool"        → MessageParam{Role:user, Content:[tool_result]} —
//     Anthropic represents tool results as user-role messages
func buildAnthropicMessagesV2(req llm.ChatRequest) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var systemBlocks []anthropic.TextBlockParam
	preferSystemBlocks := len(req.SystemBlocks) > 0
	if preferSystemBlocks {
		systemBlocks = make([]anthropic.TextBlockParam, 0, len(req.SystemBlocks))
		for _, sb := range req.SystemBlocks {
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: sb.Text,
				Type: "text",
			})
		}
	}

	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if preferSystemBlocks {
				systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
					Text: m.Content,
					Type: "text",
				})
				continue
			}
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{
				Text: m.Content,
				Type: "text",
			})
		case "user":
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				blocks = append(blocks, anthropic.NewTextBlock(m.Content))
			}
			for _, tc := range m.ToolCalls {
				var input any
				if tc.Function.Arguments != "" {
					input = json.RawMessage(tc.Function.Arguments)
				} else {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(tc.ID, input, tc.Function.Name))
			}
			if len(blocks) == 0 {
				blocks = append(blocks, anthropic.NewTextBlock(""))
			}
			msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
		case "tool":
			msgs = append(msgs, anthropic.NewUserMessage(
				anthropic.NewToolResultBlock(m.ToolCallID, m.Content, false),
			))
		}
	}
	return systemBlocks, msgs
}

// stampSystemCacheControl stamps cache_control:ephemeral on the system block
// the caller wants cached.
//
//   - When req.SystemBlocks is non-empty: find the LAST entry with
//     CacheBoundary==true and stamp cache_control on the corresponding
//     systemBlocks[i] (positional 1:1 mapping with the input slice). Blocks
//     after the marked one stay uncached — this is the Block 1 / Block 2
//     split: Block 1 (CacheBoundary=true) caches the platform prefix, Block 2
//     (per-business) does not.
//   - When req.SystemBlocks is empty: legacy fallback — stamp the LAST
//     scrubbed block (legacy behavior preserved for non-migrated callers).
func stampSystemCacheControl(req llm.ChatRequest, systemBlocks []anthropic.TextBlockParam) {
	if len(systemBlocks) == 0 {
		return
	}
	if len(req.SystemBlocks) > 0 {
		for i := len(req.SystemBlocks) - 1; i >= 0; i-- {
			if req.SystemBlocks[i].CacheBoundary {
				if i < len(systemBlocks) {
					systemBlocks[i].CacheControl = anthropic.NewCacheControlEphemeralParam()
				}
				return
			}
		}
		return
	}
	systemBlocks[len(systemBlocks)-1].CacheControl = anthropic.NewCacheControlEphemeralParam()
}

// Chat sends a request and returns the complete response.
func (p *AnthropicProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	start := time.Now()
	systemBlocks, msgs := buildAnthropicMessagesV2(req)

	stampSystemCacheControl(req, systemBlocks)

	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = defaultMaxTokensFor(req.Model)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if len(req.Tools) > 0 {
		params.Tools = toolsToAnthropic(req.Tools)
	}
	params.Temperature = anthropic.Float(req.Temperature)

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}

	var content string
	var toolCalls []llm.ToolCall
	for _, block := range resp.Content {
		switch block.Type {
		case "text":
			content += block.Text
		case "tool_use":
			tu := block.AsToolUse()
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   tu.ID,
				Type: llm.ToolCallTypeFunction,
				Function: llm.FunctionCall{
					Name:      tu.Name,
					Arguments: string(tu.Input),
				},
			})
		}
	}

	metrics.RecordLLMCacheUsage(
		req.Model,
		int(resp.Usage.CacheReadInputTokens),
		int(resp.Usage.CacheCreationInputTokens),
		int(resp.Usage.InputTokens),
	)

	return &llm.ChatResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: mapStopReason(resp.StopReason),
		Usage: llm.TokenUsage{
			InputTokens:         int(resp.Usage.InputTokens),
			OutputTokens:        int(resp.Usage.OutputTokens),
			TotalTokens:         int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
			CacheReadTokens:     int(resp.Usage.CacheReadInputTokens),
			CacheCreationTokens: int(resp.Usage.CacheCreationInputTokens),
		},
		Latency:  time.Since(start),
		Provider: "anthropic",
	}, nil
}

// ChatStream returns a channel of incremental responses.
//
// MaxTokens defaulting is applied identically to Chat. Cache control on
// system / tools is intentionally NOT applied here — the streaming-cache
// surface lives elsewhere (current ChatStream only handles text_delta events,
// not tool_use deltas, so streaming a turn that ends in tool_use would
// silently drop the tool call).
func (p *AnthropicProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	systemBlocks, msgs := buildAnthropicMessagesV2(req)

	maxTokens := int64(req.MaxTokens)
	if maxTokens == 0 {
		maxTokens = defaultMaxTokensFor(req.Model)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: maxTokens,
		Messages:  msgs,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	if len(req.Tools) > 0 {
		params.Tools = toolsToAnthropic(req.Tools)
	}
	params.Temperature = anthropic.Float(req.Temperature)

	stream := p.client.Messages.NewStreaming(ctx, params)

	ch := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()
		for stream.Next() {
			event := stream.Current()
			switch event.Type {
			case "content_block_delta":
				delta := event.AsContentBlockDelta()
				if delta.Delta.Type == "text_delta" {
					select {
					case ch <- llm.StreamChunk{Delta: delta.Delta.Text}:
					case <-ctx.Done():
						return
					}
				}
			case "message_stop":
				select {
				case ch <- llm.StreamChunk{Done: true}:
				case <-ctx.Done():
				}
				return
			}
		}
		if err := stream.Err(); err != nil {
			select {
			case ch <- llm.StreamChunk{Error: err, Done: true}:
			case <-ctx.Done():
			}
		}
	}()

	return ch, nil
}
