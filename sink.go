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
		if s.provider.latencyHisto != nil && event.Duration > 0 {
			s.provider.latencyHisto.Record(ctx, float64(event.Duration.Milliseconds()))
		}

	case observe.KindError:
		// ⚡ HIL SUSPENSION: Count pauses separately from application errors
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
		} else if s.provider.errorCounter != nil {
			if s.provider.errorCounter != nil {
				s.provider.errorCounter.Add(ctx, 1)
			}
		}

	case observe.KindRetry:
		if span.IsRecording() {
			span.AddEvent("action.retry", trace.WithAttributes(
				attribute.String("action", event.Action),
				attribute.Int("attempt", event.Attempt),
				attribute.String("error", fmt.Sprint(event.Error)),
			))
		}

	case observe.KindCacheHit:
		if span.IsRecording() {
			span.AddEvent("cache.hit", trace.WithAttributes(
				attribute.String("action", event.Action),
			))
		}

	case observe.KindCacheMiss:
		if span.IsRecording() {
			span.AddEvent("cache.miss", trace.WithAttributes(
				attribute.String("action", event.Action),
			))
		}

	case observe.KindDeduplicated:
		if span.IsRecording() {
			span.AddEvent("concurrency.deduplicated", trace.WithAttributes(
				attribute.String("action", event.Action),
			))
		}

	case observe.KindCoalesced:
		if span.IsRecording() {
			span.AddEvent("concurrency.coalesced", trace.WithAttributes(
				attribute.String("action", event.Action),
			))
		}

	case observe.KindPanic:
		if span.IsRecording() {
			span.AddEvent("action.panic", trace.WithAttributes(
				attribute.String("action", event.Action),
				attribute.String("recovered", fmt.Sprint(event.Recovered)),
			))
		}
	}
}

var _ observe.Sink = (*Sink)(nil)
