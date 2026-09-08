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
	obsllm "github.com/nexssp/observability/llm"
)

type LLMResponse struct {
	model        string
	promptTokens int
	cachedTokens int
	compTokens   int
}

func (r LLMResponse) Model() string           { return r.model }
func (r LLMResponse) PromptTokens() int       { return r.promptTokens }
func (r LLMResponse) CachedPromptTokens() int { return r.cachedTokens }
func (r LLMResponse) CompletionTokens() int   { return r.compTokens }

func main() {
	ctx := context.Background()

	// 1. Initialize Observability provider from environment
	provider, shutdown, err := obs.Auto()
	if err != nil {
		log.Fatalf("failed to initialize observability: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	// 2. Real-world prompt cache pricing calculator (e.g. DeepSeek V3 / Claude 3.5)
	// Cache Hit: $0.014 / 1M | Cache Miss: $0.14 / 1M | Output: $0.28 / 1M
	costCalc := func(model string, prompt, cached, comp int) (float64, bool) {
		uncached := prompt - cached
		if uncached < 0 {
			uncached = 0
		}
		cost := (float64(cached)/1_000_000)*0.014 +
			(float64(uncached)/1_000_000)*0.14 +
			(float64(comp)/1_000_000)*0.28
		return cost, true
	}

	// 3. Register LLM Hook with Prometheus registry and OTel bridge
	llmHook := obsllm.NewLLMHook(obsllm.LLMHookOptions{
		Metrics:                provider.LLMMetrics(),
		DetailedCostCalculator: costCalc,
		Logger:                 provider.Logger(),
	})

	// 4. Build action with both standard OTel action hook and LLM cost telemetrist
	turn := 0
	llmAction := action.New("llm.generate", func(ctx context.Context, prompt string) (LLMResponse, error) {
		turn++
		time.Sleep(50 * time.Millisecond) // Simulated inference latency

		// Simulate prompt caching: subsequent conversational turns hit 80% cache
		totalPrompt := len(prompt) * 8
		cached := 0
		if turn > 1 {
			cached = int(float64(totalPrompt) * 0.80)
		}

		return LLMResponse{
			model:        "deepseek-v3",
			promptTokens: totalPrompt,
			cachedTokens: cached,
			compTokens:   120,
		}, nil
	}).
		Description("LLM generation with prompt cache and token cost telemetry").
		AnyHook(actionhook.New(provider)).
		AnyHook(llmHook.AsAnyHook()).
		Build()

	// 5. Expose production-hardened HTTP server for Prometheus scraping and health checks
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", provider.MetricsHandler())
	mux.Handle("GET /healthz", provider.HealthHandler())

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Println("📊 Metrics endpoint active at http://localhost:8080/metrics")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("metrics server failed: %v", err)
		}
	}()
	defer func() { _ = server.Shutdown(ctx) }()

	// 6. Execute multi-turn conversation
	fmt.Println("🚀 Executing LLM turns...")
	for i := 1; i <= 3; i++ {
		res, err := llmAction.Do(ctx, fmt.Sprintf("Turn %d: Explain zero-allocation telemetry architecture in Go.", i))
		if err != nil {
			log.Printf("action failed: %v", err)
			return
		}
		fmt.Printf("Turn %d [%s] -> Prompt: %d (Cached: %d) | Output: %d\n",
			i, res.Model(), res.PromptTokens(), res.CachedPromptTokens(), res.CompletionTokens())
	}

	fmt.Println("\n✅ LLM telemetry and prompt cache attribution successfully recorded.")
	fmt.Println("👉 Verify Prometheus metrics: curl -s http://localhost:8080/metrics | grep llm_")
}
