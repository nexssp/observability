// Package actionhook adapts *obs.Hook to the framework action system.
package actionhook

import (
	"context"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	obs "github.com/nexssp/observability"
)

// New constructs an action.AnyHook that instruments actions with OpenTelemetry.
func New(provider *obs.Provider) action.AnyHook {
	hook := provider.Hook()
	return action.AnyHook{
		Before: func(ctx context.Context, _ any, meta *action.Meta) (context.Context, error) {
			if meta == nil {
				return ctx, nil
			}

			// Automatically gather context metadata from Nexss Kernel
			attrs := map[string]string{
				"nexss.execution_id": action.ExecutionIDFrom(ctx),
			}
			if reqID := xctx.RequestIDFrom(ctx); reqID != "" {
				attrs["nexss.request_id"] = reqID
			}
			if tenantID := xctx.TenantIDFrom(ctx); tenantID != "" {
				attrs["nexss.tenant_id"] = tenantID
			}

			return hook.Before(ctx, meta.Name, attrs), nil
		},
		After: func(ctx context.Context, _ any, _ any, err error, _ *action.Meta) {
			hook.After(ctx, err)
		},
	}
}
