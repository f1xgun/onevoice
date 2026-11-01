package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	natslib "github.com/nats-io/nats.go"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/f1xgun/onevoice/pkg/mtls"
	"github.com/f1xgun/onevoice/pkg/tools"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/natsexec"
	"github.com/f1xgun/onevoice/services/orchestrator/internal/toolregistry"
)

const (
	scheduleRequestTimeout = 10 * time.Second
	scheduleErrorLimit     = 4096
	scheduleReceiptLimit   = 1 << 20
)

type telegramScheduleExecutor struct {
	tool, baseURL string
	client        *http.Client
	immediate     toolregistry.ApprovalExecutor
}

func (e *telegramScheduleExecutor) Execute(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	return e.ExecuteWithApproval(ctx, args, "")
}
func (e *telegramScheduleExecutor) ExecuteWithApproval(ctx context.Context, args map[string]interface{}, approvalID string) (interface{}, error) {
	raw, scheduled := args["scheduled_at"]
	if !scheduled {
		return e.immediate.ExecuteWithApproval(ctx, args, approvalID)
	}
	value, ok := raw.(string)
	var err error
	if value != "" {
		_, err = time.Parse(time.RFC3339, value)
	}
	if !ok || err != nil || approvalID == "" {
		return nil, fmt.Errorf("RFC3339 scheduled_at and manual approval are required")
	}
	body, err := json.Marshal(map[string]interface{}{"business_id": a2a.BusinessIDFromContext(ctx), "conversation_id": a2a.ConversationIDFromContext(ctx), "approval_id": approvalID, "tool": e.tool, "args": args})
	if err != nil {
		return nil, fmt.Errorf("encode approved schedule: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.baseURL, "/")+"/internal/v1/posts/schedule", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build approved schedule request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := e.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("record approved schedule: %w", err)
	}
	defer func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			slog.DebugContext(ctx, "close scheduling response", "error", closeErr)
		}
	}()
	if response.StatusCode != http.StatusOK {
		message, readErr := io.ReadAll(io.LimitReader(response.Body, scheduleErrorLimit))
		if readErr != nil {
			return nil, fmt.Errorf("read scheduling rejection: %w", readErr)
		}
		return nil, fmt.Errorf("schedule rejected (%d): %s", response.StatusCode, message)
	}
	var receipt map[string]interface{}
	if err := json.NewDecoder(io.LimitReader(response.Body, scheduleReceiptLimit)).Decode(&receipt); err != nil {
		return nil, fmt.Errorf("decode approved schedule receipt: %w", err)
	}
	if value == "" {
		target, targetOK := receipt["external_id"].(string)
		if receipt["status"] != "approved_now" || receipt["approval_id"] != approvalID || !targetOK || target == "" {
			return nil, fmt.Errorf("API did not confirm the explicit Now edit")
		}
		immediateArgs := make(map[string]interface{}, len(args)+1)
		for key, item := range args {
			immediateArgs[key] = item
		}
		immediateArgs["channel_id"] = target
		immediateArgs["strict_target"] = true
		return e.immediate.ExecuteWithApproval(ctx, immediateArgs, approvalID)
	}
	if receipt["api_owned_schedule"] != true || receipt["post_record_id"] == "" || receipt["post_record_id"] == nil {
		return nil, fmt.Errorf("API did not confirm a scheduled post record")
	}
	return receipt, nil
}

// registerTelegramScheduling reuses the internal mTLS client configuration and existing manual tool specs.
func registerTelegramScheduling(reg *toolregistry.Registry, nc *natslib.Conn, baseURL string) error {
	transport := &http.Transport{}
	if mtls.IsEnabled() {
		paths, err := mtls.PathsFromEnv()
		if err != nil {
			return fmt.Errorf("schedule mTLS paths: %w", err)
		}
		tlsConfig, err := mtls.LoadClientTLSConfig(paths)
		if err != nil {
			return fmt.Errorf("schedule mTLS client: %w", err)
		}
		transport.TLSClientConfig = tlsConfig
	}
	client := &http.Client{Timeout: scheduleRequestTimeout, Transport: transport}
	for _, spec := range telegramTools() {
		name := spec.Def.Function.Name
		if name != tools.TelegramSendChannelPost && name != tools.TelegramSendChannelPhoto {
			continue
		}
		reg.Register(spec, &telegramScheduleExecutor{tool: name, baseURL: baseURL, client: client, immediate: natsexec.New(a2a.AgentTelegram, name, natsexec.NewNATSConn(nc))})
	}
	return nil
}
