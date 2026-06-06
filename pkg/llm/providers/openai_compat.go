package providers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"

	"github.com/f1xgun/onevoice/pkg/llm"
)

// openAICompatProvider is the shared OpenAI-compatible Chat/ChatStream
// implementation backing the OpenAI, OpenRouter and SelfHosted providers.
// providerName is reported in ChatResponse.Provider; errPrefix is the
// per-provider error-message prefix (it may differ from providerName, e.g.
// SelfHosted reports its instance name but prefixes errors with "selfhosted").
type openAICompatProvider struct {
	client       *openai.Client
	providerName string
	errPrefix    string
}

// concatSystemBlocks joins llm.SystemBlock.Text values with "\n\n" — the
// OpenAI-family concatenation convention for the SystemBlocks channel.
// CacheBoundary is silently ignored: OpenAI / OpenRouter / SelfHosted do not
// expose a comparable prompt-cache surface.
func concatSystemBlocks(blocks []llm.SystemBlock) string {
	if len(blocks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(blocks))
	for _, b := range blocks {
		parts = append(parts, b.Text)
	}
	return strings.Join(parts, "\n\n")
}

// projectOpenAIMessages renders the openai-compatible message slice for both
// Chat and ChatStream. When req.SystemBlocks is non-empty, a single leading
// role:"system" message carries the concatenated blocks; the caller-supplied
// Messages slice is assumed system-free. When SystemBlocks is empty, every
// Messages entry (including any role:"system") is forwarded verbatim — this is
// the back-compat fallback for non-migrated callers.
func projectOpenAIMessages(req llm.ChatRequest) []openai.ChatCompletionMessage {
	estimated := len(req.Messages)
	if len(req.SystemBlocks) > 0 {
		estimated++
	}
	msgs := make([]openai.ChatCompletionMessage, 0, estimated)
	if len(req.SystemBlocks) > 0 {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    "system",
			Content: concatSystemBlocks(req.SystemBlocks),
		})
	}
	for _, m := range req.Messages {
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
		msgs = append(msgs, msg)
	}
	return msgs
}

// buildOpenAITools maps llm.Tool entries onto the go-openai tool slice, or nil
// when the request carries no tools.
func buildOpenAITools(tools []llm.ToolDefinition) []openai.Tool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]openai.Tool, len(tools))
	for i, t := range tools {
		out[i] = openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		}
	}
	return out
}

// Chat sends a request and returns the complete response.
func (p *openAICompatProvider) Chat(ctx context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	start := time.Now()

	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    projectOpenAIMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Tools:       buildOpenAITools(req.Tools),
	}

	resp, err := p.client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("%s chat: %w", p.errPrefix, err)
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
		Latency:  time.Since(start),
		Provider: p.providerName,
	}, nil
}

// ChatStream returns a channel of incremental responses.
func (p *openAICompatProvider) ChatStream(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamChunk, error) {
	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    projectOpenAIMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Stream:      true,
		Tools:       buildOpenAITools(req.Tools),
	}

	stream, err := p.client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("%s stream: %w", p.errPrefix, err)
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
