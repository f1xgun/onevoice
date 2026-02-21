package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/f1xgun/onevoice/services/api/internal/middleware"
)

// Constants for task pagination
const (
	DefaultTaskLimit = 20
	MaxTaskLimit     = 100
)

// AgentTaskService defines the interface for agent task operations used by handler
type AgentTaskService interface {
	List(ctx context.Context, userID uuid.UUID, filter domain.TaskFilter) ([]domain.AgentTask, int, error)
}

// AgentTaskHandler handles agent task-related HTTP requests
type AgentTaskHandler struct {
	agentTaskService AgentTaskService
}

// NewAgentTaskHandler creates a new agent task handler instance
func NewAgentTaskHandler(agentTaskService AgentTaskService) *AgentTaskHandler {
	if agentTaskService == nil {
		panic("agentTaskService cannot be nil")
	}
	return &AgentTaskHandler{
		agentTaskService: agentTaskService,
	}
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
