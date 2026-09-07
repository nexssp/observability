// Package obs provides interfaces for observability components.
package obs

import (
	"context"
	"net/http"
)

// HealthChecker defines a component that can be polled for readiness.
type HealthChecker interface {
	Check(ctx context.Context) error
}

// HealthRegistry allows dynamic registration of system readiness probes.
type HealthRegistry interface {
	RegisterCheck(name string, check func(ctx context.Context) error)
	HealthHandler() http.Handler
}

// Tracer defines custom span generation wrappers.
type Tracer interface {
	Span(ctx context.Context, name string) (context.Context, func(error))
}
