package obs_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/ai/dag"
	"github.com/nexssp/kernel/observe"
	obs "github.com/nexssp/observability"
)

func TestProvider_AsObserveSink(t *testing.T) {
	t.Parallel()

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "test-sink",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("failed to init provider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	kernelHook := observe.Hook(provider.Sink())

	act := action.New("test.observe.action", func(_ context.Context, n int) (int, error) {
		return n * 2, nil
	}).
		AnyHook(kernelHook).
		Build()

	res, err := act.Do(context.Background(), 21)
	if err != nil || res != 42 {
		t.Fatalf("expected 42, got %d (err: %v)", res, err)
	}
}

func TestProvider_Sink_HIL_Suspended(t *testing.T) {
	t.Parallel()

	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "test-hil-sink",
		Env:         "test",
	})
	if err != nil {
		t.Fatalf("failed to init provider: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	sink := provider.Sink()
	ctx := context.Background()

	sink.Emit(ctx, observe.Event{
		Kind:     observe.KindError,
		Action:   "workflow.node_approval",
		Duration: 15 * time.Millisecond,
		Error:    dag.ErrSuspended,
	})
}
