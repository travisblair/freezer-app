package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// startupTime is captured when the server starts, used by heartbeatStatus.
var startupTime time.Time

func main() {
	// --hash-password: read a password from stdin, print the bcrypt hash, and exit.
	if len(os.Args) > 1 && os.Args[1] == "--hash-password" {
		hashPasswordCmd()
		return
	}

	// Initialize the structured logger first — everything else uses it.
	logger := GetLogger()

	// Check if the previous run crashed (missing clean-shutdown marker).
	// Sends an alert if so.
	CheckCrashOnStartup()

	db := OpenDB()

	// Graceful shutdown plumbing
	sqlDB, err := db.DB()
	if err != nil {
		logger.Fatal("failed to get underlying sql.DB: %v", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	serverErr := make(chan error, 1)

	mux := http.NewServeMux()
	setupRoutes(mux, db)

	// Wrap with middleware: security headers → request logging → CORS → panic recovery
	handler := securityHeadersMiddleware(
		requestLoggingMiddleware(
			corsMiddleware(
				recoveryMiddleware(mux),
			),
		),
	)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      requestSizeLimitMiddleware(handler),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
		// Pi Zero W has limited file descriptors; limit idle conns
		MaxHeaderBytes: 1 << 16, // 64KB
	}

	// Systemd watchdog integration
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Watchdog goroutine panicked: %v", r)
			}
		}()
		startWatchdog(stop)
	}()

	// Heartbeat alerts (every 6 hours, first after 5 minutes)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Heartbeat goroutine panicked: %v", r)
			}
		}()
		StartHeartbeat(stop, heartbeatStatus)
	}()

	// Auth rate-limit map cleanup (prevents unbounded memory growth)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("Auth cleanup goroutine panicked: %v", r)
			}
		}()
		authCleanup()
	}()

	// Track startup time for heartbeat
	startupTime = time.Now()

	// Run server in background so we can wait for shutdown signal
	go func() {
		logger.Info("Freezer app server running on http://localhost:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Don't os.Exit here — that bypasses all graceful shutdown.
			// Send the error to the main goroutine which runs cleanup.
			select {
			case serverErr <- err:
			default:
			}
		}
	}()

	// Wait for termination signal or server error
	select {
	case sig := <-stop:
		logger.Info("Received signal %v, shutting down gracefully...", sig)
	case err := <-serverErr:
		logger.Error("Server stopped unexpectedly: %v (shutting down)", err)
	}

	// Notify systemd we're stopping
	notifyWatchdog("STOPPING=1")

	// Stop accepting new requests, drain in-flight with a deadline
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error: %v", err)
	}

	// Close database cleanly
	if sqlDB != nil {
		if err := sqlDB.Close(); err != nil {
			logger.Error("Database close error: %v", err)
		}
	}

	// Write clean-shutdown marker so next startup knows this was intentional
	WriteCleanShutdown()

	// Close log file (also stops rotation goroutine)
	logger.Close()

	logger.Info("Server stopped.")
}

// recoveryMiddleware catches panics in any handler and returns 500.
// Logs the triggering request details for crash forensics.
func recoveryMiddleware(next http.Handler) http.Handler {
	logger := GetLogger()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("PANIC RECOVERED | %s %s | remote=%s | UA=%s | panic=%v\n%s",
					r.Method, r.URL.Path, r.RemoteAddr, truncate(r.UserAgent(), 100), rec, debug.Stack())
				http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// securityHeadersMiddleware sets defensive HTTP security headers on every response.
// Applied outermost so all responses include these headers.
func securityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		// CSP allows same-origin resources and Pico CSS CDN for styles.
		// script-src uses 'self' only; SolidJS does not require unsafe-eval.
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"script-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://cdn.jsdelivr.net; "+
				"img-src 'self' data: blob:; "+
				"connect-src 'self'; "+
				"font-src 'self'; "+
				"media-src 'self' blob:; "+
				"object-src 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		w.Header().Set("X-Permitted-Cross-Domain-Policies", "none")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		next.ServeHTTP(w, r)
	})
}

// corsMiddleware applies CORS headers for same-origin and trusted origins.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Vary", "Origin")
		if trustedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func trustedOrigin(origin string) bool {
	// If TRUSTED_ORIGIN is set, ONLY that exact origin is allowed.
	// This pins CORS to your specific Tailscale Funnel hostname instead
	// of trusting every *.ts.net domain.
	if trusted := os.Getenv("TRUSTED_ORIGIN"); trusted != "" {
		return origin == trusted
	}

	// Local dev (must match exact host:port pattern to prevent prefix confusion)
	if origin == "http://localhost" ||
		strings.HasPrefix(origin, "http://localhost:") ||
		origin == "http://127.0.0.1" ||
		strings.HasPrefix(origin, "http://127.0.0.1:") {
		return true
	}
	// LAN access: match private IP prefixes with port separator to avoid
	// prefix confusion (e.g. 192.168.1.evil.com).
	if strings.HasPrefix(origin, "http://192.168.") {
		rest := origin[len("http://192.168."):]
		// Allow only numeric IP continuation
		if len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			return true
		}
	}
	// Tailscale Funnel — only allow the exact suffix .ts.net
	// WARNING: any Tailscale user can create a .ts.net domain.
	// Set TRUSTED_ORIGIN to pin to your specific hostname.
	if strings.HasSuffix(origin, ".ts.net") {
		if strings.HasPrefix(origin, "https://") {
			return true
		}
		if strings.HasPrefix(origin, "http://") {
			return true
		}
	}
	return false
}

// requestSizeLimitMiddleware rejects request bodies larger than maxSize bytes.
// Prevents resource exhaustion from oversized payloads.
func requestSizeLimitMiddleware(next http.Handler) http.Handler {
	const maxSize = 1 << 20 // 1 MB
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
		}
		next.ServeHTTP(w, r)
	})
}

// ── Heartbeat status ────────────────────────────────────────────────────

// heartbeatStatus returns a one-line status for periodic alerts.
func heartbeatStatus() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return fmt.Sprintf(
		"Freezer-app alive.\nUptime: %s\nMemory: %d MB\nGoroutines: %d",
		time.Since(startupTime).Round(time.Second),
		m.Alloc/1024/1024,
		runtime.NumGoroutine(),
	)
}

// ── systemd watchdog helpers ───────────────────────────────────────────

// startWatchdog sends systemd watchdog keep-alives every 15 seconds.
// Also sends READY=1 on startup so Type=notify works.
func startWatchdog(stop <-chan os.Signal) {
	// Notify systemd that the service has started successfully
	notifyWatchdog("READY=1")

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			notifyWatchdog("WATCHDOG=1")
		case <-stop:
			return
		}
	}
}

// notifyWatchdog sends a status string to systemd via the notification socket.
// Does nothing if NOTIFY_SOCKET is not set (not running under systemd).
func notifyWatchdog(state string) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return // not running under systemd, silently skip
	}
	pid := os.Getpid()
	// Use a short timeout: during system shutdown, systemd-notify may block
	// because the notification socket has already been torn down. We never
	// want a watchdog notification to stall graceful shutdown (and prevent
	// the clean-stop marker from being written).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "systemd-notify", state, "--pid", strconv.Itoa(pid))
	_ = cmd.Run() // best-effort; failures are non-fatal
}

// hashPasswordCmd reads a password from stdin, prints the bcrypt hash, and exits.
// Used for manual user seeding: ./freezer-server --hash-password
func hashPasswordCmd() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stderr, "Password: ")
	password, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error reading password: %v\n", err)
		os.Exit(1)
	}
	password = strings.TrimSpace(password)
	if password == "" {
		fmt.Fprintln(os.Stderr, "Password cannot be empty.")
		os.Exit(1)
	}

	hash, err := hashPassword(password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error hashing password: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(hash)
}