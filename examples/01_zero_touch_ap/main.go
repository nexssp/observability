package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/nexssp/kernel/action"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
)

func main() {
	// 1. Initialize observability (Reads environment parameters automatically)
	provider, shutdown, err := obs.Auto()
	if err != nil {
		log.Fatalf("failed to initialize observability: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	// 2. Build the global AnyHook adapter
	telemetryHook := actionhook.New(provider)

	// 3. Define a normal business action with no external metrics/tracing dependencies
	createUser := action.New("user.create", func(ctx context.Context, email string) (string, error) {
		provider.Logger().InfoContext(ctx, "processing database insertion", "email", email)
		time.Sleep(30 * time.Millisecond) // Simulate work
		return "usr_id_" + email, nil
	}).
		AnyHook(telemetryHook). // 👈 Wire the telemetry interceptor
		Build()

	// 4. Setup infrastructure multiplexer
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", provider.MetricsHandler())
	mux.Handle("GET /healthz", provider.HealthHandler())

	// Standard liveness check registration
	provider.RegisterCheck("system_memory", func(ctx context.Context) error {
		return nil
	})

	mux.HandleFunc("POST /users", func(w http.ResponseWriter, r *http.Request) {
		res, err := createUser.Do(r.Context(), r.FormValue("email"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, res) //nolint:gosec // safe because Content-Type is text/plain
	})

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	provider.Logger().Info("Server starting on :8080")
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err) //nolint:gocritic // exit after defer is acceptable here
	}
}
