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

package server

import (
	"context"
	"testing"

	_ "github.com/adrien19/noc-foundry/internal/sources/ssh"
)

func TestDeviceGroupYAMLParsing(t *testing.T) {
	yamlInput := `
kind: deviceGroups
name: nokia-lab
type: static
sourceTemplates:
  ssh:
    type: ssh
    port: 22
    username: admin
    password: admin
    vendor: nokia
    platform: srlinux
    timeout: 30s
devices:
  - name: spine-1
    host: 10.0.0.1
    labels:
      role: spine
      site: east
  - name: leaf-1
    host: 10.0.0.2
    labels:
      role: leaf
      site: east
`
	_, _, _, _, _, _, _, dgConfigs, _, err := UnmarshalResourceConfig(context.Background(), []byte(yamlInput))
	if err != nil {
		t.Fatalf("UnmarshalResourceConfig() error: %v", err)
	}

	if len(dgConfigs) != 1 {
		t.Fatalf("got %d device groups, want 1", len(dgConfigs))
	}

	cfg := dgConfigs[0]
	if cfg.Name != "nokia-lab" {
		t.Errorf("name = %q, want %q", cfg.Name, "nokia-lab")
	}
	if cfg.Type != "static" {
		t.Errorf("type = %q, want %q", cfg.Type, "static")
	}
	if len(cfg.SourceTemplates) != 1 {
		t.Errorf("sourceTemplates count = %d, want 1", len(cfg.SourceTemplates))
	}
	if len(cfg.Devices) != 2 {
		t.Errorf("devices count = %d, want 2", len(cfg.Devices))
	}

	// Verify template fields
	sshTemplate, ok := cfg.SourceTemplates["ssh"]
	if !ok {
		t.Fatal("missing ssh template")
	}
	if sshTemplate["type"] != "ssh" {
		t.Errorf("ssh template type = %v, want ssh", sshTemplate["type"])
	}

	// Verify devices
	if cfg.Devices[0].Name != "spine-1" {
		t.Errorf("device[0].name = %q, want %q", cfg.Devices[0].Name, "spine-1")
	}
	if cfg.Devices[0].Labels["role"] != "spine" {
		t.Errorf("device[0].labels.role = %q, want %q", cfg.Devices[0].Labels["role"], "spine")
	}
}

func TestDeviceGroupMixedWithSources(t *testing.T) {
	yamlInput := `
kind: sources
name: standalone-source
type: ssh
host: 10.0.0.99
port: 22
username: admin
password: admin
vendor: nokia
platform: srlinux
timeout: 30s
---
kind: deviceGroups
name: lab-group
type: static
sourceTemplates:
  ssh:
    type: ssh
    port: 22
    username: admin
    password: admin
    vendor: nokia
    platform: srlinux
    timeout: 30s
devices:
  - name: r1
    host: 10.0.0.1
`
	sourceConfigs, _, _, _, _, _, _, dgConfigs, _, err := UnmarshalResourceConfig(context.Background(), []byte(yamlInput))
	if err != nil {
		t.Fatalf("UnmarshalResourceConfig() error: %v", err)
	}

	if len(sourceConfigs) != 1 {
		t.Errorf("got %d source configs, want 1", len(sourceConfigs))
	}
	if _, ok := sourceConfigs["standalone-source"]; !ok {
		t.Error("missing standalone-source in source configs")
	}
	if len(dgConfigs) != 1 {
		t.Errorf("got %d device groups, want 1", len(dgConfigs))
	}
	if dgConfigs[0].Name != "lab-group" {
		t.Errorf("device group name = %q, want %q", dgConfigs[0].Name, "lab-group")
	}
}

func TestDeviceGroupWithInventoryProviders(t *testing.T) {
	yamlInput := `
kind: deviceGroups
name: dynamic-lab
sourceTemplates:
  ssh:
    type: ssh
    port: 22
    username: admin
    password: admin
    vendor: nokia
    platform: srlinux
    timeout: 30s
inventoryProviders:
  - type: netbox
    url: https://netbox.example.com
    token: my-token
    filters:
      site: dc1
    labels:
      env: production
`
	_, _, _, _, _, _, _, dgConfigs, _, err := UnmarshalResourceConfig(context.Background(), []byte(yamlInput))
	if err != nil {
		t.Fatalf("UnmarshalResourceConfig() error: %v", err)
	}

	if len(dgConfigs) != 1 {
		t.Fatalf("got %d device groups, want 1", len(dgConfigs))
	}

	cfg := dgConfigs[0]
	if cfg.Name != "dynamic-lab" {
		t.Errorf("name = %q, want %q", cfg.Name, "dynamic-lab")
	}
	if len(cfg.InventoryProviders) != 1 {
		t.Fatalf("inventoryProviders count = %d, want 1", len(cfg.InventoryProviders))
	}
	if cfg.InventoryProviders[0]["type"] != "netbox" {
		t.Errorf("provider type = %v, want netbox", cfg.InventoryProviders[0]["type"])
	}
}

func TestDeviceGroupWithBothDevicesAndProviders(t *testing.T) {
	yamlInput := `
kind: deviceGroups
name: hybrid-lab
sourceTemplates:
  ssh:
    type: ssh
    port: 22
    username: admin
    password: admin
    vendor: nokia
    platform: srlinux
    timeout: 30s
devices:
  - name: local-r1
    host: 10.0.0.1
inventoryProviders:
  - type: netbox
    url: https://netbox.example.com
    token: my-token
`
	_, _, _, _, _, _, _, dgConfigs, _, err := UnmarshalResourceConfig(context.Background(), []byte(yamlInput))
	if err != nil {
		t.Fatalf("UnmarshalResourceConfig() error: %v", err)
	}

	if len(dgConfigs) != 1 {
		t.Fatalf("got %d device groups, want 1", len(dgConfigs))
	}

	cfg := dgConfigs[0]
	if len(cfg.Devices) != 1 {
		t.Errorf("devices count = %d, want 1", len(cfg.Devices))
	}
	if len(cfg.InventoryProviders) != 1 {
		t.Errorf("inventoryProviders count = %d, want 1", len(cfg.InventoryProviders))
	}
}
