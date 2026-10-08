// Package obs provides OpenTelemetry-based tracing, metrics, contextual logging, and readiness health probes.
package obs

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Config holds observability initialization parameters.
type Config struct {
	ServiceName  string
	NodeID       string
	Env          string
	OTLPEndpoint string
	OTLPHeaders  map[string]string
	OTLPInsecure bool
	// SampleRatio must be in [0,1]. A zero value defaults to 1 unless
	// SampleRatioSet is true, which distinguishes explicit zero sampling.
	SampleRatio        float64
	SampleRatioSet     bool
	HealthCheckTimeout time.Duration

	PrometheusRegistry *prometheus.Registry
	MetricsPrefix      string
	MetricsNamespace   string
	MetricsSubsystem   string
	HistogramBuckets   []float64
	LoggerHandler      slog.Handler
	TraceProcessor     sdktrace.SpanProcessor
}

// LoadConfigFromEnv builds Config from standard environment variables.
func LoadConfigFromEnv() Config {
	svc := os.Getenv("SERVICE_NAME")
	if svc == "" {
		svc = "nexss"
	}

	env := os.Getenv("ENV")
	if env == "" {
		env = "local"
	}

	var sampleRatio float64
	sampleRatioSet := false
	if rawRatio := os.Getenv("SAMPLE_RATIO"); rawRatio != "" {
		if val, err := strconv.ParseFloat(strings.TrimSpace(rawRatio), 64); err == nil {
			sampleRatio = val
			sampleRatioSet = true
		}
	}

	return Config{
		ServiceName:        svc,
		NodeID:             nodeID(svc),
		Env:                env,
		OTLPEndpoint:       os.Getenv("OTLP_ENDPOINT"),
		OTLPHeaders:        parseOTLPHeaders(os.Getenv("OTLP_HEADERS")),
		OTLPInsecure:       os.Getenv("OTLP_INSECURE") == "true" || strings.Contains(os.Getenv("OTLP_ENDPOINT"), "localhost"),
		SampleRatio:        sampleRatio,
		SampleRatioSet:     sampleRatioSet,
		HealthCheckTimeout: healthCheckTimeoutFromEnv(),
	}
}

func healthCheckTimeoutFromEnv() time.Duration {
	const defaultTimeout = 3 * time.Second

	value := strings.TrimSpace(os.Getenv("HEALTH_CHECK_TIMEOUT"))
	if value == "" {
		return defaultTimeout
	}

	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return defaultTimeout
	}

	return timeout
}

func nodeID(serviceName string) string {
	if value := os.Getenv("NODE_ID"); value != "" {
		return value
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	var randomBytes [4]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return fmt.Sprintf("%s-%s-%d", serviceName, hostname, time.Now().UnixNano())
	}

	return fmt.Sprintf("%s-%s-%04x", serviceName, hostname, randomBytes)
}

func parseOTLPHeaders(s string) map[string]string {
	headers := make(map[string]string)
	if s == "" {
		return headers
	}

	for kv := range strings.SplitSeq(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}

		parts := strings.SplitN(kv, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		headers[key] = val
	}

	return headers
}

type Option func(*Config)

func WithPrometheusRegistry(reg *prometheus.Registry) Option {
	return func(c *Config) { c.PrometheusRegistry = reg }
}

func WithMetricsPrefix(prefix string) Option {
	return func(c *Config) { c.MetricsPrefix = prefix }
}

func WithMetricsNamespace(ns string) Option {
	return func(c *Config) { c.MetricsNamespace = ns }
}

func WithMetricsSubsystem(sub string) Option {
	return func(c *Config) { c.MetricsSubsystem = sub }
}

func WithHistogramBuckets(buckets []float64) Option {
	return func(c *Config) { c.HistogramBuckets = buckets }
}

func WithLoggerHandler(handler slog.Handler) Option {
	return func(c *Config) { c.LoggerHandler = handler }
}
