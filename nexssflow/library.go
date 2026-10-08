// Package nexssflow adapts Nexss Observability to Nexss Flow.
package nexssflow

import (
	"context"
	"errors"

	"github.com/nexssp/flow/core"
	"github.com/nexssp/kernel/action"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
)

const ID = "observability"

func init() {
	core.Register(ID, Bundle)
}

// Config overrides the environment-derived observability configuration.
// Every field is optional; an empty value inherits from LoadConfigFromEnv
// (SERVICE_NAME, ENV, SAMPLE_RATIO, OTLP_ENDPOINT, and OTLP_INSECURE).
type Config struct {
	ServiceName      string  `flow:"service_name"`
	Env              string  `flow:"env"`
	SampleRatio      float64 `flow:"sample_ratio"`
	OTLPEndpoint     string  `flow:"otlp_endpoint"`
	OTLPInsecure     bool    `flow:"otlp_insecure"`
	MetricsPrefix    string  `flow:"metrics_prefix"`
	MetricsNamespace string  `flow:"metrics_namespace"`
	MetricsSubsystem string  `flow:"metrics_subsystem"`
	TraceExporter    string  `flow:"trace_exporter"` // stdout | otlp | none

	sampleRatioSet  bool
	otlpInsecureSet bool
}

// Bundle is the standard Flow bundle contract. Provider configuration is
// derived from environment variables; entries in @require { ... } override
// individual fields. Provider shutdown is transferred to Flow's invocation
// Host through Bundle.Shutdowns.
//
// A constructor error (invalid @require key, bad OTLP endpoint, nil
// provider) is a programmer or configuration error; panic matches the
// convention used by every transport adapter.
func Bundle(opts map[string]string) core.Bundle {
	overrides, err := decodeOverrides(opts)
	if err != nil {
		panic("nexssflow: " + err.Error())
	}

	cfg := obs.LoadConfigFromEnv()
	applyOverrides(&cfg, overrides)

	processor, err := buildTraceProcessor(overrides.TraceExporter)
	if err != nil {
		panic("nexssflow: " + err.Error())
	}
	cfg.TraceProcessor = processor

	provider, shutdown, err := obs.NewWithShutdown(cfg)
	if err != nil {
		panic("nexssflow: " + err.Error())
	}

	return buildBundle(provider, shutdown)
}

func decodeOverrides(opts map[string]string) (Config, error) {
	overrides, err := core.Decode[Config](opts)
	if err != nil {
		return Config{}, err
	}
	if value, ok := opts["sample_ratio"]; ok && value != "" {
		overrides.sampleRatioSet = true
	}
	if value, ok := opts["otlp_insecure"]; ok && value != "" {
		overrides.otlpInsecureSet = true
	}
	return overrides, nil
}

// NewBundle is the programmatic entry point for hosts that own the
// provider lifecycle. It returns an error instead of panicking so a
// caller can surface initialization failures before running any Flow.
func NewBundle(provider *obs.Provider, shutdown func(context.Context) error) (core.Bundle, error) {
	if provider == nil {
		return core.Bundle{}, errors.New("nexssflow: provider is nil")
	}
	if shutdown == nil {
		return core.Bundle{}, errors.New("nexssflow: shutdown callback is nil")
	}
	return buildBundle(provider, shutdown), nil
}

func applyOverrides(cfg *obs.Config, overrides Config) {
	if overrides.ServiceName != "" {
		cfg.ServiceName = overrides.ServiceName
	}
	if overrides.Env != "" {
		cfg.Env = overrides.Env
	}
	if overrides.sampleRatioSet || overrides.SampleRatio != 0 {
		cfg.SampleRatio = overrides.SampleRatio
		cfg.SampleRatioSet = true
	}
	if overrides.OTLPEndpoint != "" {
		cfg.OTLPEndpoint = overrides.OTLPEndpoint
	}
	if overrides.otlpInsecureSet || overrides.OTLPInsecure {
		cfg.OTLPInsecure = overrides.OTLPInsecure
	}
	if overrides.MetricsPrefix != "" {
		cfg.MetricsPrefix = overrides.MetricsPrefix
	}
	if overrides.MetricsNamespace != "" {
		cfg.MetricsNamespace = overrides.MetricsNamespace
	}
	if overrides.MetricsSubsystem != "" {
		cfg.MetricsSubsystem = overrides.MetricsSubsystem
	}
}

func buildBundle(provider *obs.Provider, shutdown func(context.Context) error) core.Bundle {
	providerConfig := provider.Config()
	return core.Bundle{
		ID: ID,
		Libraries: []action.Library{
			{Name: eventLibrary, Actions: []action.AnyAction{newEventAction(provider)}},
		},
		Hooks: []action.AnyHook{actionhook.New(provider)},
		WrapPipeline: func(_ map[string]any, inner action.AnyAction) (action.AnyAction, error) {
			return wrapRootSpan(inner, provider.Tracer("nexss/obs"), providerConfig.ServiceName, providerConfig.Env), nil
		},
		Shutdowns: []core.ShutdownFunc{shutdown},
	}
}

// wrapRootSpan wraps the whole compiled program in a single flow.run
// span, so atoms from files without @pipeline share one TraceID. Nested
// wrappers (sub-pipeline compiles) detect the existing span and pass
// through, so a run produces exactly one root span.
func wrapRootSpan(inner action.AnyAction, tracer trace.Tracer, serviceName, environment string) action.AnyAction {
	return action.New("observability.root", func(ctx context.Context, req any) (any, error) {
		if trace.SpanFromContext(ctx).SpanContext().IsValid() {
			return action.InvokeAny(ctx, inner, req)
		}
		ctx, span := tracer.Start(ctx, "flow.run",
			trace.WithSpanKind(trace.SpanKindInternal),
			trace.WithAttributes(
				attribute.String("nexss.service.name", serviceName),
				attribute.String("nexss.env", environment),
			),
		)
		defer span.End()
		return action.InvokeAny(ctx, inner, req)
	}).Build()
}
