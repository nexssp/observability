package nexssflow

import (
	"reflect"
	"testing"

	obs "github.com/nexssp/observability"
)

func TestApplyOverrides(t *testing.T) {
	baseline := obs.Config{
		ServiceName:      "baseline-service",
		Env:              "baseline-env",
		SampleRatio:      0.75,
		SampleRatioSet:   true,
		OTLPEndpoint:     "https://baseline-collector",
		OTLPInsecure:     true,
		MetricsPrefix:    "baseline-prefix",
		MetricsNamespace: "baseline-namespace",
		MetricsSubsystem: "baseline-subsystem",
	}

	tests := []struct {
		name      string
		overrides Config
		want      func(*obs.Config)
	}{
		{
			name:      "service_name",
			overrides: Config{ServiceName: "checkout"},
			want:      func(cfg *obs.Config) { cfg.ServiceName = "checkout" },
		},
		{
			name:      "env",
			overrides: Config{Env: "production"},
			want:      func(cfg *obs.Config) { cfg.Env = "production" },
		},
		{
			name:      "sample_ratio nonzero",
			overrides: Config{SampleRatio: 0.25},
			want: func(cfg *obs.Config) {
				cfg.SampleRatio = 0.25
				cfg.SampleRatioSet = true
			},
		},
		{
			name:      "sample_ratio explicit zero",
			overrides: Config{sampleRatioSet: true},
			want: func(cfg *obs.Config) {
				cfg.SampleRatio = 0
				cfg.SampleRatioSet = true
			},
		},
		{
			name:      "otlp_endpoint",
			overrides: Config{OTLPEndpoint: "http://collector:4318"},
			want:      func(cfg *obs.Config) { cfg.OTLPEndpoint = "http://collector:4318" },
		},
		{
			name:      "otlp_insecure true",
			overrides: Config{OTLPInsecure: true},
			want:      func(cfg *obs.Config) { cfg.OTLPInsecure = true },
		},
		{
			name:      "otlp_insecure explicit false",
			overrides: Config{otlpInsecureSet: true},
			want:      func(cfg *obs.Config) { cfg.OTLPInsecure = false },
		},
		{
			name:      "metrics_prefix",
			overrides: Config{MetricsPrefix: "checkout"},
			want:      func(cfg *obs.Config) { cfg.MetricsPrefix = "checkout" },
		},
		{
			name:      "metrics_namespace",
			overrides: Config{MetricsNamespace: "commerce"},
			want:      func(cfg *obs.Config) { cfg.MetricsNamespace = "commerce" },
		},
		{
			name:      "metrics_subsystem",
			overrides: Config{MetricsSubsystem: "worker"},
			want:      func(cfg *obs.Config) { cfg.MetricsSubsystem = "worker" },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := baseline
			want := baseline
			test.want(&want)
			applyOverrides(&got, test.overrides)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("applyOverrides() = %#v, want %#v", got, want)
			}
		})
	}

	t.Run("no fields set leaves config unchanged", func(t *testing.T) {
		got := baseline
		applyOverrides(&got, Config{})
		if !reflect.DeepEqual(got, baseline) {
			t.Fatalf("applyOverrides(Config{}) changed config: got %#v, want %#v", got, baseline)
		}
	})
}

func TestDecodeOverrides_PreservesExplicitZeroSampleRatio(t *testing.T) {
	overrides, err := decodeOverrides(map[string]string{"sample_ratio": "0"})
	if err != nil {
		t.Fatalf("decode sample_ratio override: %v", err)
	}
	if !overrides.sampleRatioSet || overrides.SampleRatio != 0 {
		t.Fatalf("decoded sample_ratio = (%v, set=%v), want (0, true)", overrides.SampleRatio, overrides.sampleRatioSet)
	}

	cfg := obs.Config{SampleRatio: 1}
	applyOverrides(&cfg, overrides)
	if cfg.SampleRatio != 0 || !cfg.SampleRatioSet {
		t.Fatalf("applied sample_ratio = (%v, set=%v), want (0, true)", cfg.SampleRatio, cfg.SampleRatioSet)
	}
}

func TestDecodeOverrides_PreservesExplicitFalseOTLPInsecure(t *testing.T) {
	overrides, err := decodeOverrides(map[string]string{"otlp_insecure": "false"})
	if err != nil {
		t.Fatalf("decode otlp_insecure override: %v", err)
	}
	cfg := obs.Config{OTLPInsecure: true}
	applyOverrides(&cfg, overrides)
	if cfg.OTLPInsecure {
		t.Fatal("explicit otlp_insecure=false did not override the environment-derived true value")
	}
}
