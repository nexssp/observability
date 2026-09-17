package obs

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nexssp/kernel/ai/dag"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const DefaultCollectorCapacity = 64

type CompletedSpan struct {
	Name          string `json:"name"`
	Status        string `json:"status"` // "OK", "ERROR", or "SUSPENDED"
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

// TraceCollector is a true circular ring buffer with index arithmetic
// (zero slice reallocations).
type TraceCollector struct {
	mu        sync.Mutex
	rootStart time.Time
	spans     []CompletedSpan
	head      int
	count     int
	capacity  int
}

func withCollector(ctx context.Context) (context.Context, *TraceCollector) {
	c := &TraceCollector{
		rootStart: time.Now(),
		spans:     make([]CompletedSpan, DefaultCollectorCapacity),
		capacity:  DefaultCollectorCapacity,
	}

	return context.WithValue(ctx, traceContextKey{}, c), c
}

func collectorFrom(ctx context.Context) *TraceCollector {
	c, _ := ctx.Value(traceContextKey{}).(*TraceCollector)

	return c
}

func (c *TraceCollector) Spans() []CompletedSpan {
	if c == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.count == 0 {
		return []CompletedSpan{}
	}

	out := make([]CompletedSpan, c.count)
	if c.count < c.capacity {
		copy(out, c.spans[:c.count])

		return out
	}

	// Unroll circular buffer: oldest to newest
	copied := copy(out, c.spans[c.head:])
	copy(out[copied:], c.spans[:c.head])

	return out
}

// WithCollector attaches a TraceCollector to the context for capturing spans.
func WithCollector(ctx context.Context) (context.Context, *TraceCollector) {
	return withCollector(ctx)
}

func StartSpan(ctx context.Context, name string, detail ...string) (spanCtx context.Context, endSpan func(err ...error)) {
	start := time.Now()
	det := ""
	if len(detail) > 0 {
		det = detail[0]
	}

	tracer := otel.Tracer("nexss/obs")
	attrs := []attribute.KeyValue{attribute.String("span.name", name)}
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
			if errors.Is(err[0], dag.ErrSuspended) {
				status = "SUSPENDED"
				span.SetStatus(codes.Ok, "suspended for human intervention")
				span.SetAttributes(
					attribute.Bool("nexss.suspended", true),
					attribute.String("nexss.status", "suspended"),
				)
			} else {
				status = "ERROR"
				span.RecordError(err[0])
				span.SetStatus(codes.Error, err[0].Error())
			}

			if det == "" {
				det = err[0].Error()
			} else {
				det = fmt.Sprintf("%s (Status: %s, Cause: %v)", det, status, err[0])
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
			col.spans[col.head] = cs
			col.head = (col.head + 1) % col.capacity
			if col.count < col.capacity {
				col.count++
			}
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
	spans := make([]CompletedSpan, col.count)
	if col.count < col.capacity {
		copy(spans, col.spans[:col.count])
	} else {
		copied := copy(spans, col.spans[col.head:])
		copy(spans[copied:], col.spans[:col.head])
	}
	rootStart := col.rootStart
	col.mu.Unlock()

	totalDuration := time.Since(rootStart)
	status := "OK"
	if err != nil {
		if errors.Is(err, dag.ErrSuspended) {
			status = "SUSPENDED"
		} else {
			status = "ERROR"
		}
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
