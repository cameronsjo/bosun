package log

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", DebugLevel},
		{"DEBUG", DebugLevel},
		{"info", InfoLevel},
		{"INFO", InfoLevel},
		{"warn", WarnLevel},
		{"WARN", WarnLevel},
		{"warning", WarnLevel},
		{"error", ErrorLevel},
		{"ERROR", ErrorLevel},
		{"invalid", InfoLevel}, // Defaults to info
		{"", InfoLevel},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, parseLevel(tt.input))
		})
	}
}

func TestDetectFormat(t *testing.T) {
	// Test daemon mode detection.
	t.Setenv("BOSUN_DAEMON_MODE", "true")
	assert.Equal(t, FormatJSON, detectFormat())

	// Non-terminal defaults to JSON (can't easily test terminal detection).
	// In a pipe or test context, stdout is not a terminal.
	assert.Equal(t, FormatJSON, detectFormat())
}

func TestInit(t *testing.T) {
	// Test with explicit options.
	Init(&Options{
		Format:   FormatJSON,
		Level:    DebugLevel,
		LevelSet: true,
	})
	assert.Equal(t, FormatJSON, GetFormat())
	assert.Equal(t, DebugLevel, GetLevel())

	// Test with env vars.
	t.Setenv("BOSUN_LOG_FORMAT", "console")
	t.Setenv("BOSUN_LOG_LEVEL", "warn")
	Init(nil)
	assert.Equal(t, FormatConsole, GetFormat())
	assert.Equal(t, WarnLevel, GetLevel())

	// Reset to defaults.
	Init(&Options{
		Format:   FormatJSON,
		Level:    InfoLevel,
		LevelSet: true,
	})
}

func TestInitHonorsOutput(t *testing.T) {
	tests := []struct {
		name   string
		format Format
	}{
		{name: "json", format: FormatJSON},
		{name: "console", format: FormatConsole},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			Init(&Options{
				Format:   tt.format,
				Level:    InfoLevel,
				LevelSet: true,
				Output:   &output,
			})
			t.Cleanup(func() {
				Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
			})

			Info().Msg("custom output")

			assert.Contains(t, output.String(), "custom output")
		})
	}
}

func TestInitFansOutFromCustomOutput(t *testing.T) {
	var primary bytes.Buffer
	var additional bytes.Buffer
	Init(&Options{
		Format:            FormatJSON,
		Level:             InfoLevel,
		LevelSet:          true,
		Output:            &primary,
		AdditionalWriters: []io.Writer{&additional},
	})
	t.Cleanup(func() {
		Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
	})

	Info().Msg("fan out")

	assert.Contains(t, primary.String(), "fan out")
	assert.Equal(t, primary.String(), additional.String())
}

func TestInitNilClearsOutputOverride(t *testing.T) {
	var output bytes.Buffer
	Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true, Output: &output})
	require.Same(t, &output, config.output)

	Init(nil)
	t.Cleanup(func() {
		Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
	})

	assert.Nil(t, config.output)
}

func TestEnableDaemonModeUsesJSONForAutoFormat(t *testing.T) {
	t.Setenv("BOSUN_DAEMON_MODE", "")
	var output bytes.Buffer
	Init(&Options{
		Format:   FormatAuto,
		Level:    InfoLevel,
		LevelSet: true,
		Output:   &output,
	})
	t.Cleanup(func() {
		Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
	})

	EnableDaemonMode()
	Info().Msg("daemon output")

	assert.Equal(t, "true", os.Getenv("BOSUN_DAEMON_MODE"))
	assert.Equal(t, FormatJSON, GetFormat())
	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "daemon output", entry["message"])
}

func TestEnableDaemonModePreservesExplicitConfiguration(t *testing.T) {
	t.Setenv("BOSUN_DAEMON_MODE", "")
	var primary bytes.Buffer
	var additional bytes.Buffer
	Init(&Options{
		Format:            FormatConsole,
		Level:             DebugLevel,
		LevelSet:          true,
		Output:            &primary,
		AdditionalWriters: []io.Writer{&additional},
	})
	t.Cleanup(func() {
		Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
	})

	EnableDaemonMode()
	EnableDaemonMode()
	Debug().Msg("preserved daemon configuration")

	assert.Equal(t, FormatConsole, GetFormat())
	assert.Equal(t, DebugLevel, GetLevel())
	assert.Equal(t, 1, strings.Count(primary.String(), "preserved daemon configuration"))
	assert.Equal(t, 1, strings.Count(additional.String(), "preserved daemon configuration"))
}

func TestEnableDaemonModeUsesAdditionalWriterSnapshot(t *testing.T) {
	t.Setenv("BOSUN_DAEMON_MODE", "")
	var primary bytes.Buffer
	var configured bytes.Buffer
	var replacement bytes.Buffer
	additionalWriters := []io.Writer{&configured}
	Init(&Options{
		Format:            FormatJSON,
		Level:             InfoLevel,
		LevelSet:          true,
		Output:            &primary,
		AdditionalWriters: additionalWriters,
	})
	t.Cleanup(func() {
		Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
	})

	additionalWriters[0] = &replacement
	EnableDaemonMode()
	Info().Msg("snapshotted writer")

	assert.Contains(t, primary.String(), "snapshotted writer")
	assert.Equal(t, primary.String(), configured.String())
	assert.Empty(t, replacement.String())
}

func TestEnableDaemonModePreservesRuntimeLogLevel(t *testing.T) {
	t.Setenv("BOSUN_DAEMON_MODE", "")
	var output bytes.Buffer
	Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true, Output: &output})
	t.Cleanup(func() {
		Init(&Options{Format: FormatJSON, Level: InfoLevel, LevelSet: true})
	})

	SetGlobalLevel(DebugLevel)
	EnableDaemonMode()
	Debug().Msg("runtime level preserved")

	assert.Equal(t, DebugLevel, GetLevel())
	assert.Contains(t, output.String(), "runtime level preserved")
}

func TestLoggerOutput(t *testing.T) {
	// Capture output.
	var buf bytes.Buffer
	logger = zerolog.New(&buf).With().Timestamp().Logger()

	Info().Msg("test message")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, "info", logEntry["level"])
	assert.Equal(t, "test message", logEntry["message"])
}

func TestLoggerWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	Info().
		Str(FieldComponent, ComponentDaemon).
		Str(FieldSource, SourceWebhook).
		Msg("processing request")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, ComponentDaemon, logEntry[FieldComponent])
	assert.Equal(t, SourceWebhook, logEntry[FieldSource])
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	// Test request ID.
	ctx, requestID := NewRequestContext(ctx)
	assert.NotEmpty(t, requestID)
	assert.Equal(t, requestID, RequestIDFromContext(ctx))

	// Test reconcile ID.
	ctx, reconcileID := NewReconcileContext(ctx)
	assert.NotEmpty(t, reconcileID)
	assert.Equal(t, reconcileID, ReconcileIDFromContext(ctx))

	// Test FromContext includes IDs.
	var buf bytes.Buffer
	logger = zerolog.New(&buf)
	l := FromContext(ctx)
	l.Info().Msg("test")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, requestID, logEntry[FieldRequestID])
	assert.Equal(t, reconcileID, logEntry[FieldReconcileID])
}

func TestWithRequestIDEmpty(t *testing.T) {
	ctx := context.Background()

	// Empty ID should generate new UUID.
	ctx = WithRequestID(ctx, "")
	id := RequestIDFromContext(ctx)
	assert.NotEmpty(t, id)
	assert.Len(t, id, 36) // UUID format.
}

func TestWithReconcileIDExplicit(t *testing.T) {
	ctx := context.Background()

	// Explicit ID should be used.
	ctx = WithReconcileID(ctx, "custom-id-123")
	assert.Equal(t, "custom-id-123", ReconcileIDFromContext(ctx))
}

func TestDurationMS(t *testing.T) {
	start := time.Now().Add(-100 * time.Millisecond)
	duration := DurationMS(start)
	assert.GreaterOrEqual(t, duration, int64(100))
}

func TestComponent(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	l := Component(ComponentReconcile)
	l.Info().Msg("reconciling")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, ComponentReconcile, logEntry[FieldComponent])
}

func TestWithDuration(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	start := time.Now().Add(-50 * time.Millisecond)
	WithDuration(Info(), start).Msg("operation complete")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	duration := int64(logEntry[FieldDurationMS].(float64))
	assert.GreaterOrEqual(t, duration, int64(50))
}

func TestWithError(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	err := assert.AnError
	WithError(Error(), err).Msg("operation failed")

	output := buf.String()
	assert.True(t, strings.Contains(output, "error"))
}

func TestWithErrorNil(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	WithError(Info(), nil).Msg("no error")

	output := buf.String()
	assert.False(t, strings.Contains(output, "error\":"))
}

func TestComponentCtxWithFullContext(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	// Build context with correlation IDs and stash enriched logger.
	ctx := context.Background()
	ctx = WithReconcileID(ctx, "rec-123")
	ctx = WithRequestID(ctx, "req-456")
	l := FromContext(ctx)
	ctx = WithContext(ctx, &l)

	// ComponentCtx should inherit both IDs plus add component.
	cl := ComponentCtx(ctx, ComponentGit)
	cl.Info().Msg("cloning")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, ComponentGit, logEntry[FieldComponent])
	assert.Equal(t, "rec-123", logEntry[FieldReconcileID])
	assert.Equal(t, "req-456", logEntry[FieldRequestID])
}

func TestComponentCtxWithEmptyContext(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	// Empty context — should behave like Component().
	ctx := context.Background()
	cl := ComponentCtx(ctx, ComponentDeploy)
	cl.Info().Msg("deploying")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, ComponentDeploy, logEntry[FieldComponent])
	assert.Nil(t, logEntry[FieldReconcileID])
	assert.Nil(t, logEntry[FieldRequestID])
}

func TestComponentCtxIsChainable(t *testing.T) {
	var buf bytes.Buffer
	logger = zerolog.New(&buf)

	ctx := context.Background()
	ctx = WithReconcileID(ctx, "rec-789")
	l := FromContext(ctx)
	ctx = WithContext(ctx, &l)

	// Chain additional fields on top of ComponentCtx.
	cl := ComponentCtx(ctx, ComponentDeploy).With().
		Str(FieldTarget, "unraid").
		Logger()
	cl.Info().Msg("deploying to target")

	var logEntry map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &logEntry))
	assert.Equal(t, ComponentDeploy, logEntry[FieldComponent])
	assert.Equal(t, "rec-789", logEntry[FieldReconcileID])
	assert.Equal(t, "unraid", logEntry[FieldTarget])
}
