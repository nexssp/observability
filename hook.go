// Package obs provides tracing hooks for framework action execution.
package obs

import (
	"context"
	"errors"
	"time"

	"github.com/nexssp/kernel/ai/dag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Hook instruments action executions with OpenTelemetry spans and metrics.
type Hook struct {
	provider *Provider
	tracer   trace.Tracer
}

// Hook initializes an execution Hook from the Provider.
func (p *Provider) Hook() *Hook {
	return &Hook{
		provider: p,
		tracer:   otel.Tracer("nexss/obs"),
	}
}

// BeforeWithAttributes starts an OTel span with stack-allocated attributes (0 heap allocations on hot path).
func (h *Hook) BeforeWithAttributes(ctx context.Context, actionName string, extraAttrs ...attribute.KeyValue) context.Context {
	attrs := make([]attribute.KeyValue, 0, 1+len(extraAttrs))
	attrs = append(attrs, attribute.String("nexss.action", actionName))
	attrs = append(attrs, extraAttrs...)

	ctx, span := h.tracer.Start(ctx, "action."+actionName,
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	state := &obsState{
		action:  actionName,
		startMs: time.Now().UnixMilli(),
	}
	ctx = withObsState(ctx, state)
	_ = span
	return ctx
}

// Before is preserved for backward compatibility with map-based callers.
func (h *Hook) Before(ctx context.Context, actionName string, meta map[string]string) context.Context {
	if len(meta) == 0 {
		return h.BeforeWithAttributes(ctx, actionName)
	}
	attrs := make([]attribute.KeyValue, 0, len(meta))
	for k, v := range meta {
		if k != "" {
			attrs = append(attrs, attribute.String("nexss.meta."+k, v))
		}
	}
	return h.BeforeWithAttributes(ctx, actionName, attrs...)
}

// After ends the OpenTelemetry span and records latency/error/suspension metrics.
func (h *Hook) After(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	state := getObsState(ctx)

	duration := float64(0)
	if state != nil {
		duration = float64(time.Now().UnixMilli() - state.startMs)
	}

	switch {
	// HIL SUSPENSION: A human approval pause is not an application failure.
	case errors.Is(err, dag.ErrSuspended):
		span.SetStatus(codes.Ok, "suspended for human intervention")
		span.SetAttributes(
			attribute.Bool("nexss.suspended", true),
			attribute.String("nexss.status", "suspended"),
		)
		span.AddEvent("workflow.suspended", trace.WithAttributes(
			attribute.String("reason", "waiting for human approval"),
		))
		if h.provider.suspendedCounter != nil {
			h.provider.suspendedCounter.Add(ctx, 1)
		}

	case err != nil:
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		span.SetAttributes(attribute.String("nexss.status", "error"))
		if h.provider.errorCounter != nil {
			h.provider.errorCounter.Add(ctx, 1)
		}

	default:
		span.SetStatus(codes.Ok, "")
		span.SetAttributes(attribute.String("nexss.status", "ok"))
	}

	span.SetAttributes(attribute.Float64("nexss.duration_ms", duration))

	if h.provider.latencyHisto != nil && duration > 0 {
		h.provider.latencyHisto.Record(ctx, duration)
	}

	span.End()
}
