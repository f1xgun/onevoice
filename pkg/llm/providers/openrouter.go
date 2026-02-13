package providers

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/f1xgun/onevoice/pkg/llm"
	openai "github.com/sashabaranov/go-openai"
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
	return err
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
			ID:               m.ID,
			Name:             m.ID,
			Provider:         "openrouter",
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
		msgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	}

	resp, err := p.client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter chat: %w", err)
	}

	content := ""
	if len(resp.Choices) > 0 {
		content = resp.Choices[0].Message.Content
	}

	finishReason := ""
	if len(resp.Choices) > 0 {
		finishReason = string(resp.Choices[0].FinishReason)
	}

	return &llm.ChatResponse{
		Content:      content,
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
		msgs[i] = openai.ChatCompletionMessage{Role: m.Role, Content: m.Content}
	}

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Stream:      true,
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter stream: %w", err)
	}

	ch := make(chan llm.StreamChunk, 16)
	go func() {
		defer close(ch)
		defer stream.Close()
		for {
			resp, err := stream.Recv()
			if err != nil {
				if err == io.EOF {
					ch <- llm.StreamChunk{Done: true}
				} else {
					ch <- llm.StreamChunk{Error: err, Done: true}
				}
				return
			}
			delta := ""
			if len(resp.Choices) > 0 {
				delta = resp.Choices[0].Delta.Content
			}
			ch <- llm.StreamChunk{Delta: delta}
		}
	}()

	return ch, nil
}
