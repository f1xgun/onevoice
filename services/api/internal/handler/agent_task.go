package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
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

// AgentTaskService defines the interface for agent task operations used by handler
type AgentTaskService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
	ResolveBusinessID(ctx context.Context, userID uuid.UUID) (string, error)
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

// TaskListResponse represents the task list response
type TaskListResponse struct {
	Tasks []domain.AgentTask `json:"tasks"`
	Total int                `json:"total"`
}

// ListTasks handles GET /api/v1/tasks
func (h *AgentTaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// Parse query parameters
	filter := domain.TaskFilter{
		Platform: r.URL.Query().Get("platform"),
		Status:   r.URL.Query().Get("status"),
		Type:     r.URL.Query().Get("type"),
		Limit:    DefaultTaskLimit,
		Offset:   0,
	}

	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			filter.Limit = parsedLimit
			if filter.Limit > MaxTaskLimit {
				filter.Limit = MaxTaskLimit
			}
		}
	}

	if offsetStr := r.URL.Query().Get("offset"); offsetStr != "" {
		if parsedOffset, err := strconv.Atoi(offsetStr); err == nil && parsedOffset >= 0 {
			filter.Offset = parsedOffset
		}
	}

	tasks, total, err := h.agentTaskService.List(r.Context(), userID, filter)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("failed to list tasks", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, TaskListResponse{
		Tasks: tasks,
		Total: total,
	})
}

// StreamTasks handles GET /api/v1/tasks/stream — an SSE endpoint that pushes
// task lifecycle events (task.created, task.updated) for the authenticated
// user's business.
func (h *AgentTaskHandler) StreamTasks(w http.ResponseWriter, r *http.Request) {
	userID, err := middleware.GetUserID(r.Context())
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	businessID, err := h.agentTaskService.ResolveBusinessID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, domain.ErrBusinessNotFound) {
			writeJSONError(w, http.StatusNotFound, "business not found")
			return
		}
		slog.Error("resolve business for task stream", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
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

	events, unsub := h.hub.Subscribe(businessID)
	defer unsub()

	// Immediately flush headers so the browser commits the connection.
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
