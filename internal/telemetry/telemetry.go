// Package telemetry provides OpenTelemetry tracing for bosun.
// When BOSUN_OTEL_ENDPOINT is configured, spans are exported via OTLP HTTP.
// When unconfigured, a noop provider is used (zero overhead).
package telemetry

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// state holds the module-level tracer provider and shutdown function.
var state struct {
	mu       sync.Mutex
	provider trace.TracerProvider
	shutdown func(context.Context) error
}

// Init initializes the OTel tracer provider with an OTLP HTTP exporter.
// If endpoint is empty, installs a noop provider (zero overhead).
// Returns a shutdown function that flushes pending spans.
func Init(ctx context.Context, serviceName, serviceVersion, endpoint string) (func(context.Context) error, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if endpoint == "" {
		np := noop.NewTracerProvider()
		state.provider = np
		otel.SetTracerProvider(np)
		state.shutdown = func(context.Context) error { return nil }
		return state.shutdown, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	state.provider = tp
	otel.SetTracerProvider(tp)

	state.shutdown = func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	return state.shutdown, nil
}

// Tracer returns a named tracer for creating spans.
// Safe to call before Init — returns a noop tracer if uninitialized.
func Tracer(name string) trace.Tracer {
	state.mu.Lock()
	p := state.provider
	state.mu.Unlock()

	if p == nil {
		return noop.NewTracerProvider().Tracer(name)
	}
	return p.Tracer(name)
}

// SpanError records an error on a span and sets its status to Error.
// No-op if err is nil.
func SpanError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

// SpanOK sets a span's status to OK.
func SpanOK(span trace.Span) {
	span.SetStatus(codes.Ok, "")
}

// StringAttr is a convenience wrapper for attribute.String.
func StringAttr(key, value string) attribute.KeyValue {
	return attribute.String(key, value)
}

// BoolAttr is a convenience wrapper for attribute.Bool.
func BoolAttr(key string, value bool) attribute.KeyValue {
	return attribute.Bool(key, value)
}

// IntAttr is a convenience wrapper for attribute.Int.
func IntAttr(key string, value int) attribute.KeyValue {
	return attribute.Int(key, value)
}
