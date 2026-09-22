package main

import "errors"

// Sentinel errors for cross-handler classification (pitfall #17 — never
// classify by string-matching error text).
var (
	ErrInsufficientCount      = errors.New("insufficient count")
	ErrShelfNotFound          = errors.New("shelf not found")
	ErrConcurrentModification = errors.New("concurrent modification")
)
