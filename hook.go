// Package obs provides tracing hooks for framework action execution.
package obs

import (
	"context"
	"time"

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

// Before starts an OpenTelemetry span before action invocation.
func (h *Hook) Before(ctx context.Context, action string, meta map[string]string) context.Context {
	attributes := make([]attribute.KeyValue, 0, 1+len(meta))
	attributes = append(attributes, attribute.String("nexss.action", action))

	for key, value := range meta {
		if key == "" {
			continue
		}
		attributes = append(attributes, attribute.String("nexss.meta."+key, value))
	}

	ctx, span := h.tracer.Start(ctx, "action."+action,
		trace.WithAttributes(attributes...),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	state := &obsState{
		action:  action,
		startMs: time.Now().UnixMilli(),
		attrs:   meta,
	}
	ctx = withObsState(ctx, state)
	_ = span
	return ctx
}

// After ends the OpenTelemetry span and records latency/error metrics.
func (h *Hook) After(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	state := getObsState(ctx)

	duration := float64(0)
	if state != nil {
		duration = float64(time.Now().UnixMilli() - state.startMs)
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		if h.provider.errorCounter != nil {
			h.provider.errorCounter.Add(ctx, 1)
		}
	} else {
		span.SetStatus(codes.Ok, "")
	}

	span.SetAttributes(attribute.Float64("nexss.duration_ms", duration))

	if h.provider.latencyHisto != nil && duration > 0 {
		h.provider.latencyHisto.Record(ctx, duration)
	}

	span.End()
}
