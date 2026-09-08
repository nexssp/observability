// Package actionhook adapts *obs.Hook to the framework action system.
package actionhook

import (
	"context"
	"fmt"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	obs "github.com/nexssp/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// New constructs an action.AnyHook that instruments actions with OpenTelemetry.
func New(provider *obs.Provider) action.AnyHook {
	hook := provider.Hook()
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, meta *action.Meta) (context.Context, error) {
			if meta == nil {
				return ctx, nil
			}

			// Stack-allocated array (0 heap allocations on hot path)
			var stackAttrs [3]attribute.KeyValue
			n := 0

			if execID := action.ExecutionIDFrom(ctx); execID != "" {
				stackAttrs[n] = attribute.String("nexss.execution_id", execID)
				n++
			}
			if reqID := xctx.RequestIDFrom(ctx); reqID != "" {
				stackAttrs[n] = attribute.String("nexss.request_id", reqID)
				n++
			}
			if tenantID := xctx.TenantIDFrom(ctx); tenantID != "" {
				stackAttrs[n] = attribute.String("nexss.tenant_id", tenantID)
				n++
			}

			// Start OpenTelemetry span with stack attributes
			ctx = hook.BeforeWithAttributes(ctx, meta.Name, stackAttrs[:n]...)

			// Synchronize OTel Trace/Span IDs back into Kernel context
			span := trace.SpanFromContext(ctx)
			if sctx := span.SpanContext(); sctx.IsValid() {
				ctx = action.WithTraceContext(ctx, sctx.TraceID().String(), sctx.SpanID().String())
			}

			return ctx, nil
		},

		After: func(ctx context.Context, _ any, _ any, err error, _ *action.Meta) {
			hook.After(ctx, err)
		},

		OnRetry: func(ctx context.Context, _ any, attempt int, err error, _ *action.Meta) {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.AddEvent("action.retry", trace.WithAttributes(
					attribute.Int("attempt", attempt),
					attribute.String("error", fmt.Sprint(err)),
				))
			}
		},

		OnCacheHit: func(ctx context.Context, _ any, _ any, _ *action.Meta) {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.AddEvent("cache.hit")
				span.SetAttributes(attribute.Bool("cache.hit", true))
			}
		},

		OnCacheMiss: func(ctx context.Context, _ any, _ *action.Meta) {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.AddEvent("cache.miss")
				span.SetAttributes(attribute.Bool("cache.hit", false))
			}
		},

		OnDeduplicated: func(ctx context.Context, _ any, _ *action.Meta) {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.AddEvent("concurrency.deduplicated")
			}
		},

		OnCoalesced: func(ctx context.Context, _ any, _ *action.Meta) {
			if span := trace.SpanFromContext(ctx); span.IsRecording() {
				span.AddEvent("concurrency.coalesced")
			}
		},
	}
}
