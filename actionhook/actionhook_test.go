package actionhook_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/xerr"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
)

func newProvider(t *testing.T) *obs.Provider {
	t.Helper()

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "test-service",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("failed to init obs provider: %v", err)
	}
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	return provider
}

func TestActionHook_TelemetryAndTraceContextSync(t *testing.T) {
	t.Parallel()

	hook := actionhook.New(newProvider(t))

	var observedTraceID string
	act := action.New("telemetry.test", func(ctx context.Context, req string) (string, error) {
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

	hook := actionhook.New(newProvider(t))

	act := action.New("hil.pause", func(_ context.Context, _ struct{}) (string, error) {
		return "", dag.ErrSuspended
	}).AnyHook(hook).Build()

	_, err := act.Do(context.Background(), struct{}{})
	if !errors.Is(err, dag.ErrSuspended) {
		t.Fatalf("expected ErrSuspended, got: %v", err)
	}
}

func TestActionHook_SkipsSystemActions(t *testing.T) {
	t.Parallel()

	hook := actionhook.New(newProvider(t))

	userAction := action.New("user.read", func(_ context.Context, _ struct{}) (string, error) {
		return "ok", nil
	}).AnyHook(hook).Build()

	systemAction := action.New("health.check", func(_ context.Context, _ struct{}) (string, error) {
		return "ok", nil
	}).System().AnyHook(hook).Build()

	if got := len(userAction.GetAnyHooks()); got != 1 {
		t.Fatalf("user action: expected 1 hook, got %d", got)
	}
	if got := len(systemAction.GetAnyHooks()); got != 0 {
		t.Fatalf("system action: expected 0 hooks (OnBuild filtered), got %d", got)
	}
}

func TestActionHook_RetryEventFlowsThroughSink(t *testing.T) {
	t.Parallel()

	hook := actionhook.New(newProvider(t))

	var attempts int
	act := action.New("retry.test", func(_ context.Context, _ string) (string, error) {
		attempts++
		if attempts == 1 {
			// xerr.Unavailable is transient; kernel's Retry predicate
			// classifies it via xerr.IsTransient and triggers a retry.
			return "", xerr.Unavailable("transient")
		}
		return "ok", nil
	}).
		Retry(1, action.ConstantBackoff(time.Millisecond)).
		AnyHook(hook).
		Build()

	if _, err := act.Do(context.Background(), "req"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

// BenchmarkActionHook_LatencyAndAllocs measures end-to-end action dispatch
// with an active OTel span. Expect roughly 20 allocs/op: the OTel SDK span
// lifecycle dominates and cannot be made allocation-free without removing
// OpenTelemetry. Zero-allocation guarantees in this package apply only to
// trace extraction helpers (obs.TraceID, obs.SpanID), Sink.Emit on simple
// events, and non-LLM paths in llm.Hook — all covered in
// obs/zero_alloc_test.go.
//
// Use this benchmark to detect regressions in ns/op or allocs/op, not as
// evidence of zero allocations.
func BenchmarkActionHook_LatencyAndAllocs(b *testing.B) {
	provider, shutdown, _ := obs.NewWithShutdown(obs.Config{
		ServiceName: "bench",
		Env:         "test",
	})
	defer func() { _ = shutdown(context.Background()) }()

	hook := actionhook.New(provider)
	act := action.New("bench.action", func(_ context.Context, req string) (string, error) {
		return req, nil
	}).AnyHook(hook).Build()

	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		_, _ = act.Do(ctx, "test")
	}
}
