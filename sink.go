package obs

import (
	"context"
	"errors"
	"fmt"

	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/observe"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Sink struct {
	provider *Provider
}

func NewSink(provider *Provider) *Sink {
	return &Sink{provider: provider}
}

func (s *Sink) Emit(ctx context.Context, event observe.Event) {
	if s == nil || s.provider == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	s.dispatchSpanEvent(span, event)

	switch event.Kind {
	case observe.KindSuccess:
		s.emitExecuted(ctx, event)
	case observe.KindError:
		s.emitError(ctx, event, span)
	}
}

func (s *Sink) dispatchSpanEvent(span trace.Span, event observe.Event) {
	if !span.IsRecording() {
		return
	}

	switch event.Kind {
	case observe.KindTimeout:
		span.SetStatus(codes.Error, "operation timeout")
		span.AddEvent("action.timeout")
	case observe.KindCanceled:
		span.SetStatus(codes.Error, "operation canceled")
		span.AddEvent("action.canceled")
	case observe.KindRetry:
		s.emitRetry(span, event)
	case observe.KindCacheHit:
		addActionEvent(span, "cache.hit", event.Action)
	case observe.KindCacheMiss:
		addActionEvent(span, "cache.miss", event.Action)
	case observe.KindDeduplicated:
		addActionEvent(span, "concurrency.deduplicated", event.Action)
	case observe.KindCoalesced:
		addActionEvent(span, "concurrency.coalesced", event.Action)
	case observe.KindPanic:
		span.AddEvent("action.panic", trace.WithAttributes(
			attribute.String("action", event.Action),
			attribute.String("recovered", fmt.Sprint(event.Recovered)),
		))
	}
}

// RecordStream enriches the active OpenTelemetry span with stream execution metrics.
func (s *Sink) RecordStream(ctx context.Context, stats StreamStats) {
	if s == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.SetAttributes(
		attribute.String("stream.name", stats.Stream.Name),
		attribute.Int64("stream.items.delivered", stats.Items.Delivered),
		attribute.Int64("stream.items.emitted", stats.Items.Emitted),
		attribute.Int64("stream.items.dropped", stats.Items.Dropped),
		attribute.Int64("stream.items.errors", stats.Items.Errors),
		attribute.String("stream.status", string(stats.Exec.Status)),
		attribute.Int64("stream.duration_ms", stats.Exec.Duration.Milliseconds()),
	)

	if stats.Bytes.Delivered > 0 {
		span.SetAttributes(attribute.Int64("stream.bytes.delivered", stats.Bytes.Delivered))
	}
	if stats.Error != nil {
		span.SetAttributes(
			attribute.String("stream.error.kind", stats.Error.Kind),
			attribute.String("stream.error.message", stats.Error.Message),
		)
	}
}

func (s *Sink) emitExecuted(ctx context.Context, event observe.Event) {
	if s.provider.latencyHisto != nil && event.Duration > 0 {
		s.provider.latencyHisto.Record(ctx, float64(event.Duration.Milliseconds()))
	}
}

func (s *Sink) emitError(ctx context.Context, event observe.Event, span trace.Span) {
	if errors.Is(event.Error, dag.ErrSuspended) {
		if s.provider.suspendedCounter != nil {
			s.provider.suspendedCounter.Add(ctx, 1)
		}
		if span.IsRecording() {
			span.AddEvent("workflow.suspended", trace.WithAttributes(
				attribute.String("action", event.Action),
				attribute.String("reason", "waiting for human approval"),
			))
		}
		return
	}

	if s.provider.errorCounter != nil {
		s.provider.errorCounter.Add(ctx, 1)
	}
}

func (s *Sink) emitRetry(span trace.Span, event observe.Event) {
	attrs := []attribute.KeyValue{
		attribute.String("action", event.Action),
		attribute.Int("attempt", event.Attempt),
	}
	if event.Error != nil {
		attrs = append(attrs, attribute.String("error", event.Error.Error()))
	}

	span.AddEvent("action.retry", trace.WithAttributes(attrs...))
}

func addActionEvent(span trace.Span, name, action string) {
	span.AddEvent(name, trace.WithAttributes(attribute.String("action", action)))
}

var _ observe.Sink = (*Sink)(nil)
