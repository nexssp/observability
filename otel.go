package obs

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func newTracerProvider(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	if cfg.OTLPEndpoint == "" {
		return nil, nil
	}

	raw := cfg.OTLPEndpoint
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid OTLP endpoint %q: %w", cfg.OTLPEndpoint, err)
	}

	host := u.Host
	if host == "" {
		host = u.Path
	}
	path := u.Path
	if path == "" || path == "/" {
		path = "/v1/traces"
	}

	isSecure := u.Scheme == "https"

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithURLPath(path),
		otlptracehttp.WithTimeout(5 * time.Second),
		otlptracehttp.WithHeaders(cfg.OTLPHeaders),
	}

	switch {
	case cfg.OTLPInsecure || u.Scheme == "http":
		opts = append(opts, otlptracehttp.WithInsecure())
	case isSecure:
		opts = append(opts, otlptracehttp.WithTLSClientConfig(&tls.Config{}))
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	defaultRes := resource.Default()
	res, err := resource.Merge(defaultRes,
		resource.NewWithAttributes(defaultRes.SchemaURL(),
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceInstanceID(cfg.NodeID),
			semconv.DeploymentEnvironment(cfg.Env),
		))
	if err != nil {
		return nil, fmt.Errorf("resource merge: %w", err)
	}

	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)
	otel.SetTracerProvider(tp)
	return tp, nil
}
