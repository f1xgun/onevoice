package domain

import "errors"

// User errors
var (
	ErrUserNotFound        = errors.New("user not found")
	ErrUserExists          = errors.New("user already exists")
	ErrInvalidCredentials  = errors.New("invalid credentials")
)

// Business errors
var (
	ErrBusinessNotFound = errors.New("business not found")
)

// Integration errors
var (
	ErrIntegrationNotFound = errors.New("integration not found")
	ErrTokenExpired        = errors.New("token expired")
)

// Auth errors
var (
	ErrUnauthorized     = errors.New("unauthorized")
	ErrForbidden        = errors.New("forbidden")
	ErrInvalidToken     = errors.New("invalid token")
	ErrTokenNotFound    = errors.New("token not found")
)

// Conversation errors
var (
	ErrConversationNotFound = errors.New("conversation not found")
)
