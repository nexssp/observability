package nexssflow_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/nexssp/flow/cli"
	"github.com/nexssp/flow/core"
	tracev1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"

	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/nexssflow"
)

type exportedSpan struct {
	name         string
	traceID      []byte
	spanID       []byte
	parentSpanID []byte
	serviceName  string
	environment  string
	durationMS   float64
	hasDuration  bool
}

func TestFlowAdapter_RequireInstrumentsNamedPipelineAndEventAndHostShutsDownOnce(t *testing.T) {
	source, sourceErr := os.ReadFile("examples/03_pipeline_with_children.nflow")
	if sourceErr != nil {
		t.Fatalf("read Flow example: %v", sourceErr)
	}

	// Materialize another named pipeline as a guard against accidentally
	// exporting an unnamed or duplicated pipeline span.
	flowSource := strings.Replace(
		string(source),
		"@pipeline checkout\n",
		"@pipeline unused\n  runtime.sleep @{ duration_ms: 1 }\n@end\n\n@pipeline checkout\n",
		1,
	)

	var exportedMu sync.Mutex
	var exported []exportedSpan
	var invalidTracePayload atomic.Bool
	otlpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		payload, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			invalidTracePayload.Store(true)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var batch tracev1.ExportTraceServiceRequest
		if err := proto.Unmarshal(payload, &batch); err != nil {
			invalidTracePayload.Store(true)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var requestSpans []exportedSpan
		for _, resourceSpans := range batch.GetResourceSpans() {
			for _, scopeSpans := range resourceSpans.GetScopeSpans() {
				for _, span := range scopeSpans.GetSpans() {
					got := exportedSpan{
						name:         span.GetName(),
						traceID:      append([]byte(nil), span.GetTraceId()...),
						spanID:       append([]byte(nil), span.GetSpanId()...),
						parentSpanID: append([]byte(nil), span.GetParentSpanId()...),
					}
					for _, attribute := range span.GetAttributes() {
						switch attribute.GetKey() {
						case "nexss.duration_ms":
							got.durationMS = attribute.GetValue().GetDoubleValue()
							got.hasDuration = true
						case "nexss.service.name":
							got.serviceName = attribute.GetValue().GetStringValue()
						case "nexss.env":
							got.environment = attribute.GetValue().GetStringValue()
						}
					}
					requestSpans = append(requestSpans, got)
				}
			}
		}
		exportedMu.Lock()
		exported = append(exported, requestSpans...)
		exportedMu.Unlock()
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer otlpServer.Close()

	var eventLogs bytes.Buffer
	provider, providerShutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName:   "nexssflow-integration",
		Env:           "test",
		OTLPEndpoint:  otlpServer.URL,
		OTLPInsecure:  true,
		LoggerHandler: slog.NewTextHandler(&eventLogs, nil),
	})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	var shutdownCalls atomic.Int32
	var metricsAtShutdown atomic.Value
	bundle, err := nexssflow.NewBundle(provider, func(ctx context.Context) error {
		shutdownCalls.Add(1)
		metrics := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/metrics", http.NoBody)
		provider.MetricsHandler().ServeHTTP(metrics, request)
		metricsAtShutdown.Store(metrics.Body.String())
		return providerShutdown(ctx)
	})
	if err != nil {
		_ = providerShutdown(context.Background())
		t.Fatalf("create Flow bundle: %v", err)
	}

	code := cli.RunEmbeddedWithBundleFactoriesForRequirements(
		context.Background(),
		flowSource,
		[]string{"--json"},
		[]string{"github.com/nexssp/observability/nexssflow"},
		func(adopt func(core.Bundle) error) error {
			return adopt(bundle)
		},
	)
	if code != 0 {
		t.Fatalf("Flow execution exit code = %d, want 0", code)
	}
	if got := shutdownCalls.Load(); got != 1 {
		t.Fatalf("Host shutdown callback calls = %d, want 1", got)
	}
	if invalidTracePayload.Load() {
		t.Fatal("OTLP receiver observed an invalid trace payload")
	}

	exportedMu.Lock()
	spans := append([]exportedSpan(nil), exported...)
	exportedMu.Unlock()
	rootSpan := exactlyOneSpan(t, spans, "flow.run")
	if rootSpan.serviceName != "nexssflow-integration" {
		t.Errorf("flow.run nexss.service.name = %q, want %q", rootSpan.serviceName, "nexssflow-integration")
	}
	if rootSpan.environment != "test" {
		t.Errorf("flow.run nexss.env = %q, want %q", rootSpan.environment, "test")
	}
	pipelineSpan := exactlyOneSpan(t, spans, "action.pipeline.checkout")
	sleepSpan := exactlyOneSpan(t, spans, "action.runtime.sleep")
	eventActionSpan := exactlyOneSpan(t, spans, "action.observability.emit_event")
	eventSpan := exactlyOneSpan(t, spans, "event.checkout.completed")
	if got := spansNamed(spans, "action.pipeline.unused"); got != 0 {
		t.Fatalf("uninvoked pipeline exported %d spans, want 0", got)
	}
	if !sameTraceAndParent(pipelineSpan, rootSpan) {
		t.Fatal("pipeline.checkout span is not a child of the flow.run root span")
	}
	if !sameTraceAndParent(sleepSpan, pipelineSpan) {
		t.Fatal("runtime.sleep span is not a child of the named checkout pipeline span")
	}
	if !sameTraceAndParent(eventActionSpan, pipelineSpan) {
		t.Fatal("event action span is not a child of the named checkout pipeline span")
	}
	if !sameTraceAndParent(eventSpan, eventActionSpan) {
		t.Fatal("business event span is not a child of its event action span")
	}

	if got := strings.Count(eventLogs.String(), "business_event"); got != 1 {
		t.Fatalf("business_event log records = %d, want exactly 1; output: %s", got, eventLogs.String())
	}
	for _, want := range []string{"checkout.completed", "pipeline.checkout", "ord_200"} {
		if !strings.Contains(eventLogs.String(), want) {
			t.Fatalf("business_event log missing %q; output: %s", want, eventLogs.String())
		}
	}

	metrics, ok := metricsAtShutdown.Load().(string)
	if !ok {
		t.Fatal("Flow Host shutdown callback did not capture metrics")
	}
	expectedLatencySamples := 0
	for _, span := range spans {
		if strings.HasPrefix(span.name, "action.") && span.hasDuration && span.durationMS > 0 {
			expectedLatencySamples++
		}
	}
	if expectedLatencySamples < 2 {
		t.Fatalf("positive-duration action spans = %d, want pipeline and sleeping action", expectedLatencySamples)
	}
	if got := histogramCount(metrics); got != float64(expectedLatencySamples) {
		t.Fatalf("action latency metric count = %v, want one sample for each of %d positive-duration action spans", got, expectedLatencySamples)
	}
}

func exactlyOneSpan(t *testing.T, spans []exportedSpan, name string) exportedSpan {
	t.Helper()
	var found exportedSpan
	count := 0
	for _, span := range spans {
		if span.name == name {
			found = span
			count++
		}
	}
	if count != 1 {
		t.Fatalf("exported %s spans = %d, want exactly 1", name, count)
	}
	return found
}

func spansNamed(spans []exportedSpan, name string) int {
	count := 0
	for _, span := range spans {
		if span.name == name {
			count++
		}
	}
	return count
}

func sameTraceAndParent(child, parent exportedSpan) bool {
	return len(child.traceID) > 0 && bytes.Equal(child.traceID, parent.traceID) && bytes.Equal(child.parentSpanID, parent.spanID)
}

func histogramCount(exposition string) float64 {
	for line := range strings.SplitSeq(exposition, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || !strings.HasPrefix(fields[0], "action_latency_ms") || !strings.Contains(fields[0], "_count") {
			continue
		}
		count, err := strconv.ParseFloat(fields[1], 64)
		if err == nil {
			return count
		}
	}
	return -1
}
