// Package obs provides OpenTelemetry contextual logging.
package obs

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// ContextHandler wraps a slog.Handler to inject active trace and span IDs into log records.
type ContextHandler struct {
	Handler slog.Handler
}

// NewContextHandler constructs a ContextHandler around a base slog.Handler.
func NewContextHandler(base slog.Handler) *ContextHandler {
	if base == nil {
		base = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     slog.LevelDebug,
			AddSource: true,
		})
	}
	return &ContextHandler{Handler: base}
}

// Enabled reports whether the handler handles records at the given level.
func (h *ContextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, level)
}

// Handle injects OpenTelemetry trace context and action metadata into the slog.Record.
func (h *ContextHandler) Handle(ctx context.Context, r slog.Record) error {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		sctx := span.SpanContext()
		r.AddAttrs(
			slog.String("trace_id", sctx.TraceID().String()),
			slog.String("span_id", sctx.SpanID().String()),
			slog.String("trace_flags", sctx.TraceFlags().String()),
		)
		if st := getObsState(ctx); st != nil && st.action != "" {
			r.AddAttrs(slog.String("action", st.action))
		}
	}
	err := h.Handler.Handle(ctx, r)
	if err != nil {
		return err
	}
	return nil
}

// WithAttrs returns a new ContextHandler whose attributes include given attrs.
func (h *ContextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ContextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

// WithGroup returns a new ContextHandler with the given group name appended.
func (h *ContextHandler) WithGroup(name string) slog.Handler {
	return &ContextHandler{Handler: h.Handler.WithGroup(name)}
}
