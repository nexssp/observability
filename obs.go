// Package obs initializes OpenTelemetry meters, tracers, Prometheus registries, and health endpoints.
package obs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nexssp/observability/llm"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

type obsCtxKey struct{}

type obsState struct {
	action  string
	startMs int64
}

func withObsState(ctx context.Context, state *obsState) context.Context {
	return context.WithValue(ctx, obsCtxKey{}, state)
}

func getObsState(ctx context.Context) *obsState {
	value := ctx.Value(obsCtxKey{})
	if value == nil {
		return nil
	}
	state, _ := value.(*obsState)
	return state
}

// Provider manages OpenTelemetry meters, tracers, and health checks.
type Provider struct {
	cfg          Config
	logger       *slog.Logger
	handler      *ContextHandler
	tp           *sdktrace.TracerProvider
	mp           *sdkmetric.MeterProvider
	promRegistry *prometheus.Registry

	latencyHisto     metric.Float64Histogram
	errorCounter     metric.Int64Counter
	suspendedCounter metric.Int64Counter // Tracks HIL workflows paused for human intervention

	checksMutex sync.RWMutex
	checks      map[string]func(context.Context) error
	llmMetrics  *llm.Metrics
}

var _ HealthRegistry = (*Provider)(nil)

// Auto initializes standard observability using environment configuration.
func Auto() (*Provider, func(context.Context) error, error) {
	cfg := LoadConfigFromEnv()
	return NewWithShutdown(cfg)
}

// AutoWithOptions initializes observability with optional functional modifications.
func AutoWithOptions(opts ...Option) (*Provider, func(context.Context) error, error) {
	cfg := LoadConfigFromEnv()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return NewWithShutdown(cfg)
}

// NewWithShutdown instantiates Provider alongside an explicit cleanup function.
func NewWithShutdown(cfg Config) (*Provider, func(context.Context) error, error) {
	ctx := context.Background()
	provider, err := New(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("new obs provider: %w", err)
	}
	shutdown := func(shutdownCtx context.Context) error {
		var firstError error
		if provider.tp != nil {
			if err := provider.tp.Shutdown(shutdownCtx); err != nil {
				firstError = fmt.Errorf("tracer shutdown: %w", err)
			}
		}
		if provider.mp != nil {
			if err := provider.mp.Shutdown(shutdownCtx); err != nil && firstError == nil {
				firstError = fmt.Errorf("meter shutdown: %w", err)
			}
		}
		return firstError
	}
	return provider, shutdown, nil
}

// New constructs Provider with OpenTelemetry tracer, meter, and Prometheus registry.
func New(ctx context.Context, cfg Config, opts ...Option) (*Provider, error) {
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	if cfg.ServiceName == "" {
		cfg.ServiceName = "nexss"
	}
	if cfg.NodeID == "" {
		cfg.NodeID = nodeID(cfg.ServiceName)
	}
	if cfg.SampleRatio <= 0 {
		cfg.SampleRatio = 1.0
	}
	if cfg.Env == "" {
		cfg.Env = "local"
	}
	if cfg.HealthCheckTimeout <= 0 {
		cfg.HealthCheckTimeout = 3 * time.Second
	}

	promRegistry := cfg.PrometheusRegistry
	if promRegistry == nil {
		promRegistry = prometheus.NewRegistry()
	}

	promExporter, err := otelprom.New(otelprom.WithRegisterer(promRegistry))
	if err != nil {
		return nil, fmt.Errorf("prom exporter: %w", err)
	}

	defaultRes := resource.Default()
	mergedResource, err := resource.Merge(defaultRes,
		resource.NewWithAttributes(defaultRes.SchemaURL(),
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceInstanceID(cfg.NodeID),
			semconv.DeploymentEnvironment(cfg.Env),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource merge: %w", err)
	}

	metricName := func(name string) string {
		var parts []string
		if cfg.MetricsPrefix != "" {
			parts = append(parts, cfg.MetricsPrefix)
		} else {
			if cfg.MetricsNamespace != "" {
				parts = append(parts, cfg.MetricsNamespace)
			}
			if cfg.MetricsSubsystem != "" {
				parts = append(parts, cfg.MetricsSubsystem)
			}
		}
		parts = append(parts, name)
		return strings.Join(parts, "_")
	}

	mpOpts := []sdkmetric.Option{
		sdkmetric.WithResource(mergedResource),
		sdkmetric.WithReader(promExporter),
	}

	if len(cfg.HistogramBuckets) > 0 {
		view := sdkmetric.NewView(
			sdkmetric.Instrument{Name: metricName("action_latency_ms")},
			sdkmetric.Stream{
				Aggregation: sdkmetric.AggregationExplicitBucketHistogram{
					Boundaries: cfg.HistogramBuckets,
				},
			},
		)
		mpOpts = append(mpOpts, sdkmetric.WithView(view))
	}

	meterProvider := sdkmetric.NewMeterProvider(mpOpts...)
	otel.SetMeterProvider(meterProvider)

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	tracerProvider, err := newTracerProvider(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("tracer provider: %w", err)
	}

	meter := meterProvider.Meter("nexss/obs")

	latencyHisto, err := meter.Float64Histogram(metricName("action_latency_ms"),
		metric.WithDescription("Action latency in ms"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		return nil, fmt.Errorf("latency histogram: %w", err)
	}

	errorCounter, err := meter.Int64Counter(metricName("action_errors_total"),
		metric.WithDescription("Total action errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("error counter: %w", err)
	}

	suspendedCounter, err := meter.Int64Counter(metricName("action_suspended_total"),
		metric.WithDescription("Total actions/workflows suspended for human intervention (HIL)"),
	)
	if err != nil {
		return nil, fmt.Errorf("suspended counter: %w", err)
	}

	handler := NewContextHandler(cfg.LoggerHandler)
	logger := slog.New(handler)

	llmMetrics := llm.NewMetrics(promRegistry, cfg.MetricsNamespace, cfg.MetricsSubsystem)

	return &Provider{
		cfg:              cfg,
		logger:           logger,
		handler:          handler,
		tp:               tracerProvider,
		mp:               meterProvider,
		promRegistry:     promRegistry,
		latencyHisto:     latencyHisto,
		errorCounter:     errorCounter,
		suspendedCounter: suspendedCounter,
		checks:           make(map[string]func(context.Context) error),
		llmMetrics:       llmMetrics,
	}, nil
}

// Sink returns an implementation of kernel/observe.Sink that routes
// Kernel lifecycle events to OpenTelemetry and Prometheus.
func (p *Provider) Sink() *Sink {
	return NewSink(p)
}

// Logger returns the configured contextual slog logger.
func (p *Provider) Logger() *slog.Logger { return p.logger }

// Handler returns the ContextHandler.
func (p *Provider) Handler() slog.Handler { return p.handler }

// MetricsHandler provides standard HTTP handler for Prometheus scraping.
func (p *Provider) MetricsHandler() http.Handler {
	return promhttp.HandlerFor(p.promRegistry, promhttp.HandlerOpts{})
}

// Config exposes active Provider configuration.
func (p *Provider) Config() Config { return p.cfg }

// RegisterCheck attaches a readiness health probe.
func (p *Provider) RegisterCheck(name string, checkFunc func(context.Context) error) {
	if checkFunc == nil {
		return
	}
	p.checksMutex.Lock()
	defer p.checksMutex.Unlock()
	p.checks[name] = checkFunc
}

// HealthHandler serves JSON HTTP readiness probe responses.
func (p *Provider) HealthHandler() http.Handler {
	return http.HandlerFunc(p.handleHealth)
}

func (p *Provider) LLMMetrics() *llm.Metrics {
	return p.llmMetrics
}

func (p *Provider) handleHealth(w http.ResponseWriter, r *http.Request) {
	p.checksMutex.RLock()
	checksCopy := make(map[string]func(context.Context) error, len(p.checks))
	maps.Copy(checksCopy, p.checks)
	p.checksMutex.RUnlock()

	results := make(map[string]string, len(checksCopy))
	status := http.StatusOK
	for name, checkFunc := range checksCopy {
		checkCtx, cancel := context.WithTimeout(r.Context(), p.cfg.HealthCheckTimeout)
		err := checkFunc(checkCtx)
		cancel()
		if err != nil {
			results[name] = err.Error()
			status = http.StatusServiceUnavailable
		} else {
			results[name] = "ok"
		}
	}

	payload := map[string]any{"status": http.StatusText(status)}
	if len(results) > 0 {
		payload["checks"] = results
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		p.logger.ErrorContext(r.Context(), "health response encode failed", "error", err)
	}
}

// TraceID extracts active trace ID from context as string (or returns empty).
func TraceID(ctx context.Context) string {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// SpanID extracts active span ID from context as string (or returns empty).
func SpanID(ctx context.Context) string {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.SpanContext().SpanID().String()
	}
	return ""
}
