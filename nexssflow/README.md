# Observability adapter for Nexss Flow

This is an optional nested Go module. The root `github.com/nexssp/observability` module remains independent of Flow. The adapter requires Flow v0.15.0 and Kernel v0.26.1.

`NewBundle` installs `actionhook.New(provider)` through Flow's `core.Bundle.AtomAdvise` hook path. Flow v0.15.0 materializes each named `@pipeline` as an action named `pipeline.<name>`, so the existing action hook emits a named parent span around the instrumented actions in that pipeline. The bundle also exposes `nexss_observability.emit_event`, a generic business-event action backed by the root module's existing `events.Emit` API; it does not add a public root API. The provider cleanup callback is transferred through `core.Bundle.Shutdowns` and run by Flow's invocation `Host`. The adapter does not start an HTTP listener or populate `runner.Config.Hooks`.

[`examples/observability.nflow`](examples/observability.nflow) is a runnable Flow example:

```nflow
@require github.com/nexssp/observability/nexssflow

@pipeline checkout
  runtime.sleep @{ duration_ms: 20 } ->
    nexss_observability.emit_event @{
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

With an OTLP trace exporter configured through `OTLP_ENDPOINT`, the trace contains `action.pipeline.checkout` as the parent of the `action.runtime.sleep` and `action.nexss_observability.emit_event` spans, plus one `event.order.created` span. The event action writes one structured `business_event` record through the configured provider logger. The existing `action_latency_ms` histogram records action durations; event type, entity ID, tags, and payload are not metric labels. Mount `provider.MetricsHandler()` on a host-managed server if Prometheus scraping is needed.

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
		[]string{nexssflow.Requirement},
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
