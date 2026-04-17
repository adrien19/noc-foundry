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

package listdevices_test

import (
	"context"
	"testing"

	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/tools"
	listdevices "github.com/adrien19/noc-foundry/internal/tools/network/listdevices"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// mockSourceProvider implements tools.SourceProvider for testing.
type mockSourceProvider struct {
	sources    map[string]sources.Source
	poolLabels map[string]map[string]string
}

func (m *mockSourceProvider) GetSource(name string) (sources.Source, bool) {
	s, ok := m.sources[name]
	return s, ok
}

func (m *mockSourceProvider) GetSourcesByLabels(_ context.Context, matchLabels map[string]string) (map[string]sources.Source, error) {
	return nil, nil
}

func (m *mockSourceProvider) GetDevicePoolLabels() map[string]map[string]string {
	return m.poolLabels
}

func TestParseFromYaml(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want server.ToolConfigs
	}{
		{
			desc: "basic example",
			in: `
			kind: tools
			name: list_devices
			type: network-list-devices
			`,
			want: map[string]tools.ToolConfig{
				"list_devices": listdevices.Config{
					Name:         "list_devices",
					Type:         "network-list-devices",
					AuthRequired: []string{},
				},
			},
		},
		{
			desc: "with description",
			in: `
			kind: tools
			name: list_devices
			type: network-list-devices
			description: List all network devices
			`,
			want: map[string]tools.ToolConfig{
				"list_devices": listdevices.Config{
					Name:         "list_devices",
					Type:         "network-list-devices",
					Description:  "List all network devices",
					AuthRequired: []string{},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, got, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(tools.ToolAnnotations{})); diff != "" {
				t.Fatalf("incorrect parse (-want +got):\n%s", diff)
			}
		})
	}
}

func TestInitialize(t *testing.T) {
	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := tool.GetParameters()
	names := make(map[string]bool)
	for _, p := range params {
		names[p.Manifest().Name] = true
	}
	if !names["vendor"] {
		t.Error("expected 'vendor' parameter")
	}
	if !names["role"] {
		t.Error("expected 'role' parameter")
	}
}

func TestInvokeWithDevices(t *testing.T) {
	provider := &mockSourceProvider{
		poolLabels: map[string]map[string]string{
			"dc1/spine-1/default": {"vendor": "nokia", "role": "spine", "site": "dc1"},
			"dc1/spine-2/default": {"vendor": "nokia", "role": "spine", "site": "dc1"},
			"dc1/leaf-1/default":  {"vendor": "arista", "role": "leaf", "site": "dc1"},
		},
	}

	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// No filters — should return all 3 devices
	params := parameters.ParamValues{}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	total, ok := m["total"].(int)
	if !ok {
		t.Fatalf("expected total to be int, got %T", m["total"])
	}
	if total != 3 {
		t.Errorf("expected total=3, got %d", total)
	}
}

func TestInvokeWithVendorFilter(t *testing.T) {
	provider := &mockSourceProvider{
		poolLabels: map[string]map[string]string{
			"dc1/spine-1/default": {"vendor": "nokia", "role": "spine"},
			"dc1/leaf-1/default":  {"vendor": "arista", "role": "leaf"},
		},
	}

	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{
		{Name: "vendor", Value: "nokia"},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	m := result.(map[string]any)
	total := m["total"].(int)
	if total != 1 {
		t.Errorf("expected total=1 (only nokia), got %d", total)
	}
}

func TestInvokeWithRoleFilter(t *testing.T) {
	provider := &mockSourceProvider{
		poolLabels: map[string]map[string]string{
			"dc1/spine-1/default": {"vendor": "nokia", "role": "spine"},
			"dc1/spine-2/default": {"vendor": "nokia", "role": "spine"},
			"dc1/leaf-1/default":  {"vendor": "nokia", "role": "leaf"},
		},
	}

	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{
		{Name: "role", Value: "spine"},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	m := result.(map[string]any)
	total := m["total"].(int)
	if total != 2 {
		t.Errorf("expected total=2 (only spines), got %d", total)
	}
}

func TestInvokeWithNoPool(t *testing.T) {
	provider := &mockSourceProvider{
		poolLabels: nil,
	}

	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	m := result.(map[string]any)
	total := m["total"].(int)
	if total != 0 {
		t.Errorf("expected total=0 with no pool, got %d", total)
	}
}

func TestInvokeResultsSorted(t *testing.T) {
	provider := &mockSourceProvider{
		poolLabels: map[string]map[string]string{
			"dc1/z-device/default": {"vendor": "nokia"},
			"dc1/a-device/default": {"vendor": "nokia"},
			"dc1/m-device/default": {"vendor": "nokia"},
		},
	}

	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	m := result.(map[string]any)
	devices := m["devices"].([]listdevices.DeviceInfo)
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(devices))
	}
	// Devices are sorted alphabetically by group+device key.
	if devices[0].Name != "a-device" {
		t.Errorf("expected first device to be 'a-device', got %q", devices[0].Name)
	}
	if devices[2].Name != "z-device" {
		t.Errorf("expected last device to be 'z-device', got %q", devices[2].Name)
	}
}

func TestMcpManifestIsReadOnly(t *testing.T) {
	// Existing tests above use source names without pool metadata labels,
	// relying on the fallback path that parses the source name. The tests
	// below use realistic labels as injected by the device pool.

	cfg := listdevices.Config{
		Name: "list_devices",
		Type: "network-list-devices",
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	manifest := tool.McpManifest()
	if manifest.Annotations == nil {
		t.Fatal("expected annotations")
	}
	if manifest.Annotations.ReadOnlyHint == nil || !*manifest.Annotations.ReadOnlyHint {
		t.Fatal("expected ReadOnlyHint to be true")
	}
}

// TestInvokeDeduplicatesByDevice verifies that multiple source templates
// (e.g. gnmi, netconf, ssh) for the same physical device produce a single
// DeviceInfo entry with all transports listed.
func TestInvokeDeduplicatesByDevice(t *testing.T) {
	// Realistic labels as generated by the device pool for one device with
	// three templates.
	poolLabels := map[string]map[string]string{
		"lab/spine-1/gnmi": {
			"device": "spine-1", "group": "lab",
			"template": "gnmi", "type": "gnmi",
			"vendor": "nokia", "platform": "srlinux", "role": "spine",
		},
		"lab/spine-1/netconf": {
			"device": "spine-1", "group": "lab",
			"template": "netconf", "type": "netconf",
			"vendor": "nokia", "platform": "srlinux", "role": "spine",
		},
		"lab/spine-1/ssh": {
			"device": "spine-1", "group": "lab",
			"template": "ssh", "type": "ssh",
			"vendor": "nokia", "platform": "srlinux", "role": "spine",
		},
	}
	provider := &mockSourceProvider{poolLabels: poolLabels}

	cfg := listdevices.Config{Name: "list_devices", Type: "network-list-devices"}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	result, toolErr := tool.Invoke(context.Background(), provider, parameters.ParamValues{}, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	m := result.(map[string]any)
	if total := m["total"].(int); total != 1 {
		t.Errorf("expected total=1 (one device), got %d", total)
	}

	devices := m["devices"].([]listdevices.DeviceInfo)
	dev := devices[0]

	if dev.Name != "spine-1" {
		t.Errorf("expected Name='spine-1', got %q", dev.Name)
	}
	if dev.Group != "lab" {
		t.Errorf("expected Group='lab', got %q", dev.Group)
	}
	// Pool metadata labels must be stripped.
	for _, meta := range []string{"device", "group", "template", "type"} {
		if _, present := dev.Labels[meta]; present {
			t.Errorf("Labels should not contain pool metadata key %q", meta)
		}
	}
	if dev.Labels["vendor"] != "nokia" {
		t.Errorf("expected vendor='nokia', got %q", dev.Labels["vendor"])
	}
	// All three transports must be present and sorted.
	wantTransports := []string{"gnmi", "netconf", "ssh"}
	if len(dev.Transports) != 3 {
		t.Fatalf("expected 3 transports, got %v", dev.Transports)
	}
	for i, want := range wantTransports {
		if dev.Transports[i] != want {
			t.Errorf("transport[%d]: want %q, got %q", i, want, dev.Transports[i])
		}
	}
}

// TestInvokeFilterWithMultipleTemplates verifies that vendor/role filters
// work correctly when each device has multiple transport templates.
func TestInvokeFilterWithMultipleTemplates(t *testing.T) {
	poolLabels := map[string]map[string]string{
		"lab/spine-1/gnmi": {"device": "spine-1", "group": "lab", "template": "gnmi", "type": "gnmi", "vendor": "nokia", "role": "spine"},
		"lab/spine-1/ssh":  {"device": "spine-1", "group": "lab", "template": "ssh", "type": "ssh", "vendor": "nokia", "role": "spine"},
		"lab/leaf-1/gnmi":  {"device": "leaf-1", "group": "lab", "template": "gnmi", "type": "gnmi", "vendor": "arista", "role": "leaf"},
		"lab/leaf-1/ssh":   {"device": "leaf-1", "group": "lab", "template": "ssh", "type": "ssh", "vendor": "arista", "role": "leaf"},
	}
	provider := &mockSourceProvider{poolLabels: poolLabels}

	cfg := listdevices.Config{Name: "list_devices", Type: "network-list-devices"}
	tool, _ := cfg.Initialize(map[string]sources.Source{})

	// Filter by vendor=nokia: only spine-1 should match (1 device, not 2 sources).
	result, _ := tool.Invoke(context.Background(), provider, parameters.ParamValues{{Name: "vendor", Value: "nokia"}}, "")
	m := result.(map[string]any)
	if total := m["total"].(int); total != 1 {
		t.Errorf("vendor filter: expected total=1, got %d", total)
	}

	// Filter by role=spine: only spine-1 (1 device, not 2 sources).
	result, _ = tool.Invoke(context.Background(), provider, parameters.ParamValues{{Name: "role", Value: "spine"}}, "")
	m = result.(map[string]any)
	if total := m["total"].(int); total != 1 {
		t.Errorf("role filter: expected total=1, got %d", total)
	}

	// No filter: 2 physical devices.
	result, _ = tool.Invoke(context.Background(), provider, parameters.ParamValues{}, "")
	m = result.(map[string]any)
	if total := m["total"].(int); total != 2 {
		t.Errorf("no filter: expected total=2, got %d", total)
	}
}
