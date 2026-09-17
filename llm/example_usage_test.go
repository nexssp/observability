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

func ExampleHook() {
	costCalc := func(_ string, prompt, comp int) (float64, bool) {
		// Example pricing: $0.01 per 1k tokens for both.
		return (float64(prompt) + float64(comp)) * 0.00001, true
	}

	hook := llm.NewLLMHook(llm.HookOptions{
		CostCalculator: costCalc,
		Logger:         slog.Default(),
	})

	ctx := context.Background()
	meta := &action.Meta{Name: "my_action"}
	res := MyResponse{model: "gpt-4", prompt: 100, comp: 50}

	hook.AsAnyHook().After(ctx, nil, res, nil, meta)
}
