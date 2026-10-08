package main

import (
	"context"
	"fmt"
	"io"
	"iter"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/nexssp/kernel/action"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/actionhook"
)

type Transaction struct {
	ID        int     `json:"id"`
	Account   string  `json:"account"`
	AmountUSD float64 `json:"amount_usd"`
}

func main() {
	// Initialize telemetry provider
	provider, shutdown, err := obs.Auto()
	if err != nil {
		log.Printf("failed to init telemetry: %v", err)
		return
	}
	defer func() {
		if shutdownErr := shutdown(context.Background()); shutdownErr != nil {
			log.Printf("shutdown observability: %v", shutdownErr)
		}
	}()

	// Enable in-memory trace collection for visual waterfall display
	ctx, collector := obs.WithCollector(context.Background())

	telemetryHook := actionhook.New(provider)

	// StreamAction: high-throughput financial stream
	streamAction := action.NewStream("finance.transaction_stream", func(ctx context.Context, total int) (iter.Seq2[Transaction, error], error) {
		current := 0
		return action.StreamFromFunc(func() (Transaction, error) {
			if ctx.Err() != nil {
				return Transaction{}, ctx.Err()
			}
			if current >= total {
				return Transaction{}, io.EOF
			}
			current++

			amount := 49.99
			if current == 250 {
				amount = 25000.00 // Suspicious high-value transaction
			}

			return Transaction{
				ID:        current,
				Account:   fmt.Sprintf("ACC-%04d", current%50),
				AmountUSD: amount,
			}, nil
		}), nil
	}).
		AnyHook(telemetryHook).
		HookStartEvent(func() { fmt.Println("🚀 [Stream] Ingestion gate opened") }).
		HookCancelEvent(func() { fmt.Println("🛑 [Stream] Consumer interrupted early") }).
		HookSuccessEvent(func() { fmt.Println("🏁 [Stream] Ingestion completed cleanly (EOF reached)") })

	fmt.Println("🌊 Processing 500 financial transactions with live OTel tracking...")

	startTime := time.Now()
	seq, err := streamAction.Do(ctx, 500)
	if err != nil {
		log.Printf("stream init failed: %v", err)
		return
	}

	span := trace.SpanFromContext(ctx)
	totalDelivered := int64(0)
	totalAmount := 0.0

	for tx, streamErr := range seq {
		if streamErr != nil {
			log.Printf("stream error: %v", streamErr)
			break
		}

		totalDelivered++
		totalAmount += tx.AmountUSD

		// Dynamic Span Event: annotate span on anomalies
		if tx.AmountUSD > 10000.0 {
			if span.IsRecording() {
				span.AddEvent("fraud_anomaly_detected", trace.WithAttributes(
					attribute.Int("transaction.id", tx.ID),
					attribute.Float64("transaction.amount_usd", tx.AmountUSD),
				))
			}
			fmt.Printf("⚠️  [Audit] High-value anomaly detected: TX #%d ($%.2f)\n", tx.ID, tx.AmountUSD)
		}

		// Micro-SubSpan: trace batched persistence every 100 records
		if totalDelivered%100 == 0 {
			flushBatch(ctx, totalDelivered)
		}
	}

	totalDuration := time.Since(startTime)

	// Emit structured StreamStats
	stats := obs.StreamStats{
		Stream: obs.StreamIdentity{
			Name:    "finance.transaction_stream",
			TraceID: obs.TraceID(ctx),
		},
		Exec: obs.StreamExecution{
			StartedAt: startTime,
			Duration:  totalDuration,
			Status:    obs.StreamStatusCompleted,
		},
		Items: obs.StreamItems{
			Emitted:   totalDelivered,
			Delivered: totalDelivered,
		},
	}

	provider.Logger().InfoContext(ctx, "stream execution completed",
		"stream", stats.Stream.Name,
		"trace_id", stats.Stream.TraceID,
		"items", stats.Items.Delivered,
		"total_usd", totalAmount,
		"duration_ms", stats.Exec.Duration.Milliseconds(),
	)

	// Print visual timeline of recorded sub-spans
	printTraceWaterfall(collector)

	// Dump live Prometheus metrics recorded during this run
	printPrometheusMetrics(provider)
}

func flushBatch(ctx context.Context, batchCount int64) {
	_, endSpan := obs.StartSpan(ctx, "db.batch_flush", fmt.Sprintf("items_up_to=%d", batchCount))
	defer endSpan()

	time.Sleep(2 * time.Millisecond) // Simulated database write
}

func printTraceWaterfall(collector *obs.TraceCollector) {
	spans := collector.Spans()
	if len(spans) == 0 {
		return
	}

	fmt.Println("\n📊 [OTel In-Memory Trace Waterfall]")
	fmt.Println(strings.Repeat("─", 75))
	for _, s := range spans {
		detail := ""
		if s.Detail != "" {
			detail = fmt.Sprintf(" (%s)", s.Detail)
		}
		fmt.Printf("  ├── [%s] %-24s duration=%-10s offset=%s%s\n",
			s.Status, s.Name, s.Duration, obs.FormatDuration(time.Duration(s.StartOffsetNs)), detail)
	}
	fmt.Println(strings.Repeat("─", 75))
}

func printPrometheusMetrics(provider *obs.Provider) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", http.NoBody)
	provider.MetricsHandler().ServeHTTP(rec, req)

	fmt.Println("\n📈 [Live Prometheus Metrics]")
	fmt.Println(strings.Repeat("─", 75))
	for line := range strings.SplitSeq(rec.Body.String(), "\n") {
		if strings.HasPrefix(line, "action_") && !strings.HasPrefix(line, "#") {
			fmt.Printf("  %s\n", line)
		}
	}
	fmt.Println(strings.Repeat("─", 75))
}
