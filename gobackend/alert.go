package main

import (
	"fmt"
	"net/smtp"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Alert configuration (all from env) ──────────────────────────────────
//
// Required env vars to enable alerts:
//   ALERT_SMTP_HOST       e.g. smtp.gmail.com
//   ALERT_SMTP_PORT       e.g. 587
//   ALERT_EMAIL           e.g. you@gmail.com
//   ALERT_APP_PASSWORD    Gmail app password (not your real password)
//   ALERT_TO              e.g. you@gmail.com (where alerts are sent)
//
// If any are missing, alerts are silently disabled.

type alertConfig struct {
	host     string
	port     string
	email    string
	password string
	to       string
}

var (
	alertCfgOnce sync.Once
	alertCfg     *alertConfig
)

func getAlertConfig() *alertConfig {
	alertCfgOnce.Do(func() {
		alertCfg = &alertConfig{
			host:     os.Getenv("ALERT_SMTP_HOST"),
			port:     os.Getenv("ALERT_SMTP_PORT"),
			email:    os.Getenv("ALERT_EMAIL"),
			password: os.Getenv("ALERT_APP_PASSWORD"),
			to:       os.Getenv("ALERT_TO"),
		}
	})
	return alertCfg
}

// enabled returns true if all alert config values are set.
func (c *alertConfig) enabled() bool {
	return c.host != "" && c.port != "" && c.email != "" && c.password != "" && c.to != ""
}

// ── Send alert ──────────────────────────────────────────────────────────

// sendAlert sends an email alert. Fails silently if alerts are
// disabled or the SMTP send fails (best-effort, logged).
func sendAlert(subject, body string) {
	cfg := getAlertConfig()
	if !cfg.enabled() {
		return
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		cfg.email, cfg.to, subject, body)

	addr := fmt.Sprintf("%s:%s", cfg.host, cfg.port)
	auth := smtp.PlainAuth("", cfg.email, cfg.password, cfg.host)

	GetLogger().Info("Sending alert: %s", subject)

	if err := smtp.SendMail(addr, auth, cfg.email, []string{cfg.to}, []byte(msg)); err != nil {
		GetLogger().Error("Failed to send alert: %v", err)
	}
}

// ── Crash detection on startup ──────────────────────────────────────────

// markerPath returns the path to the clean-shutdown marker file.
// Uses the data directory (owned by the app user) instead of world-writable /tmp
// to prevent tampering by other local users.
func markerPath() string {
	execDir, _ := os.Getwd()
	return filepath.Join(execDir, "data", ".freezer-app-clean-stop")
}

// CheckCrashOnStartup should be called early in main(). If the clean
// shutdown marker is absent, the previous run crashed — send an alert
// with the last log lines for forensic context.
// If the marker exists, the previous run shut down cleanly.
func CheckCrashOnStartup() {
	cfg := getAlertConfig()
	if !cfg.enabled() {
		return
	}

	marker := markerPath()
	if _, err := os.Stat(marker); err == nil {
		// Marker exists — previous run shut down cleanly.
		// Remove it so we start fresh.
		os.Remove(marker)
	} else if os.IsNotExist(err) {
		// Marker absent — previous run crashed or this is first run.
		GetLogger().Warn("Previous run did not shut down cleanly (crash or power loss)")

		body := fmt.Sprintf(
			"The freezer app crashed or lost power and has recovered.\n"+
				"Time: %s\n"+
				"Requests served before crash: %d\n\n"+
				"Pre-crash system state:\n%s\n\n"+
				"Server log: %s\n"+
				"System state log: ~/freezer-app/data/system-capture.log",
			time.Now().Format("Jan 2, 3:04 PM MST"),
			RequestCount(),
			tailOfCaptureLog(),
			logLocation(),
		)
		sendAlert("\u26d1 Freezer-app recovered", body)
	}
}

// WriteCleanShutdown creates a clean-stop marker, signaling a clean exit.
// CheckCrashOnStartup reads this on next boot to determine if the previous
// run shut down gracefully.
func WriteCleanShutdown() {
	os.WriteFile(markerPath(), []byte("clean-stop"), 0644)
}

// ── Heartbeat ───────────────────────────────────────────────────────────

// StartHeartbeat sends a periodic status alert. Runs in the background.
func StartHeartbeat(stop <-chan os.Signal, getStatus func() string) {
	cfg := getAlertConfig()
	if !cfg.enabled() {
		return
	}

	// First heartbeat after 5 minutes (to avoid spamming on frequent restarts)
	timer := time.NewTimer(5 * time.Minute)

	for {
		select {
		case <-timer.C:
			status := getStatus()
			sendAlert("\U0001f49a Freezer-app heartbeat", status)
			// Subsequent heartbeats every 6 hours
			timer.Reset(6 * time.Hour)
		case <-stop:
			return
		}
	}
}

// ── Log tail helper ─────────────────────────────────────────────────────

// logLocation returns where the server log is written (for crash alert context).
func logLocation() string {
	if lf := os.Getenv("LOG_FILE"); lf != "" {
		return lf
	}
	return "/tmp/server.log"
}

// tailOfCaptureLog returns the last ~1000 bytes of the system-capture log.
// This survives reboots (stored on SD card) and contains pre-crash state.
func tailOfCaptureLog() string {
	captureFile := os.Getenv("CAPTURE_LOG")
	if captureFile == "" {
		execDir, _ := os.Getwd()
		captureFile = filepath.Join(execDir, "data", "system-capture.log")
	}

	f, err := os.Open(captureFile)
	if err != nil {
		return "(no capture log yet)"
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "(capture log not readable)"
	}

	const tailBytes = 1000
	offset := info.Size() - tailBytes
	if offset < 0 {
		offset = 0
	}

	buf := make([]byte, info.Size()-offset)
	if _, err := f.ReadAt(buf, offset); err != nil {
		return "(failed to read capture log)"
	}

	result := string(buf)
	if len(result) > 1000 {
		result = result[len(result)-1000:]
	}
	if result == "" {
		return "(capture log was empty)"
	}
	return result
}