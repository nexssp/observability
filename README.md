# Nexss Observability

<small>Nexss Flow 🌀 ready</small>

<!-- project-status -->

[![CI](https://github.com/nexssp/observability/actions/workflows/ci.yml/badge.svg)](https://github.com/nexssp/observability/actions/workflows/ci.yml)
[![Latest tag](https://img.shields.io/github/v/tag/nexssp/observability?sort=semver&label=latest%20tag)](https://github.com/nexssp/observability/tags)
[![Go Reference](https://pkg.go.dev/badge/github.com/nexssp/observability.svg)](https://pkg.go.dev/github.com/nexssp/observability)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

<!-- /project-status -->

Nexss Observability provides OpenTelemetry tracing, Prometheus metrics, contextual `log/slog` logging, and readiness checks for Nexss Kernel actions and Go services. Instrumentation can be attached at action and transport boundaries, so application code can use ordinary `context.Context` without hand-writing telemetry bookkeeping around every operation.

Requires **Go 1.26.9 or newer**.

## Capabilities

- **Action instrumentation:** `actionhook.New(provider)` adapts the provider to Kernel's `action.AnyHook`. Instrumented actions get spans, latency and error metrics, trace context, and retry/cache/concurrency events. Human-intervention suspensions are recorded separately from failures.

- **OpenTelemetry tracing:** configure OTLP/HTTP trace export, or supply a custom span processor. No network trace exporter is attached by the Go provider unless an OTLP endpoint is configured.

- **Prometheus metrics:** expose the provider's registry through `MetricsHandler()`. Action metrics are `action_latency_ms`, `action_errors_total`, and `action_suspended_total`; names can be prefixed or namespaced. Optional LLM metrics cover token counts, cost, and duration.

- **Contextual logging:** `Provider.Logger()` returns a `slog.Logger` that adds `trace_id`, `span_id`, `trace_flags`, and the active action name when a valid span is present.

- **Propagation helpers:** `httptrace` wraps HTTP clients/transports, and `dbtrace` wraps `database/sql` operations. `TraceID` and `SpanID` read IDs from the active OpenTelemetry span or Kernel context.

- **Manual spans and local collection:** `StartSpan` creates application-level spans. `WithCollector` keeps a bounded in-memory ring of spans created with `StartSpan` for simple local timelines.

- **Readiness and business events:** register readiness checks for `HealthHandler( )`, and emit structured business events with the `events` package. The Flow adapter also exposes `observability.emit_event`.

- **Stream and LLM hooks:** attach stream statistics to an active span, or opt into LLM token/cost metrics, logs, and span attributes with the `llm` hook.

These are Go APIs unless specifically called out under [Nexss Flow integration](#nexss-flow-integration). The provider returns HTTP handlers; it does not start a server or bind a port.

## Go integration

Install the module:

```
go get github.com/nexssp/observability@latest
```

Initialize the provider, attach its action hook, and shut it down with a bounded context:

```go
package main

import (
    "context"
    "errors"
    "log"
    "time"

    "github.com/nexssp/kernel/action"
    obs "github.com/nexssp/observability"
    "github.com/nexssp/observability/actionhook"
)

func run() (err error) {
    ctx := context.Background()
    provider, shutdown, err := obs.Auto()
    if err != nil {
        return err
    }
    shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
    defer cancel()
    defer func() { err = errors.Join(err, shutdown(shutdownCtx)) }()

    greet := action.New("demo.greet", func(_ context.Context, name string) (string, error) {
        return "hello " + name, nil
    }).AnyHook(actionhook.New(provider)).Build()

    result, err := greet.Do(ctx, "world")
    if err != nil {
        return err
    }
    provider.Logger().InfoContext(ctx, "greeting completed", "result", result)
    return nil
}

func main() {
    if err := run(); err != nil {
        log.Fatal(err)
    }
}
```

`actionhook.New(provider)` installs instrumentation on eligible actions as they are built; Kernel system-scope actions are excluded. The hook creates an `action.<name>` span, records latency/errors/suspensions, and places the active trace IDs into the action context. Attach the hook when building the action, or use the Flow bundle described below.

### Configuration

`obs.Auto()` reads these environment variables. Empty values use the defaults shown; invalid health timeouts fall back to the default.

| Variable | Purpose | Default / behavior |
| --- | --- | --- |
| `SERVICE_NAME` | Service resource name | `nexss` |
| `NODE_ID` | Service-instance identifier | Generated from service name, hostname, and a random suffix when unset |
| `ENV` | Deployment environment | `local` |
| `OTLP_ENDPOINT` | OTLP/HTTP trace endpoint | Unset; no network trace exporter is attached |
| `OTLP_HEADERS` | Comma-separated `key=value` exporter headers | Empty |
| `OTLP_INSECURE` | Allow insecure OTLP transport | `false`; localhost endpoints are treated as insecure |
| `SAMPLE_RATIO` | Trace sampling ratio in `[0,1]` | `1` (record all sampled traces); explicit `0` disables sampling |
| `HEALTH_CHECK_TIMEOUT` | Per-check readiness timeout, as a Go duration | `3s` |

For code-level settings, use `AutoWithOptions` or `NewWithShutdown` with `Config`:

```go
provider, shutdown, err := obs.AutoWithOptions(
    obs.WithPrometheusRegistry(prometheus.NewRegistry()),
    obs.WithMetricsNamespace("nexss"),
    obs.WithMetricsSubsystem("checkout"),
    obs.WithHistogramBuckets([]float64{1, 5, 10, 50, 100, 500}),
    obs.WithLoggerHandler(slog.NewJSONHandler(os.Stdout, nil)),
)
```

Handle `err` and call the returned `shutdown` function when the application exits. Available options also include `WithMetricsPrefix`; when set, the prefix takes precedence over the namespace/subsystem pair for the action metrics. These settings customize the registry, metric names and histogram boundaries, and base `slog.Handler`.

### Metrics and readiness handlers

Mount the returned handlers on an HTTP server owned and managed by your application. Register checks with a context-aware function; each check receives a context bounded by `HEALTH_CHECK_TIMEOUT` (or the configured timeout).

```go
provider.RegisterCheck("database", func(ctx context.Context) error {
    return db.PingContext(ctx)
})

mux := http.NewServeMux( )
mux.Handle("GET /metrics", provider.MetricsHandler())
mux.Handle("GET /healthz", provider.HealthHandler())
// Serve mux with the application's own http.Server and lifecycle.
```

The metrics handler serves the provider's Prometheus registry. The health handler returns JSON: HTTP 200 when all registered checks pass, or HTTP 503 when a check returns an error (including a deadline error from a check that honors its timeout context ). With no checks registered, it reports healthy. The library deliberately does not create a listener.

### Context propagation and log correlation

Use the wrappers where you want client/database spans under the current action span. They preserve the supplied context so existing trace context propagates through the call.

```go
tracedClient := httptrace.WrapClient(&http.Client{Timeout: 3 * time.Second} )
tracedDB := dbtrace.Wrap(sqlDB)

traceID := obs.TraceID(ctx) // empty when there is no trace ID
spanID := obs.SpanID(ctx)   // empty when there is no span ID
provider.Logger().InfoContext(ctx, "calling dependency", "trace_id", traceID)
```

`httptrace.WrapClient` returns a cloned client with an OpenTelemetry transport; `httptrace.WrapTransport` can wrap a transport directly. `dbtrace.Wrap` instruments supported `database/sql` query, execution, statement, and transaction calls. The wrappers do not replace normal resource ownership: the application still owns its clients and underlying database.

`Provider.Logger( )` is a contextual `slog` logger. When the context contains a valid OpenTelemetry span, it adds trace IDs and trace flags; it also adds the action name when the action hook has placed it in the context. `TraceID` and `SpanID` are available without importing OpenTelemetry packages.

### Manual spans and in-memory timelines

Use `StartSpan` for work inside an action that is not covered by a wrapper. Pass an error to the returned end function so it records the error and status before closing the span.

```go
spanCtx, endSpan := obs.StartSpan(ctx, "cache.load")
value, err := loadFromCache(spanCtx)
endSpan(err)
if err != nil {
    return err
}
_ = value
```

`WithCollector` attaches a bounded collector to a context. It captures completed spans created with `StartSpan` (up to `DefaultCollectorCapacity`, currently 64); it is an in-memory aid, not an exporter or a capture of every span created by other instrumentation.

```go
ctx, collector := obs.WithCollector(ctx)
spanCtx, endSpan := obs.StartSpan(ctx, "batch.persist")
err := persist(spanCtx)
endSpan(err)

for _, span := range collector.Spans() {
    log.Printf("%s %s %s", span.Status, span.Name, span.Duration)
}
```

For a `TraceRecord` summary, call `TraceRecordFromContext(ctx, actionName, transportName, err)`. `FormatDuration` formats durations for readable output. If you add dynamic attributes directly, use the standard OpenTelemetry span from `trace.SpanFromContext(ctx)` and check `span.IsRecording()` first.

### Business events

`events.Emit` records a span named `event.<type>` and writes one structured `business_event` log record. Events are application telemetry, not Prometheus labels; keep high-cardinality values such as entity IDs in event fields rather than metric labels.

```go
events.Emit(ctx, provider.Logger(), events.BusinessEvent{
    Type:     "order.created",
    Action:   "checkout.complete",
    EntityID: "ord-42",
    Tags:     map[string]string{"channel": "web"},
    Payload:  map[string]any{"total": 129.99},
})
```

### LLM and stream telemetry

For LLM responses, implement `llm.TokenCostProvider` (`Model`, `PromptTokens`, and `CompletionTokens`). If the response also implements `CachedTokenCostProvider`, cached prompt tokens are reported separately. `llm.NewLLMHook` can add token and duration metrics, optional cost estimates, structured logs, and usage attributes to an active span. Attach `hook.AsAnyHook()` to the LLM action and pass `provider.LLMMetrics()` in its options. The metrics include prompt, cached-prompt, and completion token totals, estimated cost in USD, and call duration; cost metrics are emitted only when a cost calculator is supplied and returns a value.

For stream-producing code, populate `obs.StreamStats` with the measurements your consumer tracks and call `provider.RecordStream(ctx, stats)` to add stream attributes to the current span. The API carries stream identity, execution status/duration, emitted and delivered item counts, optional byte totals, and error details; it does not infer those counts or automatically wrap a stream iterator.

## Nexss Flow integration

The `nexssflow` adapter is part of this Go module; there is no separate module to install. Its registered bundle adds the action hook, wraps the Flow invocation in a `flow.run` root span, and provides `observability.emit_event` for structured business events. For example, a Flow file can configure the adapter and emit an event like this:

```
@require github.com/nexssp/observability {
  service_name: "checkout-demo",
  trace_exporter: "stdout",
  sample_ratio: 1.0
}
@assert: result.emitted == true
{} -> observability.emit_event @{
  type: "order.created",
  action: "checkout.complete",
  entity_id: "ord-42",
  tags: { channel: "web" },
  payload: { outcome: "accepted" }
}
```

`trace_exporter` accepts `otlp` (the default), `stdout`, or `none`. `stdout` writes spans to stderr for local inspection. The adapter accepts these per-`@require` settings:

| `@require` key | Environment fallback | Default when unset |
| --- | --- | --- |
| `service_name` | `SERVICE_NAME` | `nexss` |
| `env` | `ENV` | `local` |
| `sample_ratio` | `SAMPLE_RATIO` | `1.0`; explicit `0` disables sampling |
| `otlp_endpoint` | `OTLP_ENDPOINT` | Unset; no network trace exporter is attached |
| `otlp_insecure` | `OTLP_INSECURE` | `false`, except localhost endpoints are treated as insecure |
| `metrics_prefix` | None | Unset |
| `metrics_namespace` | None | Unset |
| `metrics_subsystem` | None | Unset |
| `trace_exporter` | None | `otlp`; also accepts `stdout` and `none` |

`metrics_prefix`, when set, takes precedence over `metrics_namespace` and `metrics_subsystem`; these metric-naming settings have no environment-variable fallback. An explicit `otlp_insecure: false` overrides an environment-derived `true`. The Flow invocation owns the bundle shutdown callback.

> **WIP — Flow parity:** Flow currently gets action instrumentation, the `flow.run` root span, exporter/configuration overrides, and the business-event action. It does **not** expose the complete Go API as Flow actions: manual spans and in-memory collection, HTTP/database wrappers, optional LLM and stream helpers, and Prometheus/readiness HTTP handlers remain Go-host or application concerns. The bundle does not start an HTTP listener. A Flow host that needs metrics or health endpoints must mount the provider handlers itself.

Runnable ecosystem examples are maintained in the [centralized Nexss examples repository](https://github.com/nexssp/examples).

## Development and tests

Install [Task](https://taskfile.dev), then run the repository's test or CI commands:

```
task test       # go test -short ./...
task test:race  # go test -race -short ./...
task ci         # lint plus uncached race tests
```

The Taskfile also provides `task test:nocache`, `task test:race:nocache`, `task test:cover`, and `task lint:nflow` (lint the bundled Flow examples/specs). To check that all Go examples build, run `task examples`.

## Related projects

- [Nexss Kernel](https://github.com/nexssp/kernel) — typed action contracts and runtime.

- [Nexss Flow](https://github.com/nexssp/flow) — Flow language and runtime.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
