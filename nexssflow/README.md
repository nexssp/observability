# Observability adapter for Nexss Flow

`nexssflow` is an optional adapter package within the root `github.com/nexssp/observability` Go module; it does not have its own `go.mod`. It is built against Flow v0.20.1 and Kernel v0.27.4, matching the versions required by the root module.

`NewBundle` installs `actionhook.New(provider)` through Flow's `core.Bundle.Hooks` path. Flow v0.20.1 materializes each named `@pipeline` as an action named `pipeline.<name>`, so the existing action hook emits a named parent span around the instrumented actions in that pipeline. The bundle also exposes `observability.emit_event`, a generic business-event action backed by the root module's existing `events.Emit` API; it does not add a public root API. The provider cleanup callback is transferred through `core.Bundle.Shutdowns` and run by Flow's invocation `Host`. The adapter does not start an HTTP listener or populate `runner.Config.Hooks`.

[`examples/observability.nflow`](examples/observability.nflow) is a runnable Flow example:

```nflow
@require github.com/nexssp/observability/nexssflow

@pipeline checkout
  runtime.sleep @{ duration_ms: 20 } ->
    observability.emit_event @{
      type: "order.created",
      action: "checkout.complete",
      entity_id: "ord_100",
      tags: { channel: "web" },
      payload: { status: "created" }
    }
@end

@assert: result.emitted == true
{} -> pipeline.checkout
```

With an OTLP trace exporter configured through `OTLP_ENDPOINT`, the trace contains `action.pipeline.checkout` as the parent of the `action.runtime.sleep` and `action.observability.emit_event` spans, plus one `event.order.created` span. The event action writes one structured `business_event` record through the configured provider logger. The existing `action_latency_ms` histogram records action durations; event type, entity ID, tags, and payload are not metric labels. Mount `provider.MetricsHandler()` on a host-managed server if Prometheus scraping is needed.

## Configuration

Each key is optional. When an environment fallback is listed, it is used unless the corresponding `@require` key is supplied. The `metrics_*` keys have no environment-variable fallback in `LoadConfigFromEnv`.

| `@require` key | Environment fallback | Default when unset |
| --- | --- | --- |
| `service_name` | `SERVICE_NAME` | `nexss` |
| `env` | `ENV` | `local` |
| `sample_ratio` | `SAMPLE_RATIO` | `1.0` (explicit `0` disables sampling) |
| `otlp_endpoint` | `OTLP_ENDPOINT` | Unset; no network trace exporter is attached |
| `otlp_insecure` | `OTLP_INSECURE` | `false`, except localhost endpoints are automatically treated as insecure |
| `metrics_prefix` | None | Unset; metric names have no custom prefix |
| `metrics_namespace` | None | Unset |
| `metrics_subsystem` | None | Unset |
| `trace_exporter` | None | `otlp`; uses `otlp_endpoint` when configured, otherwise no exporter. Also accepts `stdout` and `none` |

`metrics_prefix`, when set, takes precedence over `metrics_namespace` and `metrics_subsystem`. An explicit `otlp_insecure: false` overrides an environment-derived `true`.

## Copyable host entry point

The following is a complete `main.go` example. `run` returns the Flow runner's exit code on success and reports startup or bundle-construction errors to stderr:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	flowcli "github.com/nexssp/flow/cli"
	"github.com/nexssp/flow/core"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/nexssflow"
)

func run(args []string) int {
	provider, shutdown, err := obs.Auto()
	if err != nil {
		fmt.Fprintf(os.Stderr, "initialize observability: %v\n", err)
		return 1
	}

	bundle, err := nexssflow.NewBundle(provider, shutdown)
	if err != nil {
		if shutdownErr := shutdown(context.Background()); shutdownErr != nil {
			err = errors.Join(err, fmt.Errorf("shutdown observability provider: %w", shutdownErr))
		}
		fmt.Fprintf(os.Stderr, "create observability bundle: %v\n", err)
		return 1
	}

	return flowcli.RunWithBundleFactoriesForRequirements(
		args,
		[]string{"github.com/nexssp/observability/nexssflow"},
		func(adopt func(core.Bundle) error) error {
			return adopt(bundle)
		},
	)
}

func main() {
	os.Exit(run(os.Args[1:]))
}
```

On success, Flow's invocation `Host` runs the shutdown callback after the outer invocation. If metrics or health endpoints are needed, mount `provider.MetricsHandler()` and `provider.HealthHandler()` on a host-managed server; the adapter itself never binds a port.
