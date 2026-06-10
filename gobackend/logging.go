package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Log level constants ─────────────────────────────────────────────────

type logLevel int

const (
	levelDebug logLevel = iota
	levelInfo
	levelWarn
	levelError
)

var levelNames = map[logLevel]string{
	levelDebug: "DEBUG",
	levelInfo:  "INFO",
	levelWarn:  "WARN",
	levelError: "ERROR",
}

// ── AppLogger ───────────────────────────────────────────────────────────

// AppLogger wraps Go's log.Logger with levels and dual output (stderr + file).
type AppLogger struct {
	mu         sync.Mutex
	level      logLevel
	stdLogger  *log.Logger // stderr (journald)
	fileLogger *log.Logger // data/server.log
	file       *os.File
	done       chan struct{} // closed on shutdown to stop rotation goroutine
}

var (
	appLogOnce sync.Once
	appLog     *AppLogger
)

// GetLogger returns the singleton AppLogger, initializing it on first call.
func GetLogger() *AppLogger {
	appLogOnce.Do(func() {
		appLog = newAppLogger()
	})
	return appLog
}

func newAppLogger() *AppLogger {
	al := &AppLogger{}

	// Parse log level from env
	switch os.Getenv("LOG_LEVEL") {
	case "debug":
		al.level = levelDebug
	case "warn":
		al.level = levelWarn
	case "error":
		al.level = levelError
	default:
		al.level = levelInfo
	}

	// Open log file (best-effort; failures are non-fatal)
	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		execDir, _ := os.Getwd()
		logFile = filepath.Join(execDir, "data", "server.log")
	}
	// Ensure directory exists
	dir := filepath.Dir(logFile)
	os.MkdirAll(dir, 0755)

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		al.file = f
		al.fileLogger = log.New(f, "", 0)
	} else {
		// Logger not initialized yet — use bare log for this one message
		log.Printf("WARNING: Cannot open log file %s: %v (logging to stderr only)", logFile, err)
	}

	al.stdLogger = log.New(os.Stderr, "", 0)

	al.Info("Logger initialized (level=%s, file=%s)", levelNames[al.level], logFile)

	// Start periodic log rotation check
	al.done = make(chan struct{})
	go al.rotateCheck()

	return al
}

// ── Public logging methods (printf-style) ───────────────────────────────

func (l *AppLogger) Debug(format string, v ...interface{}) {
	if l.level <= levelDebug {
		l.write(levelDebug, "", format, v...)
	}
}

func (l *AppLogger) Info(format string, v ...interface{}) {
	if l.level <= levelInfo {
		l.write(levelInfo, "", format, v...)
	}
}

func (l *AppLogger) Warn(format string, v ...interface{}) {
	if l.level <= levelWarn {
		l.write(levelWarn, "", format, v...)
	}
}

func (l *AppLogger) Error(format string, v ...interface{}) {
	if l.level <= levelError {
		l.write(levelError, "", format, v...)
	}
}

// Fatal logs at error level, flushes the log, then exits. Last resort.
func (l *AppLogger) Fatal(format string, v ...interface{}) {
	l.write(levelError, "", format, v...)
	l.Close()
	os.Exit(1)
}

func (l *AppLogger) DebugReq(reqID, format string, v ...interface{}) {
	if l.level <= levelDebug {
		l.write(levelDebug, reqID, format, v...)
	}
}

func (l *AppLogger) InfoReq(reqID, format string, v ...interface{}) {
	if l.level <= levelInfo {
		l.write(levelInfo, reqID, format, v...)
	}
}

func (l *AppLogger) WarnReq(reqID, format string, v ...interface{}) {
	if l.level <= levelWarn {
		l.write(levelWarn, reqID, format, v...)
	}
}

func (l *AppLogger) ErrorReq(reqID, format string, v ...interface{}) {
	if l.level <= levelError {
		l.write(levelError, reqID, format, v...)
	}
}

// ── Raw message methods (for pre-formatted strings) ─────────────────────

func (l *AppLogger) InfoRaw(msg string) {
	if l.level <= levelInfo {
		l.writeRaw(levelInfo, "", msg)
	}
}

func (l *AppLogger) WarnRaw(msg string) {
	if l.level <= levelWarn {
		l.writeRaw(levelWarn, "", msg)
	}
}

func (l *AppLogger) ErrorRaw(msg string) {
	if l.level <= levelError {
		l.writeRaw(levelError, "", msg)
	}
}

func (l *AppLogger) InfoReqRaw(reqID, msg string) {
	if l.level <= levelInfo {
		l.writeRaw(levelInfo, reqID, msg)
	}
}

func (l *AppLogger) WarnReqRaw(reqID, msg string) {
	if l.level <= levelWarn {
		l.writeRaw(levelWarn, reqID, msg)
	}
}

func (l *AppLogger) ErrorReqRaw(reqID, msg string) {
	if l.level <= levelError {
		l.writeRaw(levelError, reqID, msg)
	}
}

// ── Internal formatting ─────────────────────────────────────────────────

func (l *AppLogger) write(level logLevel, reqID, format string, v ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := fmt.Sprintf(format, v...)
	l.emitLocked(level, reqID, msg)
}

// writeRaw logs a pre-formatted message with no printf expansion.
func (l *AppLogger) writeRaw(level logLevel, reqID, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.emitLocked(level, reqID, msg)
}

// emitLocked writes to both outputs. Caller must hold l.mu.
func (l *AppLogger) emitLocked(level logLevel, reqID, msg string) {
	timestamp := time.Now().Format("2006-01-02T15:04:05.000")
	prefix := fmt.Sprintf("[%s] [%s]", timestamp, levelNames[level])
	if reqID != "" {
		prefix += fmt.Sprintf(" [%s]", reqID)
	}
	line := prefix + " " + msg

	l.stdLogger.Println(line)
	if l.fileLogger != nil {
		l.fileLogger.Println(line)
	}
}

// ── Log rotation ────────────────────────────────────────────────────────

const (
	maxLogSize  = 10 * 1024 * 1024 // 10MB
	maxLogFiles = 3
)

// rotateCheck runs every 5 minutes and rotates the log file if it exceeds maxLogSize.
func (l *AppLogger) rotateCheck() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-l.done:
			return
		case <-ticker.C:
		}
		l.mu.Lock()
		if l.file == nil {
			l.mu.Unlock()
			continue
		}
		info, err := l.file.Stat()
		if err != nil || info.Size() < maxLogSize {
			l.mu.Unlock()
			continue
		}
		// Rotate: close current, rename with .1/.2/.3, reopen
		logPath := l.file.Name()
		l.fileLogger = nil
		l.file.Close()

		// Shift old backups: server.log.2 → server.log.3, etc.
		for i := maxLogFiles - 1; i >= 1; i-- {
			old := fmt.Sprintf("%s.%d", logPath, i)
			new := fmt.Sprintf("%s.%d", logPath, i+1)
			os.Rename(old, new)
		}
		os.Rename(logPath, logPath+".1")

		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err == nil {
			l.file = f
			l.fileLogger = log.New(f, "", 0)
		}
		l.mu.Unlock()
	}
}

// Close flushes and closes the log file. Stops the rotation goroutine.
func (l *AppLogger) Close() {
	l.mu.Lock()
	if l.file != nil {
		l.file.Close()
		l.file = nil
		l.fileLogger = nil
	}
	l.mu.Unlock()
	// Signal rotation goroutine to stop (outside lock to avoid deadlock)
	if l.done != nil {
		close(l.done)
	}
}

// ── Request counter (for request IDs) ───────────────────────────────────

var (
	reqCount   uint64
	reqCountMu sync.Mutex
)

// nextReqID returns a short unique request identifier.
func nextReqID() string {
	reqCountMu.Lock()
	reqCount++
	n := reqCount
	reqCountMu.Unlock()
	return fmt.Sprintf("req-%d", n)
}

// RequestCount returns the total number of requests served since start.
func RequestCount() uint64 {
	reqCountMu.Lock()
	n := reqCount
	reqCountMu.Unlock()
	return n
}

// ── responseWriter wrapper ──────────────────────────────────────────────

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
	if !rw.wroteHeader {
		rw.statusCode = code
		rw.wroteHeader = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		rw.statusCode = http.StatusOK
		rw.wroteHeader = true
	}
	return rw.ResponseWriter.Write(b)
}

// ── Request logging middleware ──────────────────────────────────────────

// requestLoggingMiddleware logs every HTTP request: method, path, status, duration, remote addr.
func requestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		reqID := nextReqID()
		logger := GetLogger()

		// Set request ID in response header for client-side debugging
		w.Header().Set("X-Request-ID", reqID)

		// Wrap response writer to capture status code
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		logger.DebugReq(reqID, "%s %s from %s (UA: %s)", r.Method, r.URL.Path, r.RemoteAddr, truncate(r.UserAgent(), 80))

		next.ServeHTTP(rw, r)

		duration := time.Since(start)
		msg := fmt.Sprintf("%s %s → %d (%s)", r.Method, r.URL.Path, rw.statusCode, duration.Round(time.Microsecond))

		if rw.statusCode >= 500 {
			logger.ErrorReqRaw(reqID, msg)
		} else if rw.statusCode >= 400 {
			logger.WarnReqRaw(reqID, msg)
		} else {
			logger.InfoReqRaw(reqID, msg)
		}
	})
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}