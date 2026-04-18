package showcoverage

import (
	"context"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

type mockCoverageSource struct {
	vendor   string
	platform string
	version  string
}

func (m mockCoverageSource) SourceType() string             { return "mock" }
func (m mockCoverageSource) ToConfig() sources.SourceConfig { return nil }
func (m mockCoverageSource) DeviceVendor() string           { return m.vendor }
func (m mockCoverageSource) DevicePlatform() string         { return m.platform }
func (m mockCoverageSource) DeviceVersion() string          { return m.version }

var _ capabilities.SourceIdentity = mockCoverageSource{}

type mockCoverageProvider struct {
	sources map[string]sources.Source
	labels  map[string]map[string]string
}

func (m mockCoverageProvider) GetSource(name string) (sources.Source, bool) {
	src, ok := m.sources[name]
	return src, ok
}

func (m mockCoverageProvider) GetSourcesByLabels(_ context.Context, matchLabels map[string]string) (map[string]sources.Source, error) {
	out := map[string]sources.Source{}
	for name, labels := range m.labels {
		matches := true
		for k, v := range matchLabels {
			if labels[k] != v {
				matches = false
				break
			}
		}
		if matches {
			out[name] = m.sources[name]
		}
	}
	return out, nil
}

func (m mockCoverageProvider) GetDevicePoolLabels() map[string]map[string]string {
	return m.labels
}

func TestInvoke_DeviceAwareSourceCoverage(t *testing.T) {
	profiles.RegisterOrReplace(&profiles.Profile{
		Vendor:   "coverage-tool",
		Platform: "router",
		Operations: map[string]profiles.OperationDescriptor{
			profiles.OpGetInterfaces: {
				OperationID: profiles.OpGetInterfaces,
				Paths:       []profiles.ProtocolPath{{Protocol: profiles.ProtocolCLI, Command: "show interfaces"}},
			},
		},
	})

	tool, err := Config{Name: "coverage", Type: kind, Source: "dc1/spine-1/default"}.Initialize(nil)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	result, toolErr := tool.Invoke(context.Background(), mockCoverageProvider{
		sources: map[string]sources.Source{
			"dc1/spine-1/default": mockCoverageSource{vendor: "coverage-tool", platform: "router"},
		},
	}, parameters.ParamValues{}, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}
	got := result.(map[string]any)
	if got["total"].(int) != 1 {
		t.Fatalf("total = %v, want 1", got["total"])
	}
	devices := got["devices"].([]DeviceCoverage)
	if devices[0].Device != "spine-1" {
		t.Fatalf("device = %q, want spine-1", devices[0].Device)
	}
}
