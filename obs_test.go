package obs_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	obs "github.com/nexssp/observability"
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

func TestObsProviderHealthCheckErrorRedaction(t *testing.T) {
	t.Parallel()

	const underlyingError = "sensitive database connection details"

	tests := []struct {
		name                  string
		redact                bool
		wantCheckValue        string
		wantUnderlyingDetails bool
	}{
		{
			name:                  "default preserves underlying error",
			wantCheckValue:        underlyingError,
			wantUnderlyingDetails: true,
		},
		{
			name:           "opt-in redacts underlying error",
			redact:         true,
			wantCheckValue: "failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := obs.Config{ServiceName: "test-service", Env: "test"}
			obs.WithRedactHealthCheckErrors(tt.redact)(&cfg)

			provider, shutdown, err := obs.NewWithShutdown(cfg)
			if err != nil {
				t.Fatalf("failed to create obs provider: %v", err)
			}
			defer func() { _ = shutdown(context.Background()) }()

			provider.RegisterCheck("database", func(context.Context) error {
				return errors.New(underlyingError)
			})

			w := httptest.NewRecorder()
			provider.HealthHandler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody))

			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected %d for a failed check, got %d", http.StatusServiceUnavailable, w.Code)
			}

			var payload struct {
				Status string            `json:"status"`
				Checks map[string]string `json:"checks"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
				t.Fatalf("failed to decode health response: %v", err)
			}
			if payload.Status != http.StatusText(http.StatusServiceUnavailable) {
				t.Fatalf("expected JSON status %q, got %q", http.StatusText(http.StatusServiceUnavailable), payload.Status)
			}
			if len(payload.Checks) != 1 {
				t.Fatalf("expected one check result, got %#v", payload.Checks)
			}
			if got := payload.Checks["database"]; got != tt.wantCheckValue {
				t.Fatalf("expected database result %q, got %q", tt.wantCheckValue, got)
			}
			if got := w.Body.String(); strings.Contains(got, underlyingError) != tt.wantUnderlyingDetails {
				t.Fatalf("unexpected underlying error visibility in response: %s", got)
			}
		})
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

	provider.RegisterCheck("db_check", func(_ context.Context) error {
		return nil
	})

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/healthz", http.NoBody)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	w := httptest.NewRecorder()
	provider.HealthHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for health handler, got %d", w.Code)
	}

	if provider.Logger() == nil {
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

	if provider.MetricsHandler() == nil {
		t.Fatal("expected non-nil metrics handler for custom registry")
	}

	if !provider.Handler().Enabled(context.Background(), slog.LevelWarn) {
		t.Error("expected logger handler level settings to be preserved")
	}
}
