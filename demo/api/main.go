// Package main runs the demo API for IMPL-0002 — a small env-driven
// HTTP service backing the generated MCP server's proxy tools. Three
// route trees mirror the three demo auth flavors (`/api/noauth`,
// `/api/bearer`, `/api/oauth2flow`); phase 1 wires the first two.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	addr := envDefault("DEMO_API_ADDR", ":8080")
	bearer := os.Getenv("DEMO_BEARER_TOKEN")
	if bearer == "" {
		return errors.New("DEMO_BEARER_TOKEN env var is required")
	}
	level := parseLevel(envDefault("DEMO_LOG_LEVEL", "info"))

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	store := NewStore()
	store.Seed(SeedRecords())

	mux := buildMux(store, bearer)

	srv := &http.Server{
		Addr:              addr,
		Handler:           accessLog(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("demo api listening", "addr", srv.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, sCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer sCancel()
		return srv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// buildMux wires every route the demo API serves. Extracted from run so
// handler tests can construct the same mux without spinning up signal
// handlers and listeners.
func buildMux(store *Store, bearerToken string) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// /api/noauth/* — no middleware, every method allowed.
	mux.HandleFunc("GET /api/noauth", listHandler(store))
	mux.HandleFunc("GET /api/noauth/{id}", getHandler(store))
	mux.HandleFunc("POST /api/noauth/{id}", updateHandler(store))
	mux.HandleFunc("PUT /api/noauth", createHandler(store))

	// /api/bearer/* — wrapped per route to keep the wiring readable.
	auth := bearerAuth(bearerToken)
	mux.Handle("GET /api/bearer", auth(listHandler(store)))
	mux.Handle("GET /api/bearer/{id}", auth(getHandler(store)))
	mux.Handle("POST /api/bearer/{id}", auth(updateHandler(store)))
	mux.Handle("PUT /api/bearer", auth(createHandler(store)))

	return mux
}

func envDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// accessLog wraps next with a one-line slog entry per request. The
// path is logged as an attribute on a slog.JSONHandler — values are
// JSON-escaped on emit, so log-format injection from the URL is not a
// concern despite gosec G706's general warning.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("http request", //nolint:gosec // G706: JSON-escaped via slog handler
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds())
	})
}

// statusRecorder captures the response status code so accessLog can
// include it in the log line. Wraps the underlying ResponseWriter
// transparently; pointer receiver to keep the recorder shareable.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
