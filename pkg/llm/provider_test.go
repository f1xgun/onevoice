package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

// MockProvider is a test implementation of Provider interface
type MockProvider struct {
	name         string
	chatResponse *ChatResponse
	chatError    error
	models       []ModelInfo
	healthError  error
}

func (m *MockProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return m.chatResponse, m.chatError
}

func (m *MockProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	ch := make(chan StreamChunk, 1)
	close(ch)
	return ch, nil
}

func (m *MockProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	return m.models, nil
}

func (m *MockProvider) HealthCheck(ctx context.Context) error {
	return m.healthError
}

func (m *MockProvider) Name() string {
	return m.name
}

// Test that MockProvider implements Provider interface
func TestMockProvider_ImplementsProvider(t *testing.T) {
	var _ Provider = (*MockProvider)(nil)
}

func TestMockProvider_Name(t *testing.T) {
	mock := &MockProvider{name: "test-provider"}
	assert.Equal(t, "test-provider", mock.Name())
}

func TestMockProvider_Chat(t *testing.T) {
	expectedResp := &ChatResponse{Content: "Hello"}
	mock := &MockProvider{chatResponse: expectedResp}

	resp, err := mock.Chat(context.Background(), ChatRequest{})
	assert.NoError(t, err)
	assert.Equal(t, expectedResp, resp)
}
