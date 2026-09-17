// Package actionhook adapts *obs.Hook to the framework action system.
package actionhook

import (
	"context"
	"reflect"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	obs "github.com/nexssp/observability"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// New constructs an action.AnyHook that instruments actions with OpenTelemetry.
// The hook drops itself at build time for ScopeSystem actions (health,
// metrics, admin), so they do not contribute spans or Prometheus series.
func New(provider *obs.Provider) action.AnyHook {
	hook := provider.Hook()

	return action.AnyHook{
		OnBuild: func(meta *action.Meta, _, _ reflect.Type) bool {
			return meta != nil && !meta.IsSystem()
		},

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

			ctx = hook.BeforeWithAttributes(ctx, meta.Name, stackAttrs[:n]...)

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
			span := trace.SpanFromContext(ctx)
			if !span.IsRecording() {
				return
			}

			attrs := []attribute.KeyValue{attribute.Int("attempt", attempt)}
			if err != nil {
				attrs = append(attrs, attribute.String("error", err.Error()))
			}

			span.AddEvent("action.retry", trace.WithAttributes(attrs...))
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
