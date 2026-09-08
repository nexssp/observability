package llm_test

import (
	"context"
	"log/slog"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/observability/llm"
)

type MyResponse struct {
	model  string
	prompt int
	comp   int
}

func (r MyResponse) Model() string         { return r.model }
func (r MyResponse) PromptTokens() int     { return r.prompt }
func (r MyResponse) CompletionTokens() int { return r.comp }

func ExampleLLMHook() {
	// Suppose you have an LLM response type that implements TokenCostProvider.

	// Define cost calculator (e.g., using ai/llm.CalculateCost).
	costCalc := func(model string, prompt, comp int) (float64, bool) {
		// Example pricing: $0.01 per 1k tokens for both.
		return (float64(prompt) + float64(comp)) * 0.00001, true
	}

	// Create hook.
	hook := llm.NewLLMHook(llm.LLMHookOptions{
		CostCalculator: costCalc,
		Logger:         slog.Default(),
	})

	// Register hook with an action (pseudo-code).
	// act.AddAnyHook(hook.AsAnyHook())

	// Simulate execution.
	ctx := context.Background()
	meta := &action.Meta{Name: "my_action"}
	res := MyResponse{model: "gpt-4", prompt: 100, comp: 50}

	// The hook's After method would be called automatically by the kernel.
	hook.AsAnyHook().After(ctx, nil, res, nil, meta)
}
