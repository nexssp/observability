package llm

import (
	"context"
	"log/slog"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type LLMHookOptions struct {
	Metrics                *Metrics
	CostCalculator         CostCalculator
	DetailedCostCalculator DetailedCostCalculator
	Logger                 *slog.Logger
}

type LLMHook struct {
	opts LLMHookOptions
	log  *slog.Logger
}

func NewLLMHook(opts LLMHookOptions) *LLMHook {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &LLMHook{opts: opts, log: opts.Logger}
}

// AsAnyHook exposes the LLM telemetrist as an active Kernel action hook.
func (h *LLMHook) AsAnyHook() action.AnyHook {
	return action.AnyHook{
		Before: h.before,
		After:  h.after,
	}
}

// Emit implements kernel/observe.Sink so LLM telemetry can plug into observe.Hook directly.
func (h *LLMHook) Emit(ctx context.Context, event observe.Event) {
	if event.Response == nil {
		return
	}
	h.record(ctx, event.Response, event.Error, event.Action, event.Duration.Seconds())
}

func (h *LLMHook) before(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
	return context.WithValue(ctx, contextKeyStartTime{}, time.Now()), nil
}

func (h *LLMHook) after(ctx context.Context, _ any, res any, err error, meta *action.Meta) {
	actionName := ""
	if meta != nil {
		actionName = meta.Name
	}
	duration := float64(0)
	if start, ok := ctx.Value(contextKeyStartTime{}).(time.Time); ok {
		duration = time.Since(start).Seconds()
	}
	h.record(ctx, res, err, actionName, duration)
}

func (h *LLMHook) record(ctx context.Context, res any, err error, actionName string, durationSec float64) {
	provider, ok := res.(TokenCostProvider)
	if !ok {
		return
	}

	model := provider.Model()
	promptTokens := provider.PromptTokens()
	compTokens := provider.CompletionTokens()
	cachedTokens := 0

	if cp, hasCache := res.(CachedTokenCostProvider); hasCache {
		cachedTokens = cp.CachedPromptTokens()
	}

	tenant := xctx.TenantIDFrom(ctx)

	// Calculate cost
	cost := float64(0)
	hasCost := false
	if h.opts.DetailedCostCalculator != nil {
		cost, hasCost = h.opts.DetailedCostCalculator(model, promptTokens, cachedTokens, compTokens)
	} else if h.opts.CostCalculator != nil {
		cost, hasCost = h.opts.CostCalculator(model, promptTokens, compTokens)
	}

	// Prometheus Metrics
	if h.opts.Metrics != nil {
		h.opts.Metrics.PromptTokensTotal.WithLabelValues(model, actionName, tenant).Add(float64(promptTokens))
		if cachedTokens > 0 {
			h.opts.Metrics.CachedPromptTokensTotal.WithLabelValues(model, actionName, tenant).Add(float64(cachedTokens))
		}
		h.opts.Metrics.CompletionTokensTotal.WithLabelValues(model, actionName, tenant).Add(float64(compTokens))

		if hasCost {
			h.opts.Metrics.CostUSDTotal.WithLabelValues(model, actionName, tenant).Add(cost)
		}
		if durationSec > 0 {
			h.opts.Metrics.LLMDurationSeconds.WithLabelValues(model, actionName, tenant).Observe(durationSec)
		}
	}

	// OpenTelemetry Span Enrichment
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.SetAttributes(
			attribute.String("gen_ai.system", "llm"),
			attribute.String("gen_ai.model", model),
			attribute.Int("gen_ai.usage.input_tokens", promptTokens),
			attribute.Int("gen_ai.usage.cached_tokens", cachedTokens),
			attribute.Int("gen_ai.usage.output_tokens", compTokens),
		)
		if hasCost {
			span.SetAttributes(attribute.Float64("gen_ai.usage.cost_usd", cost))
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetStatus(codes.Ok, "")
		}
	}

	// Zero-allocation logging guard: skip slice allocation if logging is disabled
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}

	if h.log.Enabled(ctx, level) {
		attrs := []any{
			slog.String("model", model),
			slog.Int("prompt_tokens", promptTokens),
			slog.Int("cached_tokens", cachedTokens),
			slog.Int("completion_tokens", compTokens),
			slog.String("action", actionName),
			slog.String("tenant", tenant),
		}
		if hasCost {
			attrs = append(attrs, slog.Float64("cost_usd", cost))
		}
		if err != nil {
			attrs = append(attrs, slog.Any("error", err))
			h.log.ErrorContext(ctx, "LLM execution failed", attrs...)
		} else {
			h.log.InfoContext(ctx, "LLM execution completed", attrs...)
		}
	}
}

type contextKeyStartTime struct{}

var _ observe.Sink = (*LLMHook)(nil)
