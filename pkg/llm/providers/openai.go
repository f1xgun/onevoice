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

// OpenAIProvider implements llm.Provider using the official OpenAI API
type OpenAIProvider struct {
	client *openai.Client
}

// NewOpenAI creates a new OpenAI provider. Returns nil if apiKey is empty.
func NewOpenAI(apiKey string) *OpenAIProvider {
	if apiKey == "" {
		return nil
	}
	return &OpenAIProvider{client: openai.NewClient(apiKey)}
}

// Name returns the provider identifier
func (p *OpenAIProvider) Name() string { return "openai" }

// HealthCheck verifies the provider is reachable
func (p *OpenAIProvider) HealthCheck(ctx context.Context) error {
	_, err := p.client.ListModels(ctx)
	if err != nil {
		return fmt.Errorf("openai health check: %w", err)
	}
	return nil
}

// ListModels returns available models from OpenAI
func (p *OpenAIProvider) ListModels(ctx context.Context) ([]llm.ModelInfo, error) {
	models, err := p.client.ListModels(ctx)
	if err != nil {
		return nil, fmt.Errorf("openai list models: %w", err)
	}
	result := make([]llm.ModelInfo, 0, len(models.Models))
	for _, m := range models.Models {
		result = append(result, llm.ModelInfo{
			ID:                m.ID,
			Name:              m.ID,
			Provider:          "openai",
			SupportsStreaming: true,
			SupportsToolUse:   true,
		})
	}
	return result, nil
}

// Chat sends a request and returns the complete response
func (p *OpenAIProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
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

	// Add tool definitions if present
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
		return nil, fmt.Errorf("openai chat: %w", err)
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
		Provider:    "openai",
	}, nil
}

// ChatStream returns a channel of incremental responses
func (p *OpenAIProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
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

	// Add tool definitions if present
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
		return nil, fmt.Errorf("openai stream: %w", err)
	}

	ch := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()
		for {
			resp, err := stream.Recv()
			if err != nil {
				chunk := llm.StreamChunk{Done: true}
				if !errors.Is(err, io.EOF) {
					chunk.Error = err
				}
				select {
				case ch <- chunk:
				case <-ctx.Done():
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
