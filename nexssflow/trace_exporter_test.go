package nexssflow

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestBuildTraceProcessor(t *testing.T) {
	tests := []struct {
		name       string
		kind       string
		wantNil    bool
		wantErrMsg string
	}{
		{name: "default", kind: "", wantNil: true},
		{name: "otlp", kind: "otlp", wantNil: true},
		{name: "stdout", kind: "stdout"},
		{name: "none", kind: "none"},
		{name: "unknown value", kind: "invalid", wantNil: true, wantErrMsg: "unknown trace_exporter"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor, err := buildTraceProcessor(test.kind)
			if test.wantErrMsg != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErrMsg) {
					t.Fatalf("buildTraceProcessor(%q) error = %v, want error containing %q", test.kind, err, test.wantErrMsg)
				}
				if processor != nil {
					t.Fatalf("buildTraceProcessor(%q) returned processor %T with an error", test.kind, processor)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildTraceProcessor(%q): %v", test.kind, err)
			}
			if test.wantNil && processor != nil {
				t.Fatalf("buildTraceProcessor(%q) = %T, want nil", test.kind, processor)
			}
			if !test.wantNil && processor == nil {
				t.Fatalf("buildTraceProcessor(%q) = nil, want a processor", test.kind)
			}
		})
	}
}

func TestBuildTraceProcessorStdoutExportsSpans(t *testing.T) {
	output := exportOneSpanWithProcessor(t, "stdout")
	if !strings.Contains(output, "stdout-test-span") {
		t.Fatalf("stdout exporter output = %q, want span name", output)
	}
}

func TestBuildTraceProcessorNoneExportsNoOutput(t *testing.T) {
	output := exportOneSpanWithProcessor(t, "none")
	if output != "" {
		t.Fatalf("none exporter output = %q, want no output", output)
	}
}

func exportOneSpanWithProcessor(t *testing.T, kind string) string {
	t.Helper()

	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	originalStderr := os.Stderr
	os.Stderr = writePipe
	defer func() {
		os.Stderr = originalStderr
		_ = writePipe.Close()
		_ = readPipe.Close()
	}()

	processor, err := buildTraceProcessor(kind)
	if err != nil {
		t.Fatalf("build %s processor: %v", kind, err)
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(processor))
	_, span := provider.Tracer("nexssflow.test").Start(context.Background(), "stdout-test-span")
	span.End()
	if err := provider.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown %s test provider: %v", kind, err)
	}

	if err := writePipe.Close(); err != nil {
		t.Fatalf("close stderr pipe: %v", err)
	}
	os.Stderr = originalStderr
	output, err := io.ReadAll(readPipe)
	if err != nil {
		t.Fatalf("read exporter output: %v", err)
	}
	return string(output)
}
