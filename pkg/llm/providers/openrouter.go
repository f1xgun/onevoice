package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// OpenRouterProvider implements llm.Provider using OpenRouter's OpenAI-compatible API
type OpenRouterProvider struct {
	client *openai.Client
}

// NewOpenRouter creates a new OpenRouter provider. Returns nil if apiKey is empty.
func NewOpenRouter(apiKey string) *OpenRouterProvider {
	if apiKey == "" {
		return nil
	}
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = "https://openrouter.ai/api/v1"
	return &OpenRouterProvider{client: openai.NewClientWithConfig(cfg)}
}

// Name returns the provider identifier
func (p *OpenRouterProvider) Name() string { return "openrouter" }

// HealthCheck verifies the provider is reachable
func (p *OpenRouterProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("openrouter health check: %w", err)
	}
	return nil
}

// ListModels returns available models from OpenRouter
func (p *OpenRouterProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	models, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openrouter list models: %w", err)
	}
	result := make([]llm.ModelInfo, 0, len(models.Models))
	for _, m := range models.Models {
		result = append(result, llm.ModelInfo{
			ID:                m.ID,
			Name:              m.ID,
			Provider:          "openrouter",
			SupportsStreaming: true,
		})
	}
	return result, nil
}

// Chat sends a request and returns the complete response
func (p *OpenRouterProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	start := time.Now()

	msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			oaiToolCalls := make([]openai.ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				oaiToolCalls[j] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolType(tc.Type),
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
			msg.ToolCalls = oaiToolCalls
		}
		msgs[i] = msg
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	}

	if len(req.Tools) > 0 {
		tools := make([]openai.Tool, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			}
		}
		oaiReq.Tools = tools
	}

	resp, err := p.client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter chat: %w", err)
	}

	var content, finishReason string
	var toolCalls []llm.ToolCall
	if len(resp.Choices) > 0 {
		choice := resp.Choices[0]
		content = choice.Message.Content
		finishReason = string(choice.FinishReason)
		for _, tc := range choice.Message.ToolCalls {
			toolCalls = append(toolCalls, llm.ToolCall{
				ID:   tc.ID,
				Type: string(tc.Type),
				Function: llm.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			})
		}
	}

	return &llm.ChatResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
		Usage: llm.TokenUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
			TotalTokens:  resp.Usage.TotalTokens,
		},
		Latency:     time.Since(start),
		RawResponse: resp,
		Provider:    "openrouter",
	}, nil
}

// ChatStream returns a channel of incremental responses
func (p *OpenRouterProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	msgs := make([]openai.ChatCompletionMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
		if m.ToolCallID != "" {
			msg.ToolCallID = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			oaiToolCalls := make([]openai.ToolCall, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				oaiToolCalls[j] = openai.ToolCall{
					ID:   tc.ID,
					Type: openai.ToolType(tc.Type),
					Function: openai.FunctionCall{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
			msg.ToolCalls = oaiToolCalls
		}
		msgs[i] = msg
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Stream:      true,
	}

	if len(req.Tools) > 0 {
		tools := make([]openai.Tool, len(req.Tools))
		for i, t := range req.Tools {
			tools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			}
		}
		oaiReq.Tools = tools
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter stream: %w", err)
	}

	ch := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()
		for {
			resp, err := stream.Recv()
			if err != nil {
				if errors.Is(err, io.EOF) {
					select {
					case ch <- llm.StreamChunk{Done: true}:
					case <-ctx.Done():
					}
				} else {
					select {
					case ch <- llm.StreamChunk{Error: err, Done: true}:
					case <-ctx.Done():
					}
				}
				return
			}
			delta := ""
			if len(resp.Choices) > 0 {
				delta = resp.Choices[0].Delta.Content
			}
			select {
			case ch <- llm.StreamChunk{Delta: delta}:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}
