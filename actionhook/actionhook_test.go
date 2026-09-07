package actionhook_test

import (
	"context"
	"testing"

	"github.com/nexssp/kernel/action"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
)

func TestActionHook_Telemetry(t *testing.T) {
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

	act := action.New("telemetry.test", func(ctx context.Context, req string) (string, error) {
		return "hello " + req, nil
	}).AnyHook(hook).Build()

	res, err := act.Do(context.Background(), "world")
	if err != nil || res != "hello world" {
		t.Fatalf("action execution failed: %v", err)
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
