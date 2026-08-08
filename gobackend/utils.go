package main

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
)

// writeJSON sends a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		GetLogger().Error("JSON encode failed: %v", err)
	}
}

// errorJSON sends a JSON error response.
func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// csvSafe prefixes a cell value with a single quote if it starts with a
// character that triggers formula execution in Excel/LibreOffice (=, +, -, @).
func csvSafe(s string) string {
	if len(s) > 0 && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@') {
		return "'" + s
	}
	return s
}

// escapeLike escapes SQL LIKE wildcards (% and _) and the escape character itself.
// Use this when building LIKE patterns from user input to prevent surprise matches.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// writeCSV writes a CSV response with proper headers.
// csv.NewWriter handles RFC 4180 quoting (commas, quotes, newlines) correctly.
func writeCSV(w http.ResponseWriter, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="freezer-inventory.csv"`)
	writer := csv.NewWriter(w)
	for _, row := range rows {
		writer.Write(row)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		GetLogger().Error("CSV write failed: %v", err)
	}
}