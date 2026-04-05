// Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package query_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
)

// --- Mock sources ---

// mockCLISource implements SourceIdentity + CapabilityProvider + CommandRunner.
type mockCLISource struct {
	vendor   string
	platform string
	output   map[string]string // command -> output
}

func (m *mockCLISource) SourceType() string             { return "mock-cli" }
func (m *mockCLISource) ToConfig() sources.SourceConfig { return nil }
func (m *mockCLISource) DeviceVendor() string           { return m.vendor }
func (m *mockCLISource) DevicePlatform() string         { return m.platform }
func (m *mockCLISource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}
func (m *mockCLISource) RunCommand(_ context.Context, command string) (string, error) {
	out, ok := m.output[command]
	if !ok {
		return "", fmt.Errorf("unknown command %q", command)
	}
	return out, nil
}

// mockGnmiSource implements SourceIdentity + CapabilityProvider + GnmiQuerier.
type mockGnmiSource struct {
	vendor   string
	platform string
	ocPaths  bool
	native   bool
	result   *capabilities.GnmiGetResult
}

func (m *mockGnmiSource) SourceType() string             { return "mock-gnmi" }
func (m *mockGnmiSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockGnmiSource) DeviceVendor() string           { return m.vendor }
func (m *mockGnmiSource) DevicePlatform() string         { return m.platform }
func (m *mockGnmiSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{
		GnmiSnapshot:    true,
		OpenConfigPaths: m.ocPaths,
		NativeYang:      m.native,
		CLI:             false,
	}
}
func (m *mockGnmiSource) GnmiGet(_ context.Context, _ []string, _ string) (*capabilities.GnmiGetResult, error) {
	if m.result == nil {
		return nil, fmt.Errorf("no mock result")
	}
	return m.result, nil
}

// mockDualSource supports both CLI and gNMI.
type mockDualSource struct {
	vendor     string
	platform   string
	ocPaths    bool
	native     bool
	cliOutput  map[string]string
	gnmiResult *capabilities.GnmiGetResult
}

func (m *mockDualSource) SourceType() string             { return "mock-dual" }
func (m *mockDualSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockDualSource) DeviceVendor() string           { return m.vendor }
func (m *mockDualSource) DevicePlatform() string         { return m.platform }
func (m *mockDualSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{
		GnmiSnapshot:    true,
		OpenConfigPaths: m.ocPaths,
		NativeYang:      m.native,
		CLI:             true,
	}
}
func (m *mockDualSource) RunCommand(_ context.Context, command string) (string, error) {
	out, ok := m.cliOutput[command]
	if !ok {
		return "", fmt.Errorf("unknown command %q", command)
	}
	return out, nil
}
func (m *mockDualSource) GnmiGet(_ context.Context, _ []string, _ string) (*capabilities.GnmiGetResult, error) {
	if m.gnmiResult == nil {
		return nil, fmt.Errorf("no mock gnmi result")
	}
	return m.gnmiResult, nil
}

// --- Tests ---

func TestExecute_CLIFallback_SRLinux(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show interface": `ethernet-1/1 is up, speed 25G, type None
  oper-status is up`,
		},
	}

	executor := query.NewExecutor()
	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if record.RecordType != profiles.OpGetInterfaces {
		t.Errorf("RecordType = %q, want %q", record.RecordType, profiles.OpGetInterfaces)
	}
	if record.SchemaVersion != models.SchemaVersion {
		t.Errorf("SchemaVersion = %q, want %q", record.SchemaVersion, models.SchemaVersion)
	}
	if record.Source.Vendor != "nokia" {
		t.Errorf("Source.Vendor = %q, want %q", record.Source.Vendor, "nokia")
	}
	if record.Source.Transport != "cli" {
		t.Errorf("Source.Transport = %q, want %q", record.Source.Transport, "cli")
	}
	if record.Collection.Protocol != models.ProtocolCLI {
		t.Errorf("Collection.Protocol = %q, want %q", record.Collection.Protocol, models.ProtocolCLI)
	}
	if record.Quality.MappingQuality != models.MappingDerived {
		t.Errorf("Quality.MappingQuality = %q, want %q", record.Quality.MappingQuality, models.MappingDerived)
	}

	// Verify payload contains parsed interfaces.
	ifaces, ok := record.Payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("Payload type = %T, want []models.InterfaceState", record.Payload)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if ifaces[0].Name != "ethernet-1/1" {
		t.Errorf("interface name = %q, want %q", ifaces[0].Name, "ethernet-1/1")
	}
}

func TestExecute_CLIFallback_SystemVersion(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version": `Hostname          : srl1
Software Version  : v24.3.2
System Type       : 7220 IXR-D2`,
		},
	}

	executor := query.NewExecutor()
	record, err := executor.Execute(context.Background(), src, profiles.OpGetSystemVersion, "test-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	sv, ok := record.Payload.(models.SystemVersion)
	if !ok {
		t.Fatalf("Payload type = %T, want models.SystemVersion", record.Payload)
	}
	if sv.Hostname != "srl1" {
		t.Errorf("Hostname = %q, want %q", sv.Hostname, "srl1")
	}
	if sv.SoftwareVersion != "v24.3.2" {
		t.Errorf("SoftwareVersion = %q, want %q", sv.SoftwareVersion, "v24.3.2")
	}
}

func TestExecute_SROS_CLI(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "sros",
		output: map[string]string{
			"show router interface": `===============================================================================
Interface Table (Router: Base)
===============================================================================
Interface-Name                  Adm       Opr(v4/v6)  Mode     Port/SapId
   IP-Address                                            PfxState
-------------------------------------------------------------------------------
system                          Up        Up/Down     Network  -
   10.0.0.1/32                                           n/a
===============================================================================`,
		},
	}

	executor := query.NewExecutor()
	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "sros-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	ifaces, ok := record.Payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("Payload type = %T, want []models.InterfaceState", record.Payload)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if ifaces[0].Name != "system" {
		t.Errorf("interface name = %q, want %q", ifaces[0].Name, "system")
	}
}

func TestExecute_GnmiOpenConfig(t *testing.T) {
	ifaceJSON, _ := json.Marshal([]map[string]any{
		{
			"name": "ethernet-1/1",
			"state": map[string]any{
				"admin-status": "UP",
				"oper-status":  "UP",
				"type":         "ethernetCsmacd",
				"mtu":          float64(9232),
			},
		},
	})

	src := &mockGnmiSource{
		vendor:   "nokia",
		platform: "srlinux",
		ocPaths:  true,
		native:   false,
		result: &capabilities.GnmiGetResult{
			Notifications: []capabilities.GnmiNotification{
				{Path: "/openconfig-interfaces:interfaces/interface", Value: ifaceJSON},
			},
		},
	}

	executor := query.NewExecutor()
	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "gnmi-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if record.Source.Transport != "gnmi" {
		t.Errorf("Transport = %q, want %q", record.Source.Transport, "gnmi")
	}
	if record.Collection.Protocol != models.ProtocolGnmiOpenConfig {
		t.Errorf("Protocol = %q, want %q", record.Collection.Protocol, models.ProtocolGnmiOpenConfig)
	}
	if record.Quality.MappingQuality != models.MappingExact {
		t.Errorf("MappingQuality = %q, want %q", record.Quality.MappingQuality, models.MappingExact)
	}

	ifaces, ok := record.Payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("Payload type = %T, want []models.InterfaceState", record.Payload)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if ifaces[0].Name != "ethernet-1/1" {
		t.Errorf("Name = %q, want %q", ifaces[0].Name, "ethernet-1/1")
	}
	if ifaces[0].MTU != 9232 {
		t.Errorf("MTU = %d, want %d", ifaces[0].MTU, 9232)
	}
}

func TestExecute_GnmiPreferredOverCLI(t *testing.T) {
	// When source supports both gNMI OpenConfig and CLI,
	// gNMI OpenConfig should be preferred.
	ifaceJSON, _ := json.Marshal([]map[string]any{
		{"name": "from-gnmi", "state": map[string]any{"admin-status": "UP", "oper-status": "UP"}},
	})

	src := &mockDualSource{
		vendor:   "nokia",
		platform: "srlinux",
		ocPaths:  true,
		native:   true,
		cliOutput: map[string]string{
			"show interface": "ethernet-1/1 is up, speed 25G, type None\n  oper-status is up",
		},
		gnmiResult: &capabilities.GnmiGetResult{
			Notifications: []capabilities.GnmiNotification{
				{Path: "/openconfig-interfaces:interfaces/interface", Value: ifaceJSON},
			},
		},
	}

	executor := query.NewExecutor()
	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "dual-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// Should have used gNMI, not CLI.
	if record.Source.Transport != "gnmi" {
		t.Errorf("Transport = %q, want %q (gNMI should be preferred)", record.Source.Transport, "gnmi")
	}
	if record.Collection.Protocol != models.ProtocolGnmiOpenConfig {
		t.Errorf("Protocol = %q, want %q", record.Collection.Protocol, models.ProtocolGnmiOpenConfig)
	}

	ifaces, ok := record.Payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("Payload type = %T, want []models.InterfaceState", record.Payload)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "from-gnmi" {
		t.Errorf("Expected gNMI-sourced interface 'from-gnmi', got %v", ifaces)
	}
}

func TestExecute_UnknownProfile(t *testing.T) {
	src := &mockCLISource{
		vendor:   "unknown",
		platform: "unknown",
		output:   map[string]string{},
	}

	executor := query.NewExecutor()
	_, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err == nil {
		t.Fatal("expected error for unknown profile")
	}
}

func TestExecute_UnknownOperation(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output:   map[string]string{},
	}

	executor := query.NewExecutor()
	_, err := executor.Execute(context.Background(), src, "nonexistent_op", "test-src")
	if err == nil {
		t.Fatal("expected error for unknown operation")
	}
}

func TestExecute_NoIdentity(t *testing.T) {
	// Source that doesn't implement SourceIdentity.
	src := &bareSource{}

	executor := query.NewExecutor()
	_, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err == nil {
		t.Fatal("expected error when source lacks SourceIdentity")
	}
}

// bareSource implements only sources.Source (no identity/caps).
type bareSource struct{}

func (b *bareSource) SourceType() string             { return "bare" }
func (b *bareSource) ToConfig() sources.SourceConfig { return nil }
