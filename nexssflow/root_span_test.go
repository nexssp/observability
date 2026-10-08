package nexssflow

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	obs "github.com/nexssp/observability"
)

func TestWrapRootSpan_PassesThroughExistingSpan(t *testing.T) {
	var traceID trace.TraceID
	traceID[0] = 1
	var spanID trace.SpanID
	spanID[0] = 2
	outerSpanContext := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	if !outerSpanContext.IsValid() {
		t.Fatal("test span context is invalid")
	}

	inner := action.New[any, trace.SpanContext]("test.inner", func(ctx context.Context, _ any) (trace.SpanContext, error) {
		return trace.SpanFromContext(ctx).SpanContext(), nil
	}).Build()
	wrapped := wrapRootSpan(inner, noop.NewTracerProvider().Tracer("test"), "test-service", "test")

	ctx := trace.ContextWithSpanContext(context.Background(), outerSpanContext)
	result, err := action.InvokeAny(ctx, wrapped, nil)
	if err != nil {
		t.Fatalf("invoke root-span wrapper: %v", err)
	}
	got, ok := result.(trace.SpanContext)
	if !ok {
		t.Fatalf("inner action result type = %T, want trace.SpanContext", result)
	}
	if !got.Equal(outerSpanContext) {
		t.Fatalf("inner action received span context %v, want existing outer span %v", got, outerSpanContext)
	}
}

func TestBundleRootSpanUsesBundleProvider(t *testing.T) {
	previousTracerProvider := otel.GetTracerProvider()
	previousMeterProvider := otel.GetMeterProvider()
	previousPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(previousTracerProvider)
		otel.SetMeterProvider(previousMeterProvider)
		otel.SetTextMapPropagator(previousPropagator)
	})

	bundleExporter := tracetest.NewInMemoryExporter()
	bundleProvider, bundleShutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName:    "bundle-provider",
		TraceProcessor: sdktrace.NewSimpleSpanProcessor(bundleExporter),
	})
	if err != nil {
		t.Fatalf("create bundle provider: %v", err)
	}
	t.Cleanup(func() {
		if err := bundleShutdown(context.Background()); err != nil {
			t.Errorf("shutdown bundle provider: %v", err)
		}
	})

	globalExporter := tracetest.NewInMemoryExporter()
	_, globalShutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName:    "global-provider",
		TraceProcessor: sdktrace.NewSimpleSpanProcessor(globalExporter),
	})
	if err != nil {
		t.Fatalf("create distinct global provider: %v", err)
	}
	t.Cleanup(func() {
		if err := globalShutdown(context.Background()); err != nil {
			t.Errorf("shutdown global provider: %v", err)
		}
	})

	bundle, err := NewBundle(bundleProvider, bundleShutdown)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	inner := action.New[any, any]("test.inner", func(context.Context, any) (any, error) {
		return nil, nil
	}).Build()
	wrapped, err := bundle.WrapPipeline(nil, inner)
	if err != nil {
		t.Fatalf("wrap pipeline: %v", err)
	}
	if _, err := action.InvokeAny(context.Background(), wrapped, nil); err != nil {
		t.Fatalf("invoke wrapped pipeline: %v", err)
	}

	bundleSpans := bundleExporter.GetSpans()
	if len(bundleSpans) != 1 || bundleSpans[0].Name != "flow.run" {
		t.Fatalf("bundle provider spans = %#v, want one flow.run span", bundleSpans)
	}
	if globalSpans := globalExporter.GetSpans(); len(globalSpans) != 0 {
		t.Fatalf("distinct global provider received root spans: %#v", globalSpans)
	}
}
