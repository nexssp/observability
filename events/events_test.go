package events_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/nexssp/observability/events"
)

func TestEvents_Emit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	ctx := context.Background()

	events.Emit(ctx, logger, events.BusinessEvent{
		Type:     "order.created",
		Action:   "order.create",
		EntityID: "ord_100",
		Tags:     map[string]string{"env": "test"},
		Payload:  map[string]any{"total": 99.95},
	})

	output := buf.String()
	if !strings.Contains(output, "business_event") || !strings.Contains(output, "order.created") || !strings.Contains(output, "ord_100") {
		t.Fatalf("expected structured event log, got: %s", output)
	}
}

func TestEvents_EmitProducesSpan(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = tp.Shutdown(context.Background())
	})

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	events.Emit(context.Background(), logger, events.BusinessEvent{
		Type:     "order.created",
		Action:   "order.create",
		EntityID: "ord_100",
	})

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("exported spans = %d, want 1", len(spans))
	}
	if spans[0].Name != "event.order.created" {
		t.Fatalf("span name = %q, want %q", spans[0].Name, "event.order.created")
	}
}
