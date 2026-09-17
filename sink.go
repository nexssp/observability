package obs

import (
	"context"
	"errors"
	"fmt"

	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/observe"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Sink bridges kernel/observe.Event lifecycle events to OpenTelemetry and Prometheus.
type Sink struct {
	provider *Provider
}

// NewSink creates an observe.Sink backed by Provider.
func NewSink(provider *Provider) *Sink {
	return &Sink{provider: provider}
}

// Emit processes lifecycle events from kernel/observe.
func (s *Sink) Emit(ctx context.Context, event observe.Event) {
	if s == nil || s.provider == nil {
		return
	}

	span := trace.SpanFromContext(ctx)

	switch event.Kind {
	case observe.KindExecuted:
		s.emitExecuted(ctx, event)
	case observe.KindError:
		s.emitError(ctx, event, span)
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
		if span.IsRecording() {
			span.AddEvent("action.panic", trace.WithAttributes(
				attribute.String("action", event.Action),
				attribute.String("recovered", fmt.Sprint(event.Recovered)),
			))
		}
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
	if !span.IsRecording() {
		return
	}

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
	if !span.IsRecording() {
		return
	}

	span.AddEvent(name, trace.WithAttributes(attribute.String("action", action)))
}

var _ observe.Sink = (*Sink)(nil)
