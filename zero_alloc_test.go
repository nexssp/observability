package obs_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/observe"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/llm"
)

func TestZeroAlloc_TraceContextExtraction(t *testing.T) {
	ctx := context.Background()

	allocs := testing.AllocsPerRun(1000, func() {
		_ = obs.TraceID(ctx)
		_ = obs.SpanID(ctx)
	})

	if allocs != 0 {
		t.Fatalf("expected 0 allocations for TraceID/SpanID extraction, got %.1f", allocs)
	}
}

func TestZeroAlloc_SinkLifecycleEmission(t *testing.T) {
	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName: "zero-alloc-service",
		Env:         "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	sink := provider.Sink()
	ctx := context.Background()

	cacheHitEvent := observe.Event{
		Kind:     observe.KindCacheHit,
		Action:   "order.get",
		Duration: 5 * time.Millisecond,
	}

	allocs := testing.AllocsPerRun(1000, func() {
		sink.Emit(ctx, cacheHitEvent)
	})

	if allocs != 0 {
		t.Fatalf("expected 0 allocations for Sink.Emit (cache_hit), got %.1f", allocs)
	}
}

func TestZeroAlloc_LLMHookFiltering(t *testing.T) {
	hook := llm.NewLLMHook(llm.HookOptions{})
	ctx := context.Background()

	nonLLMEvent := observe.Event{
		Kind:     observe.KindExecuted,
		Action:   "standard.db_query",
		Duration: 10 * time.Millisecond,
		Response: struct{ Status string }{Status: "ok"},
	}

	allocs := testing.AllocsPerRun(1000, func() {
		hook.Emit(ctx, nonLLMEvent)
	})

	if allocs != 0 {
		t.Fatalf("expected 0 allocations for non-LLM response evaluation, got %.1f", allocs)
	}
}

func TestZeroAlloc_VisualTraceCollectorRingBuffer(t *testing.T) {
	ctx, col := obs.WithCollector(context.Background())

	for range 64 {
		_, end := obs.StartSpan(ctx, "warmup")
		end()
	}

	initialCap := 64
	for range 100 {
		_, end := obs.StartSpan(ctx, "sub.operation")
		end()
	}

	spans := col.Spans()
	if len(spans) != initialCap {
		t.Fatalf("circular buffer failed: expected bounded capacity %d, got %d", initialCap, len(spans))
	}
}
