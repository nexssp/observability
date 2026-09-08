// Package llm provides OpenTelemetry and Prometheus instrumentation for LLM calls.
package llm

// TokenCostProvider is implemented by standard LLM responses.
type TokenCostProvider interface {
	Model() string
	PromptTokens() int
	CompletionTokens() int
}

// CachedTokenCostProvider is implemented by modern LLM responses supporting prompt caching (DeepSeek V3, Claude 3.5, GPT-4o).
type CachedTokenCostProvider interface {
	TokenCostProvider
	CachedPromptTokens() int
}

// CostCalculator calculates basic cost in USD.
type CostCalculator func(model string, promptTokens, compTokens int) (float64, bool)

// DetailedCostCalculator calculates exact cost factoring in prompt cache discounts.
type DetailedCostCalculator func(model string, promptTokens, cachedTokens, compTokens int) (float64, bool)
