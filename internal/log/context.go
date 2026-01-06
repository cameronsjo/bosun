package log

import (
	"context"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	requestIDKey   contextKey = "request_id"
	reconcileIDKey contextKey = "reconcile_id"
)

// WithRequestID adds a request ID to the context.
// If id is empty, a new UUID is generated.
func WithRequestID(ctx context.Context, id string) context.Context {
	if id == "" {
		id = uuid.New().String()
	}
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext retrieves the request ID from context.
// Returns empty string if not present.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithReconcileID adds a reconcile run ID to the context.
// If id is empty, a new UUID is generated.
func WithReconcileID(ctx context.Context, id string) context.Context {
	if id == "" {
		id = uuid.New().String()
	}
	return context.WithValue(ctx, reconcileIDKey, id)
}

// ReconcileIDFromContext retrieves the reconcile ID from context.
// Returns empty string if not present.
func ReconcileIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(reconcileIDKey).(string); ok {
		return id
	}
	return ""
}

// Ctx returns a logger from the context, or the global logger if none exists.
// This is the primary way to get a context-aware logger.
func Ctx(ctx context.Context) *zerolog.Logger {
	l := zerolog.Ctx(ctx)
	if l.GetLevel() == zerolog.Disabled {
		return &logger
	}
	return l
}

// WithContext returns a new context with the logger attached.
func WithContext(ctx context.Context, l *zerolog.Logger) context.Context {
	return l.WithContext(ctx)
}

// FromContext creates a logger with request/reconcile IDs from the context.
func FromContext(ctx context.Context) zerolog.Logger {
	l := logger.With()

	if requestID := RequestIDFromContext(ctx); requestID != "" {
		l = l.Str(FieldRequestID, requestID)
	}

	if reconcileID := ReconcileIDFromContext(ctx); reconcileID != "" {
		l = l.Str(FieldReconcileID, reconcileID)
	}

	return l.Logger()
}

// NewRequestContext creates a new context with a request ID and returns both
// the context and the ID.
func NewRequestContext(ctx context.Context) (context.Context, string) {
	id := uuid.New().String()
	return WithRequestID(ctx, id), id
}

// NewReconcileContext creates a new context with a reconcile ID and returns both
// the context and the ID.
func NewReconcileContext(ctx context.Context) (context.Context, string) {
	id := uuid.New().String()
	return WithReconcileID(ctx, id), id
}
