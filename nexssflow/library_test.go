package nexssflow_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nexssp/flow/core"

	"github.com/nexssp/observability/nexssflow"
)

const eventLibrary = "observability"

func TestBundle_Contract(t *testing.T) {
	if _, ok := core.Lookup(nexssflow.ID); !ok {
		t.Fatalf("init() did not register bundle under %q", nexssflow.ID)
	}

	bundle := nexssflow.Bundle(nil)

	if bundle.ID != nexssflow.ID {
		t.Fatalf("ID = %q, want %q", bundle.ID, nexssflow.ID)
	}
	if len(bundle.Hooks) != 1 {
		t.Fatalf("Hooks = %d, want 1", len(bundle.Hooks))
	}
	if len(bundle.Shutdowns) != 1 {
		t.Fatalf("Shutdowns = %d, want 1", len(bundle.Shutdowns))
	}
	if len(bundle.Libraries) != 1 {
		t.Fatalf("Libraries = %d, want 1", len(bundle.Libraries))
	}
	if bundle.Libraries[0].Name != eventLibrary {
		t.Fatalf("Libraries[0].Name = %q, want %q", bundle.Libraries[0].Name, eventLibrary)
	}
}

func TestBundle_RejectsUnknownOption(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on unknown @require option")
		}
	}()
	nexssflow.Bundle(map[string]string{"unknown_option": "x"})
}

func TestBundle_MalformedSampleRatioPanicsWithFieldName(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("expected panic for malformed sample_ratio")
		}
		if message := fmt.Sprint(recovered); !strings.Contains(message, "sample_ratio") {
			t.Fatalf("panic = %q, want field name sample_ratio", message)
		}
	}()
	nexssflow.Bundle(map[string]string{"sample_ratio": "abc"})
}
