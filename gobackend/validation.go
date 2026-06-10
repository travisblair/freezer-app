package main

import "strings"

const (
	maxNameLength = 100
	maxQuantity   = 9999
)

// validName returns the trimmed name and true if non-empty and ≤ maxNameLength.
func validName(name string) (string, bool) {
	n := strings.TrimSpace(name)
	return n, len(n) > 0 && len(n) <= maxNameLength
}

// validQty returns true if n is 1–9999 (for create/scan).
func validQty(n int) bool {
	return n >= 1 && n <= maxQuantity
}

// validCount returns true if n is 0–9999 (for PATCH).
func validCount(n int) bool {
	return n >= 0 && n <= maxQuantity
}

// validMode returns true if m is "increment" or "decrement".
func validMode(m string) bool {
	return m == "increment" || m == "decrement"
}

// validBarcode returns the trimmed barcode and true if non-empty and ≤ 255.
func validBarcode(b string) (string, bool) {
	bc := strings.TrimSpace(b)
	return bc, len(bc) > 0 && len(bc) <= 255
}
