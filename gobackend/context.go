package main

import "net/http"

// contextKey is an unexported type used for context value keys to prevent
// collisions with keys defined in other packages.
type contextKey string

const userContextKey contextKey = "user"

// userFromContext extracts the authenticated user from the request context.
// Returns zero values if no user is present (unauthenticated request).
func userFromContext(r *http.Request) (userID uint, userName string) {
	u := r.Context().Value(userContextKey)
	if u == nil {
		return 0, ""
	}
	info := u.(struct {
		UserID uint
		Name   string
	})
	return info.UserID, info.Name
}
