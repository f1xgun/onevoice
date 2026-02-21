package providers

import (
	"context"
	"fmt"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// AnthropicProvider implements llm.Provider using the Anthropic API
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

// Name returns the provider identifier
func (p *AnthropicProvider) Name() string { return "anthropic" }

// HealthCheck verifies the provider is reachable by sending a minimal request
func (p *AnthropicProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.ModelClaude_3_Haiku_20240307,
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

// ListModels returns known Anthropic models (API doesn't have a list endpoint)
func (p *AnthropicProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	input3 := 3.0
	output3 := 15.0
	input5 := 1.0
	output5 := 5.0
	return []llm.ModelInfo{
		{
			ID:                 "claude-3-5-sonnet-20241022",
			Name:               "Claude 3.5 Sonnet",
			Provider:           "anthropic",
			ContextLength:      200000,
			InputCostPer1MTok:  &input3,
			OutputCostPer1MTok: &output3,
			SupportsToolUse:    true,
			SupportsStreaming:  true,
			SupportsVision:     true,
		},
		{
			ID:                 "claude-3-5-haiku-20241022",
			Name:               "Claude 3.5 Haiku",
			Provider:           "anthropic",
			ContextLength:      200000,
			InputCostPer1MTok:  &input5,
			OutputCostPer1MTok: &output5,
			SupportsToolUse:    true,
			SupportsStreaming:  true,
		},
	}, nil
}

func buildAnthropicMessages(req llm.ChatRequest) ([]anthropic.TextBlockParam, []anthropic.MessageParam) {
	var systemBlocks []anthropic.TextBlockParam
	var msgs []anthropic.MessageParam
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			systemBlocks = append(systemBlocks, anthropic.TextBlockParam{Text: m.Content, Type: "text"})
		case "user":
			msgs = append(msgs, anthropic.NewUserMessage(anthropic.NewTextBlock(m.Content)))
		case "assistant":
			msgs = append(msgs, anthropic.NewAssistantMessage(anthropic.NewTextBlock(m.Content)))
		}
	}
	return systemBlocks, msgs
}

// Chat sends a request and returns the complete response
func (p *AnthropicProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	start := time.Now()
	systemBlocks, msgs := buildAnthropicMessages(req)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  msgs,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
	}
	params.Temperature = anthropic.Float(req.Temperature)

	resp, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}

	var content string
	for _, block := range resp.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	return &llm.ChatResponse{
		Content:      content,
		FinishReason: string(resp.StopReason),
		Usage: llm.TokenUsage{
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
			TotalTokens:  int(resp.Usage.InputTokens + resp.Usage.OutputTokens),
		},
		Latency:     time.Since(start),
		RawResponse: resp,
		Provider:    "anthropic",
	}, nil
}

// ChatStream returns a channel of incremental responses
func (p *AnthropicProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	systemBlocks, msgs := buildAnthropicMessages(req)

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(req.MaxTokens),
		Messages:  msgs,
	}
	if len(systemBlocks) > 0 {
		params.System = systemBlocks
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
