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

package schemas

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/models"
)

// ---------------------------------------------------------------------------
// StatusNormalizer tests
// ---------------------------------------------------------------------------

func TestNormalizeAdminStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"UP", "UP"},
		{"up", "UP"},
		{"DOWN", "DOWN"},
		{"down", "DOWN"},
		{"enable", "UP"},
		{"Enable", "UP"},
		{"disable", "DOWN"},
		{"Disable", "DOWN"},
		{"inService", "UP"},
		{"outOfService", "DOWN"},
		{"shutdown", "DOWN"},
		{"unknown-value", "unknown-value"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeValue("admin_status", tt.input)
			if got != tt.want {
				t.Errorf("NormalizeValue(admin_status, %q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeOperStatus(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"UP", "UP"},
		{"up", "UP"},
		{"DOWN", "DOWN"},
		{"down", "DOWN"},
		{"inService", "UP"},
		{"outOfService", "DOWN"},
		{"shutdown", "DOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeValue("oper_status", tt.input)
			if got != tt.want {
				t.Errorf("NormalizeValue(oper_status, %q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalizeUnknownNormalizer(t *testing.T) {
	got := NormalizeValue("nonexistent", "hello")
	if got != "hello" {
		t.Errorf("NormalizeValue(nonexistent, hello) = %q, want %q", got, "hello")
	}
}

// ---------------------------------------------------------------------------
// CanonicalMap registry tests
// ---------------------------------------------------------------------------

func TestLookupCanonicalMap(t *testing.T) {
	m, ok := LookupCanonicalMap("get_interfaces")
	if !ok || m == nil {
		t.Fatal("expected get_interfaces canonical map to be registered")
	}
	if m.ModelType != "InterfaceState" {
		t.Errorf("get_interfaces ModelType = %q, want InterfaceState", m.ModelType)
	}

	m, ok = LookupCanonicalMap("get_system_version")
	if !ok || m == nil {
		t.Fatal("expected get_system_version canonical map to be registered")
	}
	if m.ModelType != "SystemVersion" {
		t.Errorf("get_system_version ModelType = %q, want SystemVersion", m.ModelType)
	}

	_, ok = LookupCanonicalMap("nonexistent_operation")
	if ok {
		t.Error("expected nonexistent_operation to not be found")
	}
}

// ---------------------------------------------------------------------------
// SchemaMapper.MapJSON — gNMI JSON normalization tests
// ---------------------------------------------------------------------------

func TestMapJSON_SRLinuxNativeInterfaces(t *testing.T) {
	// Simulate gNMI JSON_IETF response from Nokia SR Linux (native YANG).
	data := map[string]any{
		"srl_nokia-interfaces:interface": []any{
			map[string]any{
				"name":        "ethernet-1/1",
				"admin-state": "enable",
				"oper-state":  "up",
				"description": "To spine-2",
				"mtu":         float64(9232),
			},
			map[string]any{
				"name":        "ethernet-1/2",
				"admin-state": "disable",
				"oper-state":  "down",
				"mtu":         float64(1500),
			},
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, quality, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	ifaces, ok := result.([]models.InterfaceState)
	if !ok {
		t.Fatalf("expected []models.InterfaceState, got %T", result)
	}
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(ifaces))
	}

	assertInterface(t, ifaces[0], "ethernet-1/1", "UP", "UP", "To spine-2", 9232)
	assertInterface(t, ifaces[1], "ethernet-1/2", "DOWN", "DOWN", "", 1500)

	if quality.MappingQuality != models.MappingExact {
		t.Errorf("quality = %q, want exact", quality.MappingQuality)
	}
}

func TestMapJSON_OpenConfigInterfaces(t *testing.T) {
	// OpenConfig-style response with nested state/config containers.
	data := []any{
		map[string]any{
			"name": "GigabitEthernet0/0/0",
			"state": map[string]any{
				"admin-status": "UP",
				"oper-status":  "UP",
				"type":         "ethernetCsmacd",
				"mtu":          float64(1500),
			},
			"config": map[string]any{
				"description": "Uplink to core",
			},
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, quality, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	ifaces, ok := result.([]models.InterfaceState)
	if !ok {
		t.Fatalf("expected []models.InterfaceState, got %T", result)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}

	assertInterface(t, ifaces[0], "GigabitEthernet0/0/0", "UP", "UP", "Uplink to core", 1500)
	if ifaces[0].Type != "ethernetCsmacd" {
		t.Errorf("Type = %q, want ethernetCsmacd", ifaces[0].Type)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("quality = %q, want exact", quality.MappingQuality)
	}
}

func TestMapJSON_SROSInterfaces(t *testing.T) {
	// Nokia SROS uses interface-name instead of name, and inService/outOfService.
	data := []any{
		map[string]any{
			"interface-name": "1/1/1",
			"admin-state":    "inService",
			"oper-state":     "inService",
			"description":    "Customer link",
			"mtu":            float64(9100),
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, _, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	ifaces := result.([]models.InterfaceState)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	assertInterface(t, ifaces[0], "1/1/1", "UP", "UP", "Customer link", 9100)
}

func TestMapJSON_VendorExtensions(t *testing.T) {
	// Unknown leaves should be captured in VendorExtensions.
	data := []any{
		map[string]any{
			"name":          "ethernet-1/1",
			"admin-state":   "enable",
			"oper-state":    "up",
			"custom-metric": float64(42),
			"vendor-tag":    "abc",
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, _, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	ifaces := result.([]models.InterfaceState)
	if ifaces[0].VendorExtensions == nil {
		t.Fatal("expected VendorExtensions to be populated")
	}
	if ifaces[0].VendorExtensions["custom-metric"] != float64(42) {
		t.Errorf("VendorExtensions[custom-metric] = %v, want 42", ifaces[0].VendorExtensions["custom-metric"])
	}
	if ifaces[0].VendorExtensions["vendor-tag"] != "abc" {
		t.Errorf("VendorExtensions[vendor-tag] = %v, want abc", ifaces[0].VendorExtensions["vendor-tag"])
	}
}

func TestMapJSON_InterfaceCountersChildMapping(t *testing.T) {
	data := []any{
		map[string]any{
			"name":        "ethernet-1/1",
			"admin-state": "enable",
			"oper-state":  "up",
			"counters": map[string]any{
				"in-octets":  float64(42),
				"out-octets": float64(84),
			},
			"vendor-leaf": "keep-me",
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}
	result, _, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	ifaces := result.([]models.InterfaceState)
	if ifaces[0].Counters == nil {
		t.Fatal("expected counters child mapping")
	}
	if ifaces[0].Counters.InOctets != 42 || ifaces[0].Counters.OutOctets != 84 {
		t.Fatalf("unexpected counters: %+v", ifaces[0].Counters)
	}
	if ifaces[0].VendorExtensions["vendor-leaf"] != "keep-me" {
		t.Fatalf("VendorExtensions lost unmapped field: %+v", ifaces[0].VendorExtensions)
	}
}

func TestMapJSON_ACLChildEntries(t *testing.T) {
	data := []any{
		map[string]any{
			"name": "protect-cpm",
			"type": "ipv4",
			"entries": map[string]any{
				"entry": []any{
					map[string]any{
						"sequence":        "10",
						"action":          "accept",
						"protocol":        "tcp",
						"matched-packets": float64(123),
					},
				},
			},
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_acl")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}
	result, _, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	acls := result.([]models.ACL)
	if len(acls[0].Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(acls[0].Entries))
	}
	if got := acls[0].Entries[0].MatchedPackets; got != 123 {
		t.Fatalf("MatchedPackets = %d, want 123", got)
	}
}

func TestMapJSON_QoSChildQueuesAndSchedulers(t *testing.T) {
	data := []any{
		map[string]any{
			"interface": "ethernet-1/1",
			"queues": map[string]any{
				"queue": []any{
					map[string]any{"name": "0", "transmit-packets": float64(100)},
				},
			},
			"schedulers": map[string]any{
				"scheduler": []any{
					map[string]any{"sequence": "1", "type": "strict", "weight": float64(10)},
				},
			},
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_qos_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}
	result, _, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	qos := result.([]models.QoSInterface)
	if len(qos[0].Queues) != 1 || qos[0].Queues[0].TransmitPackets != 100 {
		t.Fatalf("unexpected queues: %+v", qos[0].Queues)
	}
	if len(qos[0].Schedulers) != 1 || qos[0].Schedulers[0].Weight != 10 {
		t.Fatalf("unexpected schedulers: %+v", qos[0].Schedulers)
	}
}

func TestMapJSON_SRLinuxLLDPNestedNeighbors(t *testing.T) {
	data := map[string]any{
		"srl_nokia-system:system": map[string]any{
			"srl_nokia-lldp:lldp": map[string]any{
				"interface": []any{
					map[string]any{
						"name": "ethernet-1/1",
						"neighbor": []any{
							map[string]any{
								"system-name":        "leaf-1",
								"port-id":            "ethernet-1/49",
								"port-description":   "uplink",
								"chassis-id":         "aa:bb:cc:dd:ee:ff",
								"management-address": "192.0.2.10",
							},
						},
					},
				},
			},
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_lldp_neighbors")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}
	result, quality, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	neighbors := result.([]models.LLDPNeighbor)
	if len(neighbors) != 1 {
		t.Fatalf("neighbors = %d, want 1; quality=%+v", len(neighbors), quality)
	}
	got := neighbors[0]
	if got.LocalInterface != "ethernet-1/1" || got.RemoteSystemName != "leaf-1" || got.RemotePortID != "ethernet-1/49" {
		t.Fatalf("unexpected LLDP neighbor: %+v", got)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Fatalf("quality = %q, want exact", quality.MappingQuality)
	}
}

func TestMapJSON_SystemVersion_SRLinux(t *testing.T) {
	// SRL system version comes from system/information + system/name.
	data := map[string]any{
		"information": map[string]any{
			"version": "v24.10.1",
		},
		"name": map[string]any{
			"host-name": "spine-1",
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_system_version")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, quality, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	sv, ok := result.(models.SystemVersion)
	if !ok {
		t.Fatalf("expected models.SystemVersion, got %T", result)
	}
	if sv.Hostname != "spine-1" {
		t.Errorf("Hostname = %q, want spine-1", sv.Hostname)
	}
	if sv.SoftwareVersion != "v24.10.1" {
		t.Errorf("SoftwareVersion = %q, want v24.10.1", sv.SoftwareVersion)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("quality = %q, want exact", quality.MappingQuality)
	}
}

func TestMapJSON_SystemVersion_OpenConfig(t *testing.T) {
	data := map[string]any{
		"state": map[string]any{
			"hostname":         "core-rtr-01",
			"software-version": "22.11.1",
		},
	}

	mapper, err := NewSchemaMapper(nil, "get_system_version")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, _, err := mapper.MapJSON(data)
	if err != nil {
		t.Fatalf("MapJSON: %v", err)
	}

	sv := result.(models.SystemVersion)
	if sv.Hostname != "core-rtr-01" {
		t.Errorf("Hostname = %q, want core-rtr-01", sv.Hostname)
	}
	if sv.SoftwareVersion != "22.11.1" {
		t.Errorf("SoftwareVersion = %q, want 22.11.1", sv.SoftwareVersion)
	}
}

// ---------------------------------------------------------------------------
// SchemaMapper.MapXML — NETCONF XML normalization tests
// ---------------------------------------------------------------------------

func TestMapXML_SRLinuxSiblingInterfaces(t *testing.T) {
	// SR Linux returns sibling <interface> elements without a parent wrapper.
	rawXML := []byte(`
<interface xmlns="urn:nokia.com:srlinux:interfaces">
  <name>ethernet-1/1</name>
  <admin-state>enable</admin-state>
  <oper-state>up</oper-state>
  <description>Link to spine-2</description>
  <mtu>9232</mtu>
</interface>
<interface xmlns="urn:nokia.com:srlinux:interfaces">
  <name>ethernet-1/2</name>
  <admin-state>disable</admin-state>
  <oper-state>down</oper-state>
  <mtu>1500</mtu>
</interface>`)

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, quality, err := mapper.MapXML(rawXML)
	if err != nil {
		t.Fatalf("MapXML: %v", err)
	}

	ifaces, ok := result.([]models.InterfaceState)
	if !ok {
		t.Fatalf("expected []models.InterfaceState, got %T", result)
	}
	if len(ifaces) != 2 {
		t.Fatalf("expected 2 interfaces, got %d", len(ifaces))
	}
	assertInterface(t, ifaces[0], "ethernet-1/1", "UP", "UP", "Link to spine-2", 9232)
	assertInterface(t, ifaces[1], "ethernet-1/2", "DOWN", "DOWN", "", 1500)
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("quality = %q, want exact", quality.MappingQuality)
	}
}

func TestMapXML_OpenConfigWrapped(t *testing.T) {
	rawXML := []byte(`
<rpc-reply>
<data>
<interfaces>
  <interface>
    <name>GigabitEthernet0/0/0</name>
    <state>
      <admin-status>UP</admin-status>
      <oper-status>UP</oper-status>
      <mtu>1500</mtu>
    </state>
    <config>
      <description>Uplink</description>
    </config>
  </interface>
</interfaces>
</data>
</rpc-reply>`)

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, _, err := mapper.MapXML(rawXML)
	if err != nil {
		t.Fatalf("MapXML: %v", err)
	}

	ifaces := result.([]models.InterfaceState)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	assertInterface(t, ifaces[0], "GigabitEthernet0/0/0", "UP", "UP", "Uplink", 1500)
}

func TestMapXML_SROSNested(t *testing.T) {
	rawXML := []byte(`
<state>
  <router>
    <interface>
      <interface-name>1/1/1</interface-name>
      <admin-state>inService</admin-state>
      <oper-state>inService</oper-state>
      <description>Customer</description>
      <mtu>9100</mtu>
    </interface>
  </router>
</state>`)

	mapper, err := NewSchemaMapper(nil, "get_interfaces")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, _, err := mapper.MapXML(rawXML)
	if err != nil {
		t.Fatalf("MapXML: %v", err)
	}

	ifaces := result.([]models.InterfaceState)
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	assertInterface(t, ifaces[0], "1/1/1", "UP", "UP", "Customer", 9100)
}

func TestMapXML_SystemVersion(t *testing.T) {
	rawXML := []byte(`
<system>
  <information>
    <version>v24.10.1</version>
  </information>
  <name>
    <host-name>spine-1</host-name>
  </name>
</system>`)

	mapper, err := NewSchemaMapper(nil, "get_system_version")
	if err != nil {
		t.Fatalf("NewSchemaMapper: %v", err)
	}

	result, _, err := mapper.MapXML(rawXML)
	if err != nil {
		t.Fatalf("MapXML: %v", err)
	}

	sv := result.(models.SystemVersion)
	if sv.Hostname != "spine-1" {
		t.Errorf("Hostname = %q, want spine-1", sv.Hostname)
	}
	if sv.SoftwareVersion != "v24.10.1" {
		t.Errorf("SoftwareVersion = %q, want v24.10.1", sv.SoftwareVersion)
	}
}

func TestNewSchemaMapper_UnknownOperation(t *testing.T) {
	_, err := NewSchemaMapper(nil, "nonexistent_operation")
	if err == nil {
		t.Error("expected error for unknown operation")
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertInterface(t *testing.T, iface models.InterfaceState, name, admin, oper, desc string, mtu int) {
	t.Helper()
	if iface.Name != name {
		t.Errorf("Name = %q, want %q", iface.Name, name)
	}
	if iface.AdminStatus != admin {
		t.Errorf("[%s] AdminStatus = %q, want %q", name, iface.AdminStatus, admin)
	}
	if iface.OperStatus != oper {
		t.Errorf("[%s] OperStatus = %q, want %q", name, iface.OperStatus, oper)
	}
	if iface.Description != desc {
		t.Errorf("[%s] Description = %q, want %q", name, iface.Description, desc)
	}
	if iface.MTU != mtu {
		t.Errorf("[%s] MTU = %d, want %d", name, iface.MTU, mtu)
	}
}
