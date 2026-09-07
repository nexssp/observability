package events_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/nexssp/observability/events"
)

func TestEvents_Emit(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	ctx := context.Background()

	events.Emit(ctx, logger, events.BusinessEvent{
		Type:     "order.created",
		Action:   "order.create",
		EntityID: "ord_100",
		Tags:     map[string]string{"env": "test"},
		Payload:  map[string]any{"total": 99.95},
	})

	output := buf.String()
	if !strings.Contains(output, "business_event") || !strings.Contains(output, "order.created") || !strings.Contains(output, "ord_100") {
		t.Fatalf("expected structured event log, got: %s", output)
	}
}
