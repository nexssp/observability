package llm_test

import (
	"context"
	"testing"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/observability/llm"
	"github.com/prometheus/client_golang/prometheus"
)

type testResponse struct {
	model        string
	promptTokens int
	cachedTokens int
	compTokens   int
}

func (r testResponse) Model() string           { return r.model }
func (r testResponse) PromptTokens() int       { return r.promptTokens }
func (r testResponse) CachedPromptTokens() int { return r.cachedTokens }
func (r testResponse) CompletionTokens() int   { return r.compTokens }

func TestLLMHook_DetailedCostCalculation(t *testing.T) {
	t.Parallel()

	reg := prometheus.NewRegistry()
	metrics := llm.NewMetrics(reg, "test", "ai")

	costCalculated := false
	costCalc := func(_ string, _, cached, _ int) (float64, bool) {
		costCalculated = true
		if cached != 80 {
			t.Errorf("expected 80 cached tokens, got %d", cached)
		}

		return 0.0042, true
	}

	hook := llm.NewLLMHook(llm.HookOptions{
		Metrics:                metrics,
		DetailedCostCalculator: costCalc,
	})

	act := action.New("llm.inference", func(_ context.Context, _ string) (testResponse, error) {
		return testResponse{
			model:        "claude-3-5-sonnet",
			promptTokens: 100,
			cachedTokens: 80,
			compTokens:   50,
		}, nil
	}).AnyHook(hook.AsAnyHook()).Build()

	res, err := act.Do(context.Background(), "test prompt")
	if err != nil {
		t.Fatalf("action failed: %v", err)
	}

	if !costCalculated {
		t.Fatal("expected DetailedCostCalculator to be executed")
	}
	if res.Model() != "claude-3-5-sonnet" {
		t.Fatalf("unexpected model: %s", res.Model())
	}
}

func TestLLMHook_AsObserveSink(t *testing.T) {
	t.Parallel()

	costHit := false
	hook := llm.NewLLMHook(llm.HookOptions{
		CostCalculator: func(_ string, _, _ int) (float64, bool) {
			costHit = true

			return 0.001, true
		},
	})

	sink := hook
	sink.Emit(context.Background(), observe.Event{
		Kind:     observe.KindExecuted,
		Action:   "llm.direct_sink",
		Duration: 100 * time.Millisecond,
		Response: testResponse{
			model:        "deepseek-v3",
			promptTokens: 200,
			compTokens:   80,
		},
	})

	if !costHit {
		t.Fatal("expected Hook.Emit to process TokenCostProvider from observe.Event")
	}
}
