package diagnostictools

import (
	"context"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

type mockDiagnosticSource struct{}

func (m mockDiagnosticSource) SourceType() string             { return "mock" }
func (m mockDiagnosticSource) ToConfig() sources.SourceConfig { return nil }
func (m mockDiagnosticSource) DeviceVendor() string           { return "diagnostic-vendor" }
func (m mockDiagnosticSource) DevicePlatform() string         { return "diagnostic-platform" }
func (m mockDiagnosticSource) DeviceVersion() string          { return "" }

var _ capabilities.SourceIdentity = mockDiagnosticSource{}

type mockDiagnosticProvider struct {
	source sources.Source
}

func (m mockDiagnosticProvider) GetSource(name string) (sources.Source, bool) {
	if name != "lab/device/ssh" {
		return nil, false
	}
	return m.source, true
}

func (m mockDiagnosticProvider) GetSourcesByLabels(context.Context, map[string]string) (map[string]sources.Source, error) {
	return nil, nil
}

func (m mockDiagnosticProvider) GetDevicePoolLabels() map[string]map[string]string { return nil }

func TestInvoke_RejectsUnsupportedDiagnosticTransport(t *testing.T) {
	profiles.RegisterOrReplace(&profiles.Profile{
		Vendor:   "diagnostic-vendor",
		Platform: "diagnostic-platform",
		DiagnosticCommands: map[string]profiles.DiagnosticCommandTemplate{
			profiles.OpRunPing: {
				OperationID: profiles.OpRunPing,
				Transport:   "netconf_rpc",
				Command:     "<rpc/>",
			},
		},
	})

	tool, err := Config{Name: "ping", Type: "network-ping", Source: "lab/device/ssh", spec: diagSpecs[0]}.Initialize(nil)
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	_, invokeErr := tool.Invoke(context.Background(), mockDiagnosticProvider{source: mockDiagnosticSource{}}, parameters.ParamValues{
		{Name: "target", Value: "198.51.100.1"},
		{Name: "count", Value: 5},
	}, "")
	if invokeErr == nil {
		t.Fatal("expected unsupported diagnostic transport error")
	}
	if !strings.Contains(invokeErr.Error(), "only cli is implemented") {
		t.Fatalf("error = %v, want unsupported transport message", invokeErr)
	}
}
