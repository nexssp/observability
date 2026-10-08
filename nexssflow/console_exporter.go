package nexssflow

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// consoleExporter writes one line per span to a writer. Intended for
// local development where a developer wants to see tracing without
// running a collector.
type consoleExporter struct {
	w  io.Writer
	mu sync.Mutex
}

func newConsoleExporter(w io.Writer) *consoleExporter {
	return &consoleExporter{w: w}
}

func (e *consoleExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, span := range spans {
		status := "ok "
		if span.Status().Code == codes.Error {
			status = "err"
		}

		parent := "--------"
		if pid := span.Parent().SpanID(); pid.IsValid() {
			parent = pid.String()[:8]
		}

		fmt.Fprintf(e.w, "%s [%s] %-40s %8s  trace=%s  span=%s  parent=%s\n",
			span.StartTime().Local().Format("15:04:05.000"),
			status,
			truncateName(span.Name(), 40),
			formatSpanDuration(span.EndTime().Sub(span.StartTime())),
			span.SpanContext().TraceID().String()[:8],
			span.SpanContext().SpanID().String()[:8],
			parent,
		)
	}
	return nil
}

func (e *consoleExporter) Shutdown(_ context.Context) error { return nil }

func truncateName(name string, limit int) string {
	if len(name) <= limit {
		return name
	}
	return name[:limit-1] + "…"
}

func formatSpanDuration(d time.Duration) string {
	switch {
	case d < time.Microsecond:
		return fmt.Sprintf("%dns", d.Nanoseconds())
	case d < time.Millisecond:
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000.0)
	case d < time.Second:
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}
