package obs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type CompletedSpan struct {
	Name          string `json:"name"`
	Status        string `json:"status"` // "OK" or "ERROR"
	StartOffsetUs int64  `json:"start_offset_us"`
	StartOffsetNs int64  `json:"start_offset_ns"`
	DurationUs    int64  `json:"duration_us"`
	DurationNs    int64  `json:"duration_ns"`
	Duration      string `json:"duration"`
	Detail        string `json:"detail,omitempty"`
}

type TraceRecord struct {
	TraceID         string          `json:"trace_id"`
	Action          string          `json:"action"`
	Transport       string          `json:"transport"`
	Timestamp       time.Time       `json:"timestamp"`
	TotalDurationUs int64           `json:"total_duration_us"`
	Duration        string          `json:"duration"`
	Status          string          `json:"status"`
	Spans           []CompletedSpan `json:"spans"`
}

type traceContextKey struct{}

type activeTraceCollector struct {
	mu        sync.Mutex
	rootStart time.Time
	spans     []CompletedSpan
}

func withCollector(ctx context.Context) (context.Context, *activeTraceCollector) {
	c := &activeTraceCollector{
		rootStart: time.Now(),
		spans:     make([]CompletedSpan, 0, 16),
	}
	return context.WithValue(ctx, traceContextKey{}, c), c
}

func collectorFrom(ctx context.Context) *activeTraceCollector {
	c, _ := ctx.Value(traceContextKey{}).(*activeTraceCollector)
	return c
}

func (c *activeTraceCollector) Spans() []CompletedSpan {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]CompletedSpan, len(c.spans))
	copy(out, c.spans)
	return out
}

func WithCollector(ctx context.Context) (context.Context, *activeTraceCollector) {
	return withCollector(ctx)
}

// StartSpan creates a child OpenTelemetry span with start offsets for visual timelines.
func StartSpan(ctx context.Context, name string, detail ...string) (context.Context, func(err ...error)) {
	start := time.Now()
	det := ""
	if len(detail) > 0 {
		det = detail[0]
	}

	tracer := otel.Tracer("nexss/obs")
	attrs := []attribute.KeyValue{
		attribute.String("span.name", name),
	}
	if det != "" {
		attrs = append(attrs, attribute.String("span.detail", det))
	}

	childCtx, span := tracer.Start(ctx, name,
		trace.WithAttributes(attrs...),
		trace.WithSpanKind(trace.SpanKindInternal),
	)

	col := collectorFrom(childCtx)
	var offsetUs, offsetNs int64
	if col != nil {
		offsetDuration := start.Sub(col.rootStart)
		offsetUs = offsetDuration.Microseconds()
		offsetNs = offsetDuration.Nanoseconds()
	}

	return childCtx, func(err ...error) {
		dur := time.Since(start)
		status := "OK"

		if len(err) > 0 && err[0] != nil {
			status = "ERROR"
			span.RecordError(err[0])
			span.SetStatus(codes.Error, err[0].Error())
			if det == "" {
				det = err[0].Error()
			} else {
				det = fmt.Sprintf("%s (Error: %v)", det, err[0])
			}
		} else {
			span.SetStatus(codes.Ok, "")
		}
		span.End()

		cs := CompletedSpan{
			Name:          name,
			Status:        status,
			StartOffsetUs: offsetUs,
			StartOffsetNs: offsetNs,
			Duration:      FormatDuration(dur),
			DurationUs:    dur.Microseconds(),
			DurationNs:    dur.Nanoseconds(),
			Detail:        det,
		}

		if col != nil {
			col.mu.Lock()
			col.spans = append(col.spans, cs)
			col.mu.Unlock()
		}
	}
}

func FormatDuration(d time.Duration) string {
	if d < time.Microsecond {
		return fmt.Sprintf("%dns", d.Nanoseconds())
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Nanoseconds())/1000.0)
	}
	if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// TraceRecordFromContext builds a complete, machine-readable trace record (timeline) from the context.
// Returns false if there was no active collector in the context (WithCollector).
func TraceRecordFromContext(ctx context.Context, actionName, transportName string, err error) (TraceRecord, bool) {
	col := collectorFrom(ctx)
	if col == nil {
		return TraceRecord{}, false
	}

	span := trace.SpanFromContext(ctx)
	traceID := ""
	if span.SpanContext().IsValid() {
		traceID = span.SpanContext().TraceID().String()
	}

	col.mu.Lock()
	spans := make([]CompletedSpan, len(col.spans))
	copy(spans, col.spans)
	rootStart := col.rootStart
	col.mu.Unlock()

	totalDuration := time.Since(rootStart)
	status := "OK"
	if err != nil {
		status = "ERROR"
	}

	return TraceRecord{
		TraceID:         traceID,
		Action:          actionName,
		Transport:       transportName,
		Timestamp:       rootStart.UTC(),
		TotalDurationUs: totalDuration.Microseconds(),
		Duration:        FormatDuration(totalDuration),
		Status:          status,
		Spans:           spans,
	}, true
}
