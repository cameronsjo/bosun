package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
	collectortracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func preserveTelemetryState(t *testing.T) {
	t.Helper()
	globalProvider := otel.GetTracerProvider()
	state.mu.Lock()
	provider := state.provider
	shutdown := state.shutdown
	state.mu.Unlock()
	t.Cleanup(func() {
		state.mu.Lock()
		state.provider = provider
		state.shutdown = shutdown
		state.mu.Unlock()
		otel.SetTracerProvider(globalProvider)
	})
}

func TestInit_EmptyEndpoint_ReturnsNoopProvider(t *testing.T) {
	preserveTelemetryState(t)
	ctx := context.Background()

	shutdown, err := Init(ctx, "bosun", "test", " \t ")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Shutdown should be a no-op.
	require.NoError(t, shutdown(ctx))

	// Global provider should be noop.
	tp := otel.GetTracerProvider()
	assert.IsType(t, noop.NewTracerProvider(), tp)
}

func TestInit_WithEndpoint_ExportsOnShutdown(t *testing.T) {
	preserveTelemetryState(t)
	ctx := context.Background()
	type exportRequest struct {
		method      string
		path        string
		contentType string
		body        []byte
		err         error
	}
	requests := make(chan exportRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		requests <- exportRequest{
			method:      r.Method,
			path:        r.URL.Path,
			contentType: r.Header.Get("Content-Type"),
			body:        body,
			err:         err,
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Schemes are case-insensitive, and configured paths are treated as base
	// endpoint noise because Bosun uses the standard /v1/traces path.
	endpoint := strings.Replace(server.URL, "http://", "HTTP://", 1) + "/ignored/path"
	shutdown, err := Init(ctx, "bosun", "test", endpoint)
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	// Provider should NOT be noop.
	tp := otel.GetTracerProvider()
	assert.NotEqual(t, noop.NewTracerProvider(), tp)

	_, span := Tracer("test").Start(ctx, "test.op")
	span.End()

	shutdownCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	require.NoError(t, shutdown(shutdownCtx))

	select {
	case request := <-requests:
		require.NoError(t, request.err)
		assert.Equal(t, http.MethodPost, request.method)
		assert.Equal(t, "/v1/traces", request.path)
		assert.Equal(t, "application/x-protobuf", request.contentType)
		assert.NotEmpty(t, request.body)

		var exportRequest collectortracepb.ExportTraceServiceRequest
		require.NoError(t, proto.Unmarshal(request.body, &exportRequest))
		resourceSpans := exportRequest.GetResourceSpans()
		require.Len(t, resourceSpans, 1)
		attrs := make(map[string]string)
		for _, attr := range resourceSpans[0].GetResource().GetAttributes() {
			attrs[attr.GetKey()] = attr.GetValue().GetStringValue()
		}
		assert.Equal(t, "bosun", attrs["service.name"])
		assert.Equal(t, "test", attrs["service.version"])
	case <-time.After(time.Second):
		t.Fatal("collector did not receive flushed span")
	}
}

func TestInit_CanceledContextPreservesProvider(t *testing.T) {
	preserveTelemetryState(t)
	previous := noop.NewTracerProvider()
	state.mu.Lock()
	state.provider = previous
	state.mu.Unlock()
	otel.SetTracerProvider(previous)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	shutdown, err := Init(ctx, "bosun", "test", "http://127.0.0.1:4318")
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, shutdown)

	state.mu.Lock()
	assert.Equal(t, previous, state.provider)
	state.mu.Unlock()
	assert.Equal(t, previous, otel.GetTracerProvider())
}

func TestInit_InvalidEndpointPreservesProvider(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{name: "unsupported scheme", endpoint: "grpc://collector:4317", wantErr: "unsupported OpenTelemetry endpoint scheme"},
		{name: "port-only authority", endpoint: "http://:4318", wantErr: "must include a host"},
		{name: "empty query", endpoint: "http://collector:4318?", wantErr: "query or fragment"},
		{name: "empty fragment", endpoint: "http://collector:4318#", wantErr: "query or fragment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preserveTelemetryState(t)
			previous := noop.NewTracerProvider()
			state.mu.Lock()
			state.provider = previous
			state.mu.Unlock()
			otel.SetTracerProvider(previous)

			shutdown, err := Init(context.Background(), "bosun", "test", tt.endpoint)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, shutdown)

			state.mu.Lock()
			assert.Equal(t, previous, state.provider)
			state.mu.Unlock()
			assert.Equal(t, previous, otel.GetTracerProvider())
		})
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		name         string
		endpoint     string
		wantHostPort string
		wantInsecure bool
		wantErr      string
	}{
		{name: "bare host and port", endpoint: "collector:4318", wantHostPort: "collector:4318", wantInsecure: true},
		{name: "bare IPv4 and port", endpoint: "127.0.0.1:4318", wantHostPort: "127.0.0.1:4318", wantInsecure: true},
		{name: "bare bracketed IPv6 and port", endpoint: "[::1]:4318", wantHostPort: "[::1]:4318", wantInsecure: true},
		{name: "uppercase HTTP URL", endpoint: "HTTP://collector:4318", wantHostPort: "collector:4318", wantInsecure: true},
		{name: "HTTP base URL", endpoint: "http://collector:4318/v1/traces", wantHostPort: "collector:4318", wantInsecure: true},
		{name: "base URL with trailing slash", endpoint: "http://collector:4318/otel/", wantHostPort: "collector:4318", wantInsecure: true},
		{name: "percent-encoded path", endpoint: "http://collector:4318/tenant%2Fone", wantHostPort: "collector:4318", wantInsecure: true},
		{name: "case-insensitive HTTPS URL", endpoint: "HTTPS://collector.example:4318/otel", wantHostPort: "collector.example:4318"},
		{name: "IPv6 URL", endpoint: "https://[::1]:4318", wantHostPort: "[::1]:4318"},
		{name: "whitespace", endpoint: " ", wantErr: "endpoint is empty"},
		{name: "unsupported scheme", endpoint: "grpc://collector:4317", wantErr: "unsupported"},
		{name: "hostless URL", endpoint: "http://", wantErr: "include a host"},
		{name: "port-only bare endpoint", endpoint: ":4318", wantErr: "include a host"},
		{name: "port-only URL", endpoint: "http://:4318", wantErr: "include a host"},
		{name: "userinfo", endpoint: "http://user:secret@collector:4318", wantErr: "user information"},
		{name: "query", endpoint: "http://collector:4318?token=secret", wantErr: "query or fragment"},
		{name: "empty query", endpoint: "http://collector:4318?", wantErr: "query or fragment"},
		{name: "fragment", endpoint: "http://collector:4318#fragment", wantErr: "query or fragment"},
		{name: "empty fragment", endpoint: "http://collector:4318#", wantErr: "query or fragment"},
		{name: "invalid escape", endpoint: "http://collector:4318/%zz", wantErr: "parse OpenTelemetry endpoint"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostPort, insecure, err := parseEndpoint(tt.endpoint)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				assert.Empty(t, hostPort)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantHostPort, hostPort)
			assert.Equal(t, tt.wantInsecure, insecure)
		})
	}
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
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	_, span := tp.Tracer("test").Start(context.Background(), "op")
	SpanOK(span)
	span.End()

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Ok, spans[0].Status.Code)
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
	assert.Equal(t, codes.Error, spans[0].Status.Code)
	assert.Equal(t, testErr.Error(), spans[0].Status.Description)
}
