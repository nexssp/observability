package obs_test

import (
	"context"
	"strings"
	"testing"

	obs "github.com/nexssp/observability"
)

func TestSampleRatio_DirectConfigPreservesExplicitZero(t *testing.T) {
	provider, shutdown, err := obs.NewWithShutdown(obs.Config{
		ServiceName:    "sample-ratio-zero-direct",
		SampleRatio:    0,
		SampleRatioSet: true,
	})
	if err != nil {
		t.Fatalf("create provider with explicit zero sample ratio: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	if got := provider.Config().SampleRatio; got != 0 {
		t.Fatalf("sample ratio = %v, want explicit zero", got)
	}
}

func TestSampleRatio_DirectConfigDefaultsOmittedZeroToOne(t *testing.T) {
	provider, shutdown, err := obs.NewWithShutdown(obs.Config{ServiceName: "sample-ratio-default"})
	if err != nil {
		t.Fatalf("create provider with omitted sample ratio: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()

	if got := provider.Config().SampleRatio; got != 1 {
		t.Fatalf("sample ratio = %v, want default 1", got)
	}
}

func TestSampleRatio_DirectConfigRejectsNegative(t *testing.T) {
	_, _, err := obs.NewWithShutdown(obs.Config{ServiceName: "sample-ratio-negative-direct", SampleRatio: -0.25})
	if err == nil || !strings.Contains(err.Error(), "sample ratio must be between 0 and 1") {
		t.Fatalf("negative sample ratio error = %v, want range validation error", err)
	}
}

func TestSampleRatio_EnvironmentPreservesExplicitZero(t *testing.T) {
	t.Setenv("SAMPLE_RATIO", "0")
	cfg := obs.LoadConfigFromEnv()
	if !cfg.SampleRatioSet || cfg.SampleRatio != 0 {
		t.Fatalf("environment sample ratio = (%v, set=%v), want (0, true)", cfg.SampleRatio, cfg.SampleRatioSet)
	}

	provider, shutdown, err := obs.NewWithShutdown(cfg)
	if err != nil {
		t.Fatalf("create provider from explicit-zero environment config: %v", err)
	}
	defer func() { _ = shutdown(context.Background()) }()
	if got := provider.Config().SampleRatio; got != 0 {
		t.Fatalf("provider sample ratio = %v, want explicit zero", got)
	}
}

func TestSampleRatio_EnvironmentRejectsNegative(t *testing.T) {
	t.Setenv("SAMPLE_RATIO", "-0.25")
	cfg := obs.LoadConfigFromEnv()
	if !cfg.SampleRatioSet || cfg.SampleRatio != -0.25 {
		t.Fatalf("environment sample ratio = (%v, set=%v), want (-0.25, true)", cfg.SampleRatio, cfg.SampleRatioSet)
	}
	if _, _, err := obs.NewWithShutdown(cfg); err == nil || !strings.Contains(err.Error(), "sample ratio must be between 0 and 1") {
		t.Fatalf("negative environment sample ratio error = %v, want range validation error", err)
	}
}
