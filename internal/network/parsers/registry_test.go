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

package parsers_test

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/parsers"
)

// TestDispatch_RegisteredVendors verifies that the Nokia srlinux and sros
// parsers are registered via their init() functions and that Dispatch routes
// raw output to the correct parser for the given format.
func TestDispatch_SRLinux_Text_Interfaces(t *testing.T) {
	raw := `ethernet-1/1 is up, speed 25G, type None
  oper-status is up`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "srlinux",
		Operation: "get_interfaces", Format: "text",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingDerived {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingDerived)
	}
	ifaces, ok := payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("payload type = %T, want []models.InterfaceState", payload)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "ethernet-1/1" {
		t.Errorf("unexpected interfaces: %+v", ifaces)
	}
}

func TestDispatch_SRLinux_JSON_Interfaces(t *testing.T) {
	raw := `{"interface":[{"name":"ethernet-1/1","admin-state":"enable","oper-state":"up"}]}`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "srlinux",
		Operation: "get_interfaces", Format: "json",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingExact)
	}
	ifaces, ok := payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("payload type = %T, want []models.InterfaceState", payload)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if ifaces[0].Name != "ethernet-1/1" {
		t.Errorf("Name = %q, want %q", ifaces[0].Name, "ethernet-1/1")
	}
	if ifaces[0].AdminStatus != "UP" {
		t.Errorf("AdminStatus = %q, want %q", ifaces[0].AdminStatus, "UP")
	}
	if ifaces[0].OperStatus != "UP" {
		t.Errorf("OperStatus = %q, want %q", ifaces[0].OperStatus, "UP")
	}
}

func TestDispatch_SRLinux_JSON_InterfacesPluralWrapper(t *testing.T) {
	raw := `{"interfaces":[{"name":"ethernet-1/1","admin-state":"enable","subinterfaces":[{"oper-state":"up"}]}]}`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "srlinux",
		Operation: "get_interfaces", Format: "json",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingExact)
	}
	ifaces, ok := payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("payload type = %T, want []models.InterfaceState", payload)
	}
	if len(ifaces) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(ifaces))
	}
	if ifaces[0].Name != "ethernet-1/1" {
		t.Errorf("Name = %q, want %q", ifaces[0].Name, "ethernet-1/1")
	}
	if ifaces[0].OperStatus != "UP" {
		t.Errorf("OperStatus = %q, want %q", ifaces[0].OperStatus, "UP")
	}
}

func TestDispatch_SRLinux_JSON_Interfaces_InvalidJSON(t *testing.T) {
	_, _, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "srlinux",
		Operation: "get_interfaces", Format: "json",
	}, "not valid json {{")

	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestDispatch_SRLinux_Text_SystemVersion(t *testing.T) {
	raw := `Hostname          : srl1
Software Version  : v24.3.2
System Type       : 7220 IXR-D2`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "srlinux",
		Operation: "get_system_version", Format: "text",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingDerived {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingDerived)
	}
	sv, ok := payload.(models.SystemVersion)
	if !ok {
		t.Fatalf("payload type = %T, want models.SystemVersion", payload)
	}
	if sv.Hostname != "srl1" {
		t.Errorf("Hostname = %q, want %q", sv.Hostname, "srl1")
	}
}

func TestDispatch_SRLinux_JSON_SystemVersion(t *testing.T) {
	raw := `{"hostname":"srl1","software-version":"v24.3.2","platform-type":"7220 IXR-D2"}`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "srlinux",
		Operation: "get_system_version", Format: "json",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingExact)
	}
	sv, ok := payload.(models.SystemVersion)
	if !ok {
		t.Fatalf("payload type = %T, want models.SystemVersion", payload)
	}
	if sv.Hostname != "srl1" {
		t.Errorf("Hostname = %q, want %q", sv.Hostname, "srl1")
	}
	if sv.SoftwareVersion != "v24.3.2" {
		t.Errorf("SoftwareVersion = %q, want %q", sv.SoftwareVersion, "v24.3.2")
	}
	if sv.SystemType != "7220 IXR-D2" {
		t.Errorf("SystemType = %q, want %q", sv.SystemType, "7220 IXR-D2")
	}
}

func TestDispatch_SROS_Text_Interfaces(t *testing.T) {
	raw := `===============================================================================
Interface Table (Router: Base)
===============================================================================
Interface-Name                  Adm       Opr(v4/v6)  Mode     Port/SapId
   IP-Address                                            PfxState
-------------------------------------------------------------------------------
system                          Up        Up/Down     Network  -
   10.0.0.1/32                                           n/a
===============================================================================`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "sros",
		Operation: "get_interfaces", Format: "text",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingDerived {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingDerived)
	}
	ifaces, ok := payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("payload type = %T, want []models.InterfaceState", payload)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "system" {
		t.Errorf("unexpected interfaces: %+v", ifaces)
	}
}

func TestDispatch_SROS_JSON_Interfaces(t *testing.T) {
	raw := `{"router-interface":[{"interface-name":"system","admin-state":"enable","oper-state":"up"}]}`

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "nokia", Platform: "sros",
		Operation: "get_interfaces", Format: "json",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() error: %v", err)
	}
	if quality.MappingQuality != models.MappingExact {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingExact)
	}
	ifaces, ok := payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("payload type = %T, want []models.InterfaceState", payload)
	}
	if len(ifaces) != 1 || ifaces[0].Name != "system" {
		t.Errorf("unexpected interfaces: %+v", ifaces)
	}
}

func TestDispatch_UnknownKey_ReturnsRaw(t *testing.T) {
	raw := "some raw output"

	payload, quality, err := parsers.Dispatch(parsers.ParserKey{
		Vendor: "unknown_vendor_xyz", Platform: "unknown_platform_xyz",
		Operation: "get_interfaces", Format: "text",
	}, raw)

	if err != nil {
		t.Fatalf("Dispatch() should not error for unknown key, got: %v", err)
	}
	if quality.MappingQuality != models.MappingPartial {
		t.Errorf("MappingQuality = %q, want %q", quality.MappingQuality, models.MappingPartial)
	}
	if len(quality.Warnings) == 0 {
		t.Error("expected a warning for unknown key, got none")
	}
	if s, ok := payload.(string); !ok || s != raw {
		t.Errorf("payload = %v, want raw string %q", payload, raw)
	}
}
