package nexssflow

import (
	"context"
	"fmt"
	"os"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func buildTraceProcessor(kind string) (sdktrace.SpanProcessor, error) {
	switch kind {
	case "", "otlp":
		return nil, nil // provider falls back to OTLP/env default

	case "none":
		return sdktrace.NewSimpleSpanProcessor(noopExporter{}), nil

	case "stdout":
		return sdktrace.NewSimpleSpanProcessor(newConsoleExporter(os.Stderr)), nil

	default:
		return nil, fmt.Errorf("unknown trace_exporter %q (want stdout|otlp|none)", kind)
	}
}

type noopExporter struct{}

func (noopExporter) ExportSpans(_ context.Context, _ []sdktrace.ReadOnlySpan) error { return nil }
func (noopExporter) Shutdown(_ context.Context) error                               { return nil }
