// Package events provides business telemetry event emission.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("nexss/events")

// BusinessEvent defines structured telemetry emitted during business operations.
type BusinessEvent struct {
	Type      string            `json:"type"`
	Action    string            `json:"action"`
	EntityID  string            `json:"entity_id"`
	Tags      map[string]string `json:"tags,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Payload   any               `json:"payload,omitempty"`
}

// Emit records a business telemetry event with explicit JSON handling and tracing spans.
func Emit(ctx context.Context, logger *slog.Logger, evt BusinessEvent) {
	if logger == nil {
		logger = slog.Default()
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}
	if evt.Tags == nil {
		evt.Tags = map[string]string{}
	}

	ctx, span := tracer.Start(ctx, "event."+evt.Type)
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", evt.Type),
		attribute.String("event.action", evt.Action),
		attribute.String("event.entity_id", evt.EntityID),
	)

	payloadBytes, err := json.Marshal(evt)
	if err != nil {
		logger.ErrorContext(ctx, "business_event_marshal_failed",
			"error", err,
			"event_type", evt.Type,
			"action", evt.Action,
		)
		return
	}

	logger.InfoContext(ctx, "business_event",
		slog.String("event_type", evt.Type),
		slog.String("action", evt.Action),
		slog.String("entity_id", evt.EntityID),
		slog.Any("tags", evt.Tags),
		slog.String("payload", string(payloadBytes)),
	)
}
