package telemetry

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestInit_EmptyEndpoint_ReturnsNoopProvider(t *testing.T) {
	ctx := context.Background()

	shutdown, err := Init(ctx, "bosun", "test", "")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown should be a no-op.
	require.NoError(t, shutdown(ctx))

	// Global provider should be noop.
	tp := otel.GetTracerProvider()
	assert.IsType(t, noop.NewTracerProvider(), tp)
}

func TestInit_WithEndpoint_ConfiguresProvider(t *testing.T) {
	ctx := context.Background()

	// Use localhost with a short timeout; Init succeeds because the SDK
	// batches exports asynchronously.
	shutdown, err := Init(ctx, "bosun", "test", "http://127.0.0.1:14318")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Provider should NOT be noop.
	tp := otel.GetTracerProvider()
	assert.NotEqual(t, noop.NewTracerProvider(), tp)

	// Shutdown with a short deadline so we don't wait for export retries.
	shutdownCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	_ = shutdown(shutdownCtx)
}

func TestTracer_BeforeInit_ReturnsNoop(t *testing.T) {
	// Reset state to simulate pre-Init.
	state.mu.Lock()
	prev := state.provider
	state.provider = nil
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.provider = prev
		state.mu.Unlock()
	}()

	tracer := Tracer("test")
	assert.NotNil(t, tracer)

	// Should produce a non-recording span.
	_, span := tracer.Start(context.Background(), "test.op")
	assert.False(t, span.IsRecording())
	span.End()
}

func TestSpanError_NilError_NoOp(t *testing.T) {
	_, span := noop.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	// Should not panic.
	SpanError(span, nil)
	span.End()
}

func TestSpanOK(t *testing.T) {
	_, span := noop.NewTracerProvider().Tracer("test").Start(context.Background(), "op")
	// Should not panic.
	SpanOK(span)
	span.End()
}

func TestConvenienceAttrs(t *testing.T) {
	t.Run("StringAttr", func(t *testing.T) {
		kv := StringAttr("key", "val")
		assert.Equal(t, "key", string(kv.Key))
		assert.Equal(t, "val", kv.Value.AsString())
	})

	t.Run("BoolAttr", func(t *testing.T) {
		kv := BoolAttr("enabled", true)
		assert.Equal(t, "enabled", string(kv.Key))
		assert.True(t, kv.Value.AsBool())
	})

	t.Run("IntAttr", func(t *testing.T) {
		kv := IntAttr("count", 42)
		assert.Equal(t, "count", string(kv.Key))
		assert.Equal(t, int64(42), kv.Value.AsInt64())
	})
}

// TestSpanHierarchy_InMemory verifies parent-child span relationships using
// an in-memory exporter, avoiding network timeouts.
func TestSpanHierarchy_InMemory(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	// Install the test provider.
	state.mu.Lock()
	prev := state.provider
	state.provider = tp
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.provider = prev
		state.mu.Unlock()
		_ = tp.Shutdown(context.Background())
	}()

	ctx := context.Background()
	tracer := Tracer("reconcile")

	parentCtx, parentSpan := tracer.Start(ctx, "reconcile")
	assert.True(t, parentSpan.IsRecording())

	_, childSpan := tracer.Start(parentCtx, "reconcile.git_sync")
	assert.True(t, childSpan.IsRecording())

	// Child should share the parent's trace ID.
	assert.Equal(t, parentSpan.SpanContext().TraceID(), childSpan.SpanContext().TraceID())

	childSpan.End()
	parentSpan.End()

	// Verify spans were recorded.
	spans := exporter.GetSpans()
	assert.Len(t, spans, 2)
}

// TestSpanError_RecordsOnSpan verifies SpanError records the error event.
func TestSpanError_RecordsOnSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))

	state.mu.Lock()
	prev := state.provider
	state.provider = tp
	state.mu.Unlock()
	defer func() {
		state.mu.Lock()
		state.provider = prev
		state.mu.Unlock()
		_ = tp.Shutdown(context.Background())
	}()

	ctx := context.Background()
	_, span := Tracer("test").Start(ctx, "test.op")
	testErr := errors.New("something broke")

	SpanError(span, testErr)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	// The span should have events (the recorded error).
	assert.NotEmpty(t, spans[0].Events)
}
