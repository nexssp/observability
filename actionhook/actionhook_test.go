package actionhook_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
)

func TestActionHook_TelemetryAndTraceContextSync(t *testing.T) {
	t.Parallel()

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "test-service",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("failed to init obs provider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	hook := actionhook.New(provider)

	var observedTraceID string
	act := action.New("telemetry.test", func(ctx context.Context, req string) (string, error) {
		// ⚡ Kernel's action.TraceIDFrom must return the OTel trace ID automatically
		observedTraceID = action.TraceIDFrom(ctx)
		return "hello " + req, nil
	}).AnyHook(hook).Build()

	res, err := act.Do(context.Background(), "world")
	if err != nil || res != "hello world" {
		t.Fatalf("action execution failed: %v", err)
	}

	if observedTraceID == "" {
		t.Fatal("expected action.TraceIDFrom(ctx) to be populated from OTel span, got empty string")
	}
}

func TestActionHook_HIL_SuspendedIsNotError(t *testing.T) {
	t.Parallel()

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "test-service",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("failed to init obs provider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	hook := actionhook.New(provider)

	act := action.New("hil.pause", func(ctx context.Context, _ struct{}) (string, error) {
		return "", dag.ErrSuspended
	}).AnyHook(hook).Build()

	_, err = act.Do(context.Background(), struct{}{})
	// ⚡ Use errors.Is to unwrap the error returned by action.Do
	if !errors.Is(err, dag.ErrSuspended) {
		t.Fatalf("expected ErrSuspended, got: %v", err)
	}
}

func BenchmarkActionHook_ZeroAlloc(b *testing.B) {
	provider, shutdown, _ := obs.NewWithShutdown(obs.Config{
		ServiceName: "bench",
		Env:         "test",
	})
	defer shutdown(context.Background())

	hook := actionhook.New(provider)
	act := action.New("bench.action", func(ctx context.Context, req string) (string, error) {
		return req, nil
	}).AnyHook(hook).Build()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _ = act.Do(context.Background(), "test")
	}
}
