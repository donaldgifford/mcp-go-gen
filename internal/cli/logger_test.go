package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogger_LevelRespectsVerbose(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		verbose        bool
		wantDebugLines int // 1 if DEBUG should be emitted, 0 otherwise
	}{
		{name: "default_level_info_drops_debug", verbose: false, wantDebugLines: 0},
		{name: "verbose_level_debug_emits_debug", verbose: true, wantDebugLines: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			logger := newLogger(&buf, tt.verbose)
			logger.DebugContext(context.Background(), "debug line")
			logger.InfoContext(context.Background(), "info line")

			got := strings.Count(buf.String(), `"level":"DEBUG"`)
			if got != tt.wantDebugLines {
				t.Errorf("newLogger(verbose=%v) debug lines = %d, want %d (output: %q)",
					tt.verbose, got, tt.wantDebugLines, buf.String())
			}
			if !strings.Contains(buf.String(), `"level":"INFO"`) {
				t.Errorf("newLogger(verbose=%v) missing INFO line (output: %q)",
					tt.verbose, buf.String())
			}
		})
	}
}

func TestNewLogger_EmitsJSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := newLogger(&buf, false)
	logger.Info("hello", "key", "value")

	out := buf.String()
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("newLogger output not JSON: %q", out)
	}
	if !strings.Contains(out, `"msg":"hello"`) || !strings.Contains(out, `"key":"value"`) {
		t.Errorf("newLogger output missing fields: %q", out)
	}
}

func TestLoggerFrom_FallbackWhenMissing(t *testing.T) {
	t.Parallel()

	got := loggerFrom(context.Background())
	if got == nil {
		t.Fatal("loggerFrom(empty ctx) = nil, want fallback logger")
	}
}

func TestLoggerFrom_ReturnsInstalledLogger(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	want := newLogger(&buf, true)
	ctx := context.WithValue(context.Background(), loggerKey{}, want)

	got := loggerFrom(ctx)
	if got != want {
		t.Errorf("loggerFrom returned %p, want installed logger %p", got, want)
	}
	// Sanity: installed logger should still honor its level.
	got.Debug("from ctx")
	if !strings.Contains(buf.String(), `"msg":"from ctx"`) {
		t.Errorf("installed logger did not emit: %q", buf.String())
	}
}

func TestNewLogger_DiscardingIsWritable(t *testing.T) {
	t.Parallel()

	// Use nil writer via a concrete implementation that satisfies io.Writer.
	// We're checking that a successfully constructed logger behaves as a
	// real slog.Logger.
	var buf bytes.Buffer
	logger := newLogger(&buf, false)
	_, ok := any(logger).(*slog.Logger)
	if !ok {
		t.Fatal("newLogger did not return *slog.Logger")
	}
}
