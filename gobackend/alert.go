package main

import (
	"crypto/tls"
	"fmt"
	"net"
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

// trySendAlert attempts one email alert with a hard deadline. net/smtp has
// no built-in timeout, and a stalled SMTP session (Pi WiFi flaking after
// power loss) must never hang the caller indefinitely.
func trySendAlert(subject, body string) error {
	cfg := getAlertConfig()
	if !cfg.enabled() {
		return nil
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s",
		cfg.email, cfg.to, subject, body)

	addr := net.JoinHostPort(cfg.host, cfg.port)
	auth := smtp.PlainAuth("", cfg.email, cfg.password, cfg.host)

	GetLogger().Info("Sending alert: %s", subject)

	const dialTimeout = 15 * time.Second
	const sessionTimeout = 30 * time.Second

	dialer := &net.Dialer{Timeout: dialTimeout}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	client, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()
	_ = conn.SetDeadline(time.Now().Add(sessionTimeout))
	// Upgrade to TLS when the server offers it (smtp.SendMail does this
	// automatically; the manual rewrite dropped it, which made
	// smtp.PlainAuth refuse to send credentials — "unencrypted
	// connection" — and killed every alert silently. Regression-pinned
	// by TestTrySendAlertUsesStartTLS.)
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.host}); err != nil {
			conn.Close()
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := client.Mail(cfg.email); err != nil {
		return fmt.Errorf("mail: %w", err)
	}
	if err := client.Rcpt(cfg.to); err != nil {
		return fmt.Errorf("rcpt: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

// sendAlert sends one best-effort alert, logging failures.
func sendAlert(subject, body string) {
	if err := trySendAlert(subject, body); err != nil {
		GetLogger().Error("Failed to send alert (%v)", err)
	}
}

// sendAlertWithRetry retries an alert with backoff, stopping on the first
// success. Used for the crash alert: at boot the Pi's WiFi is usually still
// associating after a power loss, and a one-shot attempt loses the most
// forensically valuable alert. Never blocks the caller.
func sendAlertWithRetry(subject, body string) {
	go func() {
		delays := []time.Duration{0, 30 * time.Second, 90 * time.Second, 180 * time.Second, 300 * time.Second}
		for i, d := range delays {
			if i > 0 {
				time.Sleep(d)
			}
			if err := trySendAlert(subject, body); err == nil {
				return
			}
		}
	}()
}

// ── Crash detection on startup ──────────────────────────────────────────

// markerPath returns the path to the clean-shutdown marker file.
// Uses the data directory (owned by the app user) instead of world-writable /tmp
// to prevent tampering by other local users.
func markerPath() string {
	execDir, _ := os.Getwd()
	return filepath.Join(execDir, "data", ".freezer-app-clean-stop")
}

// firstBootPath returns the path to a sentinel written once on the very
// first boot, so a fresh install doesn't send a false "crash" alert.
func firstBootPath() string {
	execDir, _ := os.Getwd()
	return filepath.Join(execDir, "data", ".freezer-app-first-boot")
}

// CheckCrashOnStartup should be called early in main(). If the clean
// shutdown marker is absent, the previous run crashed — send an alert
// with the last log lines for forensic context.
// If the marker exists, the previous run shut down cleanly.
func CheckCrashOnStartup() {
	marker := markerPath()
	markerExisted := false
	if _, err := os.Stat(marker); err == nil {
		markerExisted = true
		// Consume the marker UNCONDITIONALLY — even when alerts are
		// disabled. The old code early-returned before removing it, so a
		// stale marker from an alerts-disabled boot suppressed a genuine
		// crash alert on a later alerts-enabled boot.
		if err := os.Remove(marker); err != nil {
			GetLogger().Error("failed to remove clean-stop marker: %v", err)
		}
	} else if !os.IsNotExist(err) {
		GetLogger().Error("clean-stop marker check failed: %v", err)
	}

	if !getAlertConfig().enabled() {
		return
	}

	if markerExisted {
		return // previous run shut down cleanly
	}

	// Marker absent. First-ever boot (no sentinel yet) is not a crash —
	// write the sentinel and stay quiet.
	if _, err := os.Stat(firstBootPath()); os.IsNotExist(err) {
		if err := os.WriteFile(firstBootPath(), []byte("initialized"), 0644); err != nil {
			GetLogger().Error("failed to write first-boot sentinel: %v", err)
		}
		return
	}

	GetLogger().Warn("Previous run did not shut down cleanly (crash or power loss)")

	logRef := "journald: journalctl -u freezer-app -b -1 --no-pager"
	if lf := os.Getenv("LOG_FILE"); lf != "" {
		logRef = lf
	}
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
		logRef,
	)
	// Retry with backoff in the background — at boot the Pi's WiFi is often
	// still associating after a power loss, and the crash alert is the most
	// forensically valuable one. Never blocks startup.
	sendAlertWithRetry("\u26d1 Freezer-app recovered", body)
}

// WriteCleanShutdown creates a clean-stop marker, signaling a clean exit.
// CheckCrashOnStartup reads this on next boot to determine if the previous
// run shut down gracefully.
func WriteCleanShutdown() {
	if err := os.WriteFile(markerPath(), []byte("clean-stop"), 0644); err != nil {
		GetLogger().Error("failed to write clean-stop marker: %v", err)
	}
}

// ── Heartbeat ───────────────────────────────────────────────────────────

// StartHeartbeat sends a periodic status alert. Runs in the background.
func StartHeartbeat(stop <-chan struct{}, getStatus func() string) {
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
			// Send async so a stalled SMTP session can never block this
			// loop's stop handling (the deadline in trySendAlert bounds it,
			// but the loop must stay responsive regardless).
			go sendAlert("\U0001f49a Freezer-app heartbeat", status)
			// Subsequent heartbeats every 6 hours
			timer.Reset(6 * time.Hour)
		case <-stop:
			return
		}
	}
}

// ── Log tail helper ─────────────────────────────────────────────────────

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
