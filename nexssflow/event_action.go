package nexssflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/nexssp/kernel/action"

	obs "github.com/nexssp/observability"
	"github.com/nexssp/observability/events"
)

const eventLibrary = "observability"

func newEventAction(provider *obs.Provider) action.AnyAction {
	return action.New(eventLibrary+".emit_event", func(ctx context.Context, input any) (map[string]any, error) {
		fields, ok := input.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("nexssflow.emit_event: request must be an object, got %T", input)
		}

		evt, err := businessEventFromFields(fields)
		if err != nil {
			return nil, err
		}
		// events.Emit reports serialization failures through its logger rather than
		// returning an error. Validate first so a successful action means the
		// requested event can be serialized and emitted.
		if _, err := json.Marshal(evt); err != nil {
			return nil, fmt.Errorf("nexssflow.emit_event: event is not JSON-serializable: %w", err)
		}

		events.Emit(ctx, provider.Logger(), evt)
		return map[string]any{"emitted": true}, nil
	}).Description("Emit a structured business event").Build()
}

func businessEventFromFields(fields map[string]any) (events.BusinessEvent, error) {
	eventType, err := requiredString(fields, "type")
	if err != nil {
		return events.BusinessEvent{}, err
	}
	actionName, err := requiredString(fields, "action")
	if err != nil {
		return events.BusinessEvent{}, err
	}
	entityID, err := optionalString(fields, "entity_id")
	if err != nil {
		return events.BusinessEvent{}, err
	}
	tags, err := eventTags(fields["tags"])
	if err != nil {
		return events.BusinessEvent{}, err
	}

	return events.BusinessEvent{
		Type:     eventType,
		Action:   actionName,
		EntityID: entityID,
		Tags:     tags,
		Payload:  fields["payload"],
	}, nil
}

func requiredString(fields map[string]any, key string) (string, error) {
	value, ok := fields[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("nexssflow.emit_event: %q must be a non-empty string", key)
	}
	return value, nil
}

func optionalString(fields map[string]any, key string) (string, error) {
	value, exists := fields[key]
	if !exists || value == nil {
		return "", nil
	}
	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("nexssflow.emit_event: %q must be a string", key)
	}
	return text, nil
}

func eventTags(value any) (map[string]string, error) {
	if value == nil {
		return map[string]string{}, nil
	}

	tags := make(map[string]string)
	switch typed := value.(type) {
	case map[string]string:
		maps.Copy(tags, typed)
	case map[string]any:
		for key, raw := range typed {
			tag, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("nexssflow.emit_event: tag %q must be a string", key)
			}
			tags[key] = tag
		}
	default:
		return nil, errors.New("nexssflow.emit_event: tags must be an object of strings")
	}

	return tags, nil
}
