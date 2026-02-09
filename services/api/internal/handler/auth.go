package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/f1xgun/onevoice/pkg/domain"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

// UserService defines the interface for user-related operations
type UserService interface {
	Register(ctx context.Context, email, password string) (*domain.User, error)
	Login(ctx context.Context, email, password string) (*domain.User, string, string, error)
	RefreshToken(ctx context.Context, refreshToken string) (string, error)
	Logout(ctx context.Context, refreshToken string) error
	GetByID(ctx context.Context, userID uuid.UUID) (*domain.User, error)
}

// AuthHandler handles authentication endpoints
type AuthHandler struct {
	userService UserService
	validate    *validator.Validate
}

// NewAuthHandler creates a new auth handler instance
func NewAuthHandler(userService UserService) *AuthHandler {
	return &AuthHandler{
		userService: userService,
		validate:    validator.New(),
	}
}

// RegisterRequest represents the registration request payload
type RegisterRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// Register handles user registration
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	// Parse request body
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate request
	if err := h.validate.Struct(req); err != nil {
		writeValidationError(w, err)
		return
	}

	// Call service
	user, err := h.userService.Register(r.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, domain.ErrUserExists) {
			writeJSONError(w, http.StatusConflict, "user already exists")
			return
		}
		slog.Error("failed to register user", "error", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Return user (password hash already sanitized by service)
	writeJSON(w, http.StatusCreated, user)
}
