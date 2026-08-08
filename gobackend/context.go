package main

import "net/http"

// contextKey is an unexported type used for context value keys to prevent
// collisions with keys defined in other packages.
type contextKey string

const userContextKey contextKey = "user"

// AuthUser holds the authenticated user's identity, injected into the request
// context by requireAuth middleware for use by handlers and audit logging.
type AuthUser struct {
	UserID uint
	Name   string
}

// userFromContext extracts the authenticated user from the request context.
// Returns zero values if no user is present (unauthenticated request).
func userFromContext(r *http.Request) (userID uint, userName string) {
	u, ok := r.Context().Value(userContextKey).(AuthUser)
	if !ok {
		return 0, ""
	}
	return u.UserID, u.Name
}
