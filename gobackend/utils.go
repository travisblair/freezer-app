package main

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
)

// writeJSON sends a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// errorJSON sends a JSON error response.
func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
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