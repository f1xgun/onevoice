package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/authz"
	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/openapi"
	"github.com/f1xgun/onevoice/services/api/internal/taskhub"
)

// Constants for task pagination
const (
	DefaultTaskLimit = 20
	MaxTaskLimit     = 100
)

// streamHeartbeatInterval keeps proxies/load balancers from closing idle SSE
// connections. Browsers ignore comment lines.
const streamHeartbeatInterval = 20 * time.Second

// AgentTaskService defines the interface for agent task operations.
type AgentTaskService interface {
	List(ctx context.Context, businessID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
}

// AgentTaskHandler handles agent task-related HTTP requests
type AgentTaskHandler struct {
	agentTaskService AgentTaskService
	hub              *taskhub.Hub
}

// NewAgentTaskHandler creates a new agent task handler instance
func NewAgentTaskHandler(agentTaskService AgentTaskService, hub *taskhub.Hub) (*AgentTaskHandler, error) {
	if agentTaskService == nil {
		return nil, fmt.Errorf("NewAgentTaskHandler: agentTaskService cannot be nil")
	}
	if hub == nil {
		return nil, fmt.Errorf("NewAgentTaskHandler: hub cannot be nil")
	}
	return &AgentTaskHandler{
		agentTaskService: agentTaskService,
		hub:              hub,
	}, nil
}

// domainAgentTaskToOpenAPI maps the internal domain.AgentTask to the
// spec-owned openapi.AgentTask wire shape. BusinessID parses string→UUID
// (corrupt rows fall back to uuid.Nil and are logged). startedAt and
// completedAt switch from absent (omitempty) to explicit null when nil
// to match the spec's nullable: true contract.
func domainAgentTaskToOpenAPI(t domain.AgentTask) openapi.AgentTask {
	businessID, err := uuid.Parse(t.BusinessID)
	if err != nil {
		slog.Warn("agent task BusinessID not a valid UUID", "taskID", t.ID, "raw", t.BusinessID, "error", err)
		businessID = uuid.Nil
	}

	out := openapi.AgentTask{
		Id:          t.ID,
		BusinessId:  businessID,
		Type:        t.Type,
		Status:      t.Status,
		Platform:    t.Platform,
		StartedAt:   t.StartedAt,
		CompletedAt: t.CompletedAt,
		CreatedAt:   t.CreatedAt,
	}
	if t.DisplayName != "" {
		v := t.DisplayName
		out.DisplayName = &v
	}
	if t.DisplayNameKey != "" {
		v := t.DisplayNameKey
		out.DisplayNameKey = &v
	}
	if t.Input != nil {
		v := t.Input
		out.Input = &v
	}
	if t.Output != nil {
		v := t.Output
		out.Output = &v
	}
	if t.Error != "" {
		v := t.Error
		out.Error = &v
	}
	if t.ErrorCode != "" {
		v := t.ErrorCode
		out.ErrorCode = &v
	}
	return out
}

// ListTasks handles GET /api/v1/tasks
func (h *AgentTaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "ListTasks", authz.PermContentRead)
	if !ok {
		return
	}

	limit, offset := parseLimitOffset(r, DefaultTaskLimit, MaxTaskLimit)
	filter := domain.TaskFilter{
		Platform: r.URL.Query().Get("platform"),
		Status:   r.URL.Query().Get("status"),
		Type:     r.URL.Query().Get("type"),
		Limit:    limit,
		Offset:   offset,
	}

	tasks, total, err := h.agentTaskService.List(r.Context(), bc.BusinessID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to list tasks", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	out := make([]openapi.AgentTask, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, domainAgentTaskToOpenAPI(t))
	}

	writeJSON(w, http.StatusOK, openapi.AgentTaskListResponse{
		Tasks: out,
		Total: total,
	})
}

// StreamTasks handles GET /api/v1/tasks/stream — an SSE endpoint that pushes
// task lifecycle events (task.created, task.updated) for the authenticated
// user's business.
func (h *AgentTaskHandler) StreamTasks(w http.ResponseWriter, r *http.Request) {
	bc, ok := requireBusiness(w, r, "StreamTasks", authz.PermContentRead)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	events, unsub := h.hub.Subscribe(bc.BusinessID.String())
	defer unsub()

	flusher.Flush()

	heartbeat := time.NewTicker(streamHeartbeatInterval)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				slog.Error("marshal task stream event", "error", err)
				continue
			}
			if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
