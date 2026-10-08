# Nexss Observability 🔭 + 🌀 NEXSS FLOW

[![Go Reference](https://pkg.go.dev/badge/github.com/nexssp/observability.svg)](https://pkg.go.dev/github.com/nexssp/observability)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**Zero-Touch Telemetry and Pragmatic Observability for High-Performance Go Applications.**

`nexssp/observability` keeps infrastructure concerns out of your domain code.
Go's native `context.Context` and `nexssp/kernel`'s `AnyHook` execution hooks
carry tracing, metrics, and structured logging across every action without a
single infrastructure import in your handlers.

## 🏗️ Architecture Philosophy

**Observability is an infrastructure concern, not a domain concern.**

Business logic (`kernel/action` handlers) never sees a tracer, a metric
counter, or a correlation ID extractor. The `AnyHook` interface intercepts
every action at the boundary.

### What you get

- **Prometheus metrics** — `action_latency_ms`, `action_errors_total`,
  `action_suspended_total`, plus LLM cost/token counters.
- **OpenTelemetry traces** — one span per action, plus a `flow.run` root span
  when driven through the Flow adapter.
- **Contextual logging** — `log/slog` records carry `trace_id`, `span_id`,
  and the current action name.
- **Transparent propagation** — wrapped DB drivers and HTTP clients attach
  themselves to the active trace through `context.Context`.
- **Health aggregation** — a registry of readiness checks behind a bounded
  HTTP handler.

---

## 🤯 The Magic of Nexss: "Zero-Touch" Telemetry

In traditional microservices, observability infects the business logic.
Developers mix OpenTelemetry APIs, Prometheus counters, and context
extraction directly into their domain code.

### ❌ The "Old Way" (Spaghetti Code)

Over 70% of this handler is infrastructure boilerplate.

```go
func CreateOrder(ctx context.Context, req OrderReq) (OrderRes, error) {
    ctx, span := tracer.Start(ctx, "CreateOrder")
    defer span.End()

    start := time.Now()
    defer func() {
        requestLatency.WithLabelValues("CreateOrder").Observe(time.Since(start).Seconds())
    }()

    log.With("trace_id", span.SpanContext().TraceID().String()).Info("creating order")

    err := db.ExecContext(ctx, "INSERT INTO orders...", req.ID)
    if err != nil {
        errorCounter.WithLabelValues("CreateOrder").Inc()
        span.RecordError(err)
        return OrderRes{}, err
    }
    return OrderRes{Status: "created"}, nil
}
```

### ✨ The "Nexss Way" (Pristine Domain Logic)

```go
var CreateOrder = action.New("order.create", func(ctx context.Context, req OrderReq) (OrderRes, error) {

    logger.InfoContext(ctx, "creating order")

    err := db.ExecContext(ctx, "INSERT INTO orders...", req.ID)
    if err != nil {
        return OrderRes{}, err
    }

    return OrderRes{Status: "created"}, nil
}).Build()
```

### 🪄 How is this possible?

The tracing and metrics live in a hook, attached once at the boundary:

```go
telemetryPlugin := actionhook.New(obsProvider)
CreateOrder.AddAnyHook(telemetryPlugin)
```

When `CreateOrder` runs:

1. **Interceptor fires** — the hook opens an OTel span and stamps the context.
2. **Context propagates** — the wrapped `dbtrace.DB` and `httptrace.Client`
   find the trace through `context.Context` and attach their spans.
3. **Cleanup runs** — the hook records the error, increments counters, and
   ends the span.

The hook adds no heap allocations beyond what the OTel SDK's own span
lifecycle already does. Trace ID and span ID extraction
(`obs.TraceID`, `obs.SpanID`) are zero-allocation.

---

## 📦 Getting Started

### 1. Installation

```bash
go get github.com/nexssp/observability
```

### 2. Auto-configuration

```bash
export SERVICE_NAME="order-processor"
export ENV="production"
export OTLP_ENDPOINT="http://openobserve:5080/v1/traces"
export OTLP_HEADERS="Authorization=Bearer abc123xyz"
```

```go
package main

import (
	"context"
	"log"

	obs "github.com/nexssp/observability"
)

func main() {
	ctx := context.Background()

	provider, shutdown, err := obs.Auto()
	if err != nil {
		log.Fatalf("failed to initialize telemetry: %v", err)
	}
	defer func() { _ = shutdown(ctx) }()

	provider.Logger().InfoContext(ctx, "telemetry successfully initialized")
}
```

### 3. Advanced Configuration

Use `AutoWithOptions` for custom registries, naming conventions, histogram
boundaries, or a custom `slog.Handler`:

```go
provider, shutdown, _ := obs.AutoWithOptions(
	// Prometheus naming: namespace_subsystem_metric
	obs.WithMetricsNamespace("nexssp"),
	obs.WithMetricsSubsystem("kernel"),
	// Or a flat prefix, e.g. for Datadog or OpenObserve:
	// obs.WithMetricsPrefix("custom_flat_prefix"),

	obs.WithHistogramBuckets([]float64{
		0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 25.0, 50.0, 100.0, 250.0, 500.0, 1000.0,
	}),

	obs.WithPrometheusRegistry(prometheus.NewRegistry()),
	obs.WithLoggerHandler(slog.NewJSONHandler(os.Stdout, nil)),
)
defer func() { _ = shutdown(context.Background()) }()
```

### 4. Zero-Dependency Trace ID Extraction

Business logic can read the active trace and span IDs as strings without
importing the OTel SDK:

```go
import obs "github.com/nexssp/observability"

func Process(ctx context.Context) {
	traceID := obs.TraceID(ctx) // "4bf92f3577b34da6a3ce929d0e0e4736"
	spanID := obs.SpanID(ctx)   // "00f067aa0ba902b7"
	_ = traceID
	_ = spanID
}
```

---

## 🌀 Flow Integration

The `nexssflow` adapter turns every `.nflow` atom into a span without
touching the flow source. It lives inside this module — no separate `go get`.

```nflow
@require github.com/nexssp/observability/nexssflow

@assert: result.value == "hello"
{ value: "hello" } -> noop -> noop
```

The adapter delivers its hook through `core.Bundle.Hooks`, which
`runner.BuildConfig` aggregates into `runner.Config.Hooks` and
`executionResolver` clones into every resolved action — regardless of which
bundle mounted it. A `WrapPipeline` wrapper opens a single `flow.run` root
span, so every atom from `pipeline.<name>` to `runtime.*` shares one TraceID.

### Local development without a collector

```nflow
@require github.com/nexssp/observability/nexssflow { trace_exporter: "stdout" }

{ value: "hello" } -> noop -> noop
```

One line per span on stderr, live:

```
15:19:56.417 [ok ] action.runtime.noop   0ns     trace=51ec7318  span=b5808e92  parent=b3198488
15:19:56.427 [ok ] flow.run              9.74ms  trace=51ec7318  span=b3198488  parent=--------
```

`trace_exporter` accepts `otlp` (default), `stdout`, or `none`. Every
environment variable in `LoadConfigFromEnv` can be overridden per flow:

```nflow
@require github.com/nexssp/observability/nexssflow {
  otlp_endpoint: "http://collector:4318",
  service_name: "checkout",
  sample_ratio: 0.1
}
```

See [`nexssflow/README.md`](nexssflow/README.md) for the full configuration
table and runnable examples.

---

## ⚡ SRE Edge Cases Covered

### 1. Panic Isolation inside Sub-Spans

Deferred named-error processing captures panics inside manual spans so the
span is always ended, even when the closure is bypassed:

```go
func ParseComplexJSON(ctx context.Context, data []byte) (res string, err error) {
	ctx, endSpan := obs.StartSpan(ctx, "json.parse")
	defer func() {
		if r := recover(); r != nil {
			err = xerr.PanicRecovery(r)
		}
		endSpan(err)
	}()
	// ... code that might panic ...
	return res, nil
}
```

### 2. Dynamic Span Enrichment

The context carries a standard OTel span; add attributes anywhere without
vendor lock-in:

```go
if span := trace.SpanFromContext(ctx); span.IsRecording() {
	span.SetAttributes(
		attribute.String("user.tier", "VIP"),
		attribute.Float64("payment.amount", 250.00),
	)
}
```

### 3. Non-Blocking Async Trace Detachment

Background goroutines must not inherit the request's cancellation. Clone the
trace context first:

```go
asyncCtx := xctx.CloneForAsync(ctx)

go func() {
	ctx, endSpan := obs.StartSpan(asyncCtx, "async.background_cleanup")
	defer endSpan()
	processCleanup(ctx)
}()
```

### 4. Visual Timeline & Waterfall Extraction

For TUI dashboards or agent control panels, use the in-memory trace
collector to render a Gantt-style waterfall of every sub-span in one request:

```go
ctx, collector := obs.WithCollector(ctx)
_ = executeFidelitySteps(ctx)

if record, ok := obs.TraceRecordFromContext(ctx, "task.run", "http", nil); ok {
	renderTimelineWaterfall(record)
}
```

### 5. High-Throughput Stream Telemetry

`iter.Seq2` streams report aggregate duration, item counts, dropped items,
in-flight span events, and structured `obs.StreamStats` with zero per-item
allocation overhead. See
[`examples/05_stream_telemetry`](examples/05_stream_telemetry).

---

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
