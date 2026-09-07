# Nexss Observability 🔭

[![Go Reference](https://pkg.go.dev/badge/github.com/nexssp/observability.svg)](https://pkg.go.dev/github.com/nexssp/observability)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**Zero-Touch Telemetry and Pragmatic Observability for High-Performance Go Applications.**

`nexssp/observability` separates application infrastructure concerns from your core business domain rules [1]. By utilizing Go's native `context.Context` propagation and the `nexssp/kernel` zero-allocation `AnyHook` execution hooks, we achieve full metric tracking, distributed tracing, and structured contextual logging without introducing external SDK dependencies into your business logic [1].

## 🏗️ Architecture Philosophy

**Observability is an infrastructure concern, not a domain concern.**

We strictly forbid polluting business logic (`kernel/action` handlers) with tracing setup, metric counters, or correlation ID extraction.

Instead, this package utilizes the Kernel's `AnyHook` interface to intercept executions at the boundary.

### Telemetry Stack Integration:
* **Prometheus Metrics:** Automatic `action_latency_ms` and `action_errors_total` on every action.
* **OpenTelemetry Traces:** Spans automatically created and closed for every action invocation.
* **Contextual Logging:** `log/slog` automatically enriched with `trace_id`, `span_id`, and `action` names.
* **Transparent Propagation:** Wrapped DB drivers and HTTP clients that automatically extract/inject W3C trace headers.
* **Health Aggregation:** A unified `/healthz` endpoint with strict timeouts to prevent cascading infrastructure failures.

---

## 🤯 The Magic of Nexss: "Zero-Touch" Telemetry

In traditional microservices, observability is a disease that infects your business logic. Developers are forced to become infrastructure experts, mixing OpenTelemetry APIs, Prometheus counters, and context extraction directly into their domain code.

### ❌ The "Old Way" (Spaghetti Code)
Look at a standard Go handler. Over 70% of this code is infrastructure boilerplate.

```go
func CreateOrder(ctx context.Context, req OrderReq) (OrderRes, error) {
    // 1. Tracing Boilerplate
    ctx, span := tracer.Start(ctx, "CreateOrder")
    defer span.End()

    // 2. Metrics Boilerplate
    start := time.Now()
    defer func() {
        requestLatency.WithLabelValues("CreateOrder").Observe(time.Since(start).Seconds())
    }()

    // 3. Logging Boilerplate
    log.With("trace_id", span.SpanContext().TraceID().String()).Info("creating order")

    // --- FINALLY, THE ACTUAL BUSINESS LOGIC ---
    err := db.ExecContext(ctx, "INSERT INTO orders...", req.ID)
    if err != nil {
        errorCounter.WithLabelValues("CreateOrder").Inc() // More boilerplate
        span.RecordError(err)                             // More boilerplate
        return OrderRes{}, err
    }
    return OrderRes{Status: "created"}, nil
}
```

### ✨ The "Nexss Way" (Pristine Domain Logic)
In Nexss, your developers **never write a single line of tracing or metrics code**. They write pure business logic.

```go
var CreateOrder = action.New("order.create", func(ctx context.Context, req OrderReq) (OrderRes, error) {

    // 1. Contextual Logger automatically knows the Trace ID!
    logger.InfoContext(ctx, "creating order")

    // 2. Passing 'ctx' automatically creates a Child Span for the DB query!
    err := db.ExecContext(ctx, "INSERT INTO orders...", req.ID)
    if err != nil {
        return OrderRes{}, err
    }

    return OrderRes{Status: "created"}, nil
}).Build()
```

### 🪄 How is this possible? (The `AnyHook` Architecture)

Where did the OpenTelemetry spans and Prometheus metrics go? **They are completely decoupled.**

Instead of hardcoding infrastructure into the action, you plug it in at the gateway boundary during server boot using the lock-free `AnyHook` interface:

```go
// Wire infrastructure ONCE in main.go
telemetryPlugin := actionhook.New(obsProvider)

// Instantly instrument EVERY action in your app
CreateOrder.AddAnyHook(telemetryPlugin)
```

When `CreateOrder` runs, the Nexss Kernel does the following with **zero heap allocations**:
1. **Interceptor Fires:** The hook generates the OTel Span and starts the Prometheus timer.
2. **Context Injection:** The hook silently places the active trace into the standard `context.Context`.
3. **Execution:** Your pristine business logic runs. The wrapped `dbtrace.DB` finds the trace in the context and attaches its queries to it.
4. **Cleanup:** The hook catches any returned errors, increments failure metrics, logs the stack trace, and closes the span.

---

## 📦 Getting Started

### 1. Installation
```bash
go get github.com/nexssp/observability
```

### 2. Basic Initialization (Auto-configuration)
Calling `obs.Auto()` configures the telemetry provider out-of-the-box using standard environment variables:

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

	// Auto-configure tracer and metrics providers from env
	provider, shutdown, err := obs.Auto()
	if err != nil {
		log.Fatalf("failed to initialize telemetry: %v", err)
	}
	defer shutdown(ctx)

	provider.Logger().InfoContext(ctx, "telemetry successfully initialized")
}
```

### 3. Advanced Configuration (Prometheus, VictoriaMetrics & Grafana)
For shared infrastructure environments or custom metric layouts, use `AutoWithOptions` to configure custom registries, naming structures, and custom histogram boundaries:

```go
provider, shutdown, _ := obs.AutoWithOptions(
	// 1. Enforce Prometheus standard naming conventions (namespace_subsystem_metric)
	obs.WithMetricsNamespace("nexssp"),
	obs.WithMetricsSubsystem("kernel"),

	// Or apply a flat, direct prefix if you are using simpler systems (e.g., Datadog, OpenObserve)
	// obs.WithMetricsPrefix("custom_flat_prefix"),

	// 2. Customize latency histogram buckets for fine-grained millisecond tracking
	obs.WithHistogramBuckets([]float64{
		0.1, 0.5, 1.0, 2.0, 5.0, 10.0, 25.0, 50.0, 100.0, 250.0, 500.0, 1000.0,
	}),

	// 3. Inject a shared or global Prometheus Registry
	obs.WithPrometheusRegistry(prometheus.NewRegistry()),

	// 4. Wrap any custom slog.Handler (e.g., console output or a custom Loki log shipper)
	obs.WithLoggerHandler(slog.NewJSONHandler(os.Stdout, nil)),
)
defer shutdown(context.Background())
```

### 4. Zero-Dependency Trace ID Extraction
Avoid forcing your business logic to import the heavy OpenTelemetry trace SDK just to retrieve a trace or span ID as a string. Use our zero-dependency string helpers:

```go
import obs "github.com/nexssp/observability"

func Process(ctx context.Context) {
	traceID := obs.TraceID(ctx) // Returns string (e.g., "4bf92f3577b34da6a3ce929d0e0e4736")
	spanID := obs.SpanID(ctx)   // Returns string (e.g., "00f067aa0ba902b7")

	println("Active Trace ID:", traceID)
}
```

---

## ⚡ SRE Edge Cases Covered

### 1. Panic Isolation inside Sub-Spans
When creating manual spans inside your code, Go's panic recovery may bypass the standard closure function, causing orphan spans. We use deferred named-error processing to capture panics:

```go
import obs "github.com/nexssp/observability"

func ParseComplexJSON(ctx context.Context, data []byte) (res string, err error) {
    ctx, endSpan := obs.StartSpan(ctx, "json.parse")
    defer func() {
        if r := recover(); r != nil {
            err = xerr.PanicRecovery(r)
        }
        endSpan(err) // Safely closes the span and records the recovered panic
    }()

    // ... code that might panic ...
}
```

### 2. Dynamic Span Enrichment
Because the context contains standard OpenTelemetry data, you can dynamically add tags or attributes to the current running span anywhere in your business logic without vendor lock-in.

```go
import "go.opentelemetry.io/otel/trace"
import "go.opentelemetry.io/otel/attribute"

// Enrich the active span (if present)
if span := trace.SpanFromContext(ctx); span.IsRecording() {
    span.SetAttributes(
        attribute.String("user.tier", "VIP"),
        attribute.Float64("payment.amount", 250.00),
    )
}
```

### 3. Non-Blocking Async Trace Detachment
Launching background goroutines with standard request contexts is dangerous—when the client request ends, the context cancels, killing any downstream background traces. We use trace detachment to let background operations execute under independent lifecycles while preserving tracing context:

```go
import "github.com/nexssp/kernel/xctx"

func HandleRequest(ctx context.Context) {
    // Clone trace context without cancellation to prevent background tracing failure
    asyncCtx := xctx.CloneForAsync(ctx)

    go func() {
        ctx, endSpan := obs.StartSpan(asyncCtx, "async.background_cleanup")
        defer endSpan()

        // Executes safely even if the main client request has exited
        processCleanup(ctx)
    }()
}
```

### 4. Visual Timeline & Waterfall Extraction (e.g., for UI/CLI Dashboards)
If you are building interactive developer consoles (like TUI dashboards or agent control panels) and want to render a visual waterfall timeline of all sub-spans executed within a request, you can use the in-memory trace collector:

```go
import obs "github.com/nexssp/observability"

func RunComplexTask(ctx context.Context) {
	// 1. Initialize an in-memory trace collector in the context
	ctx, collector := obs.WithCollector(ctx)

	// 2. Execute standard database queries, API calls, and sub-spans
	_ = executeFidelitySteps(ctx)

	// 3. Export a complete, machine-readable TraceRecord
	if record, ok := obs.TraceRecordFromContext(ctx, "task.run", "http", nil); ok {
		// Send to your frontend (e.g., via Server-Sent Events / SSE) to render Gantt charts
		renderTimelineWaterfall(record)
	}
}
```

---

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
