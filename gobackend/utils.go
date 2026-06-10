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
	json.NewEncoder(w).Encode(v)
}

// errorJSON sends a JSON error response.
func errorJSON(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// escapeCSV quotes a field if needed for CSV output.
func escapeCSV(val string) string {
	if strings.ContainsAny(val, ",\"\n\r") {
		return `"` + strings.ReplaceAll(val, `"`, `""`) + `"`
	}
	return val
}

// writeCSV writes a CSV response with proper headers.
func writeCSV(w http.ResponseWriter, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="freezer-inventory.csv"`)
	writer := csv.NewWriter(w)
	for _, row := range rows {
		writer.Write(row)
	}
	writer.Flush()
}