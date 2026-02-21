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

// SelfHostedProvider implements llm.Provider for any OpenAI-compatible inference server.
type SelfHostedProvider struct {
	client *openai.Client
	name   string
}

// NewSelfHosted creates a provider pointing at baseURL.
// Returns nil if name or baseURL is empty.
// apiKey is optional — pass "" if the server requires no authentication.
// name must be unique (e.g. "selfhosted-0") to distinguish multiple endpoints in the router.
func NewSelfHosted(name, baseURL, apiKey string) *SelfHostedProvider {
	if name == "" || baseURL == "" {
		return nil
	}
	key := apiKey
	if key == "" {
		key = "none" // go-openai requires a non-empty string; most servers ignore it
	}
	cfg := openai.DefaultConfig(key)
	cfg.BaseURL = baseURL
	return &SelfHostedProvider{
		client: openai.NewClientWithConfig(cfg),
		name:   name,
	}
}

// Name returns the unique provider identifier set at construction time.
func (p *SelfHostedProvider) Name() string { return p.name }

// HealthCheck always returns nil — self-hosted servers may not support /v1/models.
func (p *SelfHostedProvider) HealthCheck(_ context.Context) error { return nil }

// ListModels returns empty — model discovery is not reliable on self-hosted servers.
func (p *SelfHostedProvider) ListModels(_ context.Context) ([]llm.ModelInfo, error) {
	return nil, nil
}

// Chat sends a chat completion request to the self-hosted server.
func (p *SelfHostedProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
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
		return nil, fmt.Errorf("selfhosted chat: %w", err)
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
		Provider:    p.name,
	}, nil
}

// ChatStream returns a channel of incremental responses from the self-hosted server.
func (p *SelfHostedProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
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
		return nil, fmt.Errorf("selfhosted stream: %w", err)
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
