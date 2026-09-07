package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nexssp/kernel/action"
	"github.com/nexssp/kernel/xctx"
	"github.com/nexssp/kernel/xerr"
	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
	"github.com/nexssp/observability/events"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	provider, shutdown, _ := obs.Auto()
	defer func() { _ = shutdown(context.Background()) }()

	logger := provider.Logger()

	processData := action.New("data.process", func(ctx context.Context, input string) (string, error) {

		// 1. Dynamic Span Enrichment
		// Safely extract the running span and add dynamic runtime variables without breaking boundaries
		if span := trace.SpanFromContext(ctx); span.IsRecording() {
			span.SetAttributes(
				attribute.String("payload.type", "string"),
				attribute.Int("payload.size_bytes", len(input)),
			)
		}

		// 2. Micro-Measurement Sub-Span (with panic safety)
		// We execute custom inline parsing inside a dedicated, isolated sub-span
		parsed, err := func(c context.Context) (res string, e error) {
			spanCtx, endSpan := obs.StartSpan(c, "parser.heavy_computation")
			defer func() {
				if r := recover(); r != nil {
					e = xerr.PanicRecovery(r) // Map unexpected panic safely
				}
				endSpan(e) // Safely close and record status
			}()

			res, e = executeHeavyParsing(spanCtx, input)
			return res, e
		}(ctx)

		if err != nil {
			return "", err
		}

		// 3. Non-Blocking Async Trace Detachment
		// We safely spawn a non-blocking background transaction that executes under its own lifecycle,
		// yet remains fully correlated in our OpenObserve tracing dashboard
		asyncCtx := xctx.CloneForAsync(ctx)
		go func() {
			bgCtx, endSpan := obs.StartSpan(asyncCtx, "async.background_cleanup")
			defer endSpan()

			time.Sleep(50 * time.Millisecond) // Simulate slow cleanup
			logger.InfoContext(bgCtx, "background cleanup finished successfully")
		}()

		// 4. Emit Business Event
		events.Emit(ctx, logger, events.BusinessEvent{
			Type:     "data.computation_completed",
			Action:   "data.process",
			EntityID: "comp_101",
			Payload:  map[string]any{"parsed_len": len(parsed)},
		})

		return parsed, nil
	}).
		AnyHook(actionhook.New(provider)).
		Build()

	fmt.Println("Executing advanced pipeline...")
	res, _ := processData.Do(context.Background(), "heavy payload here")
	fmt.Println("Final Result:", res)
	time.Sleep(100 * time.Millisecond) // Allow background async routine to log before exiting
}

func executeHeavyParsing(ctx context.Context, input string) (string, error) {
	if input == "" {
		return "", errors.New("empty payload")
	}
	return "processed_" + input, nil
}
