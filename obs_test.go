package obs_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	obs "github.com/nexssp/observability"
	"github.com/prometheus/client_golang/prometheus"
)

func TestObsProviderHealthCheckTimeout(t *testing.T) {
	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName:        "test-service",
		Env:                "test",
		HealthCheckTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("failed to create obs provider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	provider.RegisterCheck("blocked_dependency", func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	started := time.Now()
	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	provider.HealthHandler().ServeHTTP(w, req)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("health handler exceeded bounded check timeout: %v", elapsed)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for timed out dependency, got %d", w.Code)
	}
}

func TestObsProvider_HealthAndContextHandler(t *testing.T) {
	t.Parallel()

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "test-service",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("failed to create obs provider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	// 1. Register Health Check
	provider.RegisterCheck("db_check", func(ctx context.Context) error {
		return nil
	})

	// 2. Test Health Handler
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	w := httptest.NewRecorder()
	provider.HealthHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for health handler, got %d", w.Code)
	}

	// 3. Test Context Logger
	logger := provider.Logger()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestObsProvider_AdvancedConfigOptions(t *testing.T) {
	t.Parallel()

	customRegistry := prometheus.NewRegistry()
	customHandler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn})

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName:        "advanced-service",
		Env:                "production",
		PrometheusRegistry: customRegistry,
		MetricsPrefix:      "my_enterprise_app",
		LoggerHandler:      customHandler,
	})
	if err != nil {
		t.Fatalf("failed to create obs provider with advanced config: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	// Verify custom registry was successfully integrated
	if provider.MetricsHandler() == nil {
		t.Fatal("expected non-nil metrics handler for custom registry")
	}

	// Context logger must wrap and inherit log levels from customHandler
	if !provider.Handler().Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected logger handler level settings to be preserved")
	}
}
