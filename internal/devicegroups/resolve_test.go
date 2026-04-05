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

package devicegroups

import (
	"context"
	"fmt"
	"testing"

	"github.com/goccy/go-yaml"
)

// mockProvider is a test inventory provider that returns a fixed device list.
type mockProvider struct {
	devices []DeviceEntry
	err     error
}

func (m *mockProvider) Discover(_ context.Context) ([]DeviceEntry, error) {
	return m.devices, m.err
}

func (m *mockProvider) ProviderType() string { return "mock" }

// mockProviderConfig is a test config that produces a mockProvider.
type mockProviderConfig struct {
	provider *mockProvider
}

func (m *mockProviderConfig) ProviderConfigType() string { return "mock" }
func (m *mockProviderConfig) Initialize(_ context.Context) (InventoryProvider, error) {
	return m.provider, nil
}

func init() {
	RegisterProvider("mock", func(ctx context.Context, decoder *yaml.Decoder) (InventoryProviderConfig, error) {
		var raw struct {
			Type    string        `yaml:"type"`
			Devices []DeviceEntry `yaml:"devices"`
			Error   string        `yaml:"error,omitempty"`
		}
		if err := decoder.DecodeContext(ctx, &raw); err != nil {
			return nil, err
		}
		var discoverErr error
		if raw.Error != "" {
			discoverErr = fmt.Errorf("%s", raw.Error)
		}
		return &mockProviderConfig{
			provider: &mockProvider{devices: raw.Devices, err: discoverErr},
		}, nil
	})
}

func TestResolveDevicesStaticOnly(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
			{Name: "r2", Host: "10.0.0.2"},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("ResolveDevices() error: %v", err)
	}

	devices := cfg.AllDevices()
	if len(devices) != 2 {
		t.Fatalf("AllDevices() returned %d, want 2", len(devices))
	}
	if devices[0].Name != "r1" || devices[1].Name != "r2" {
		t.Errorf("unexpected devices: %v", devices)
	}
}

func TestResolveDevicesProviderOnly(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				"devices": []any{
					map[string]any{"name": "r1", "host": "10.0.0.1"},
					map[string]any{"name": "r2", "host": "10.0.0.2"},
				},
			},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("ResolveDevices() error: %v", err)
	}

	devices := cfg.AllDevices()
	if len(devices) != 2 {
		t.Fatalf("AllDevices() returned %d, want 2", len(devices))
	}
}

func TestResolveDevicesMixed(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "static-r1", Host: "10.0.0.1"},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				"devices": []any{
					map[string]any{"name": "provider-r1", "host": "10.0.0.2"},
				},
			},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("ResolveDevices() error: %v", err)
	}

	devices := cfg.AllDevices()
	if len(devices) != 2 {
		t.Fatalf("AllDevices() returned %d, want 2", len(devices))
	}
	if devices[0].Name != "static-r1" {
		t.Errorf("devices[0].Name = %q, want %q", devices[0].Name, "static-r1")
	}
	if devices[1].Name != "provider-r1" {
		t.Errorf("devices[1].Name = %q, want %q", devices[1].Name, "provider-r1")
	}
}

func TestResolveDevicesDuplicateStaticName(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
			{Name: "r1", Host: "10.0.0.2"},
		},
	}

	err := cfg.ResolveDevices(context.Background())
	if err == nil {
		t.Fatal("expected error for duplicate static device name, got nil")
	}
}

func TestResolveDevicesDuplicateAcrossProviders(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				"devices": []any{
					map[string]any{"name": "r1", "host": "10.0.0.2"}, // same name as static
				},
			},
		},
	}

	err := cfg.ResolveDevices(context.Background())
	if err == nil {
		t.Fatal("expected error for duplicate device name across static and provider, got nil")
	}
}

func TestResolveDevicesProviderError(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		InventoryProviders: []map[string]any{
			{
				"type":  "mock",
				"error": "connection refused",
			},
		},
	}

	err := cfg.ResolveDevices(context.Background())
	if err == nil {
		t.Fatal("expected error from failing provider, got nil")
	}
}

func TestResolveDevicesNoSources(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		// No devices and no inventory providers producing devices
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				// empty devices list
			},
		},
	}

	err := cfg.ResolveDevices(context.Background())
	if err == nil {
		t.Fatal("expected error for empty device list, got nil")
	}
}

func TestResolveDevicesIdempotent(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("first ResolveDevices() error: %v", err)
	}
	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("second ResolveDevices() error: %v", err)
	}

	devices := cfg.AllDevices()
	if len(devices) != 1 {
		t.Fatalf("AllDevices() returned %d, want 1", len(devices))
	}
}

func TestAllDevicesBeforeResolve(t *testing.T) {
	cfg := &Config{
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
		},
	}

	// Before ResolveDevices, AllDevices returns static list
	devices := cfg.AllDevices()
	if len(devices) != 1 {
		t.Fatalf("AllDevices() before resolve returned %d, want 1", len(devices))
	}
}

func TestResolveDevicesUnknownProvider(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "nonexistent-provider",
			},
		},
	}

	err := cfg.ResolveDevices(context.Background())
	if err == nil {
		t.Fatal("expected error for unknown provider type, got nil")
	}
}

func TestExpandToSourceMapsWithResolvedDevices(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				"devices": []any{
					map[string]any{"name": "r1", "host": "10.0.0.1"},
				},
			},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("ResolveDevices() error: %v", err)
	}

	sources, err := cfg.ExpandToSourceMaps()
	if err != nil {
		t.Fatalf("ExpandToSourceMaps() error: %v", err)
	}

	if len(sources) != 1 {
		t.Fatalf("ExpandToSourceMaps() returned %d sources, want 1", len(sources))
	}

	key := "lab/r1/ssh"
	src, ok := sources[key]
	if !ok {
		t.Fatalf("missing source %q", key)
	}
	if src["host"] != "10.0.0.1" {
		t.Errorf("source host = %v, want 10.0.0.1", src["host"])
	}
}

func TestBuildLabelIndexWithResolvedDevices(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh", "vendor": "nokia"},
		},
		Devices: []DeviceEntry{
			{Name: "static-r1", Host: "10.0.0.1", Labels: map[string]string{"role": "spine"}},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				"devices": []any{
					map[string]any{
						"name": "dynamic-r1",
						"host": "10.0.0.2",
						"labels": map[string]any{
							"role": "leaf",
						},
					},
				},
			},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("ResolveDevices() error: %v", err)
	}

	labels := cfg.BuildLabelIndex()
	if len(labels) != 2 {
		t.Fatalf("BuildLabelIndex() returned %d entries, want 2", len(labels))
	}

	staticLabels, ok := labels["lab/static-r1/ssh"]
	if !ok {
		t.Fatal("missing labels for lab/static-r1/ssh")
	}
	if staticLabels["role"] != "spine" {
		t.Errorf("static device role = %q, want %q", staticLabels["role"], "spine")
	}

	dynamicLabels, ok := labels["lab/dynamic-r1/ssh"]
	if !ok {
		t.Fatal("missing labels for lab/dynamic-r1/ssh")
	}
	if dynamicLabels["role"] != "leaf" {
		t.Errorf("dynamic device role = %q, want %q", dynamicLabels["role"], "leaf")
	}
}

func TestMultipleProviders(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		InventoryProviders: []map[string]any{
			{
				"type": "mock",
				"devices": []any{
					map[string]any{"name": "from-provider-1", "host": "10.0.0.1"},
				},
			},
			{
				"type": "mock",
				"devices": []any{
					map[string]any{"name": "from-provider-2", "host": "10.0.0.2"},
				},
			},
		},
	}

	if err := cfg.ResolveDevices(context.Background()); err != nil {
		t.Fatalf("ResolveDevices() error: %v", err)
	}

	devices := cfg.AllDevices()
	if len(devices) != 2 {
		t.Fatalf("AllDevices() returned %d, want 2", len(devices))
	}
}
