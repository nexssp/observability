package llm

import (
	"context"
	"log/slog"
	"reflect"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/observe"
	"github.com/nexssp/kernel/xctx"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type HookOptions struct {
	Metrics                *Metrics
	CostCalculator         CostCalculator
	DetailedCostCalculator DetailedCostCalculator
	Logger                 *slog.Logger
}

type Hook struct {
	opts HookOptions
	log  *slog.Logger
}

type usage struct {
	model        string
	promptTokens int
	cachedTokens int
	compTokens   int
	costUSD      float64
	hasCost      bool
}

func NewLLMHook(opts HookOptions) *Hook {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}

	return &Hook{opts: opts, log: opts.Logger}
}

// AsAnyHook exposes the LLM telemetrist as an active Kernel action hook.
// The hook drops itself at build time for actions whose response type does
// not implement TokenCostProvider, so non-LLM actions pay zero cost.
func (h *Hook) AsAnyHook() action.AnyHook {
	return action.AnyHook{
		OnBuild: func(_ *action.Meta, _, resType reflect.Type) bool {
			return resType.Implements(reflect.TypeFor[TokenCostProvider]())
		},
		Before: h.before,
		After:  h.after,
	}
}

// Emit implements kernel/observe.Sink so LLM telemetry can plug into observe.Hook directly.
func (h *Hook) Emit(ctx context.Context, event observe.Event) {
	if event.Response == nil {
		return
	}

	h.record(ctx, event.Response, event.Error, event.Action, event.Duration.Seconds())
}

func (h *Hook) before(ctx context.Context, _ any, _ *action.Meta) (context.Context, error) {
	return context.WithValue(ctx, contextKeyStartTime{}, time.Now()), nil
}

func (h *Hook) after(ctx context.Context, _, res any, err error, meta *action.Meta) {
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

func (h *Hook) record(ctx context.Context, res any, err error, actionName string, durationSec float64) {
	provider, ok := res.(TokenCostProvider)
	if !ok {
		return
	}

	u := h.extractUsage(provider, res)
	h.recordMetrics(ctx, u, actionName, durationSec)
	h.enrichSpan(ctx, u, err)
	h.logUsage(ctx, u, actionName, err)
}

func (h *Hook) extractUsage(provider TokenCostProvider, res any) usage {
	u := usage{
		model:        provider.Model(),
		promptTokens: provider.PromptTokens(),
		compTokens:   provider.CompletionTokens(),
	}

	if cp, hasCache := res.(CachedTokenCostProvider); hasCache {
		u.cachedTokens = cp.CachedPromptTokens()
	}

	switch {
	case h.opts.DetailedCostCalculator != nil:
		u.costUSD, u.hasCost = h.opts.DetailedCostCalculator(u.model, u.promptTokens, u.cachedTokens, u.compTokens)
	case h.opts.CostCalculator != nil:
		u.costUSD, u.hasCost = h.opts.CostCalculator(u.model, u.promptTokens, u.compTokens)
	}

	return u
}

func (h *Hook) recordMetrics(ctx context.Context, u usage, actionName string, durationSec float64) {
	if h.opts.Metrics == nil {
		return
	}

	tenant := xctx.TenantIDFrom(ctx)
	labels := []string{u.model, actionName, tenant}

	h.opts.Metrics.PromptTokensTotal.WithLabelValues(labels...).Add(float64(u.promptTokens))
	if u.cachedTokens > 0 {
		h.opts.Metrics.CachedPromptTokensTotal.WithLabelValues(labels...).Add(float64(u.cachedTokens))
	}
	h.opts.Metrics.CompletionTokensTotal.WithLabelValues(labels...).Add(float64(u.compTokens))

	if u.hasCost {
		h.opts.Metrics.CostUSDTotal.WithLabelValues(labels...).Add(u.costUSD)
	}

	if durationSec > 0 {
		h.opts.Metrics.LLMDurationSeconds.WithLabelValues(labels...).Observe(durationSec)
	}
}

func (h *Hook) enrichSpan(ctx context.Context, u usage, err error) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.SetAttributes(
		attribute.String("gen_ai.system", "llm"),
		attribute.String("gen_ai.model", u.model),
		attribute.Int("gen_ai.usage.input_tokens", u.promptTokens),
		attribute.Int("gen_ai.usage.cached_tokens", u.cachedTokens),
		attribute.Int("gen_ai.usage.output_tokens", u.compTokens),
	)

	if u.hasCost {
		span.SetAttributes(attribute.Float64("gen_ai.usage.cost_usd", u.costUSD))
	}

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return
	}

	span.SetStatus(codes.Ok, "")
}

func (h *Hook) logUsage(ctx context.Context, u usage, actionName string, err error) {
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelError
	}

	if !h.log.Enabled(ctx, level) {
		return
	}

	attrs := []any{
		slog.String("model", u.model),
		slog.Int("prompt_tokens", u.promptTokens),
		slog.Int("cached_tokens", u.cachedTokens),
		slog.Int("completion_tokens", u.compTokens),
		slog.String("action", actionName),
		slog.String("tenant", xctx.TenantIDFrom(ctx)),
	}

	if u.hasCost {
		attrs = append(attrs, slog.Float64("cost_usd", u.costUSD))
	}

	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
		h.log.ErrorContext(ctx, "LLM execution failed", attrs...)

		return
	}

	h.log.InfoContext(ctx, "LLM execution completed", attrs...)
}

type contextKeyStartTime struct{}

var _ observe.Sink = (*Hook)(nil)
