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

func TestNormalizeStatus(t *testing.T) {
	tcs := []struct {
		in   string
		want string
	}{
		{"up", "UP"},
		{"Up", "UP"},
		{"UP", "UP"},
		{"enable", "UP"},
		{"enabled", "UP"},
		{"down", "DOWN"},
		{"Down", "DOWN"},
		{"disable", "DOWN"},
		{"disabled", "DOWN"},
		{"unknown", "UNKNOWN"},
		{" up ", "UP"},
	}
	for _, tc := range tcs {
		t.Run(tc.in, func(t *testing.T) {
			got := parsers.NormalizeStatus(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeStatus(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseSRLinuxInterfaces(t *testing.T) {
	raw := `===========================================================================
Interface Summary
===========================================================================
ethernet-1/1 is up, speed 25G, type None
  oper-status is up
  Description: uplink to spine1
ethernet-1/2 is down, speed 10G, type None
  oper-status is down
mgmt0 is up, speed 1G, type None
  oper-status is up
===========================================================================`

	got := parsers.ParseSRLinuxInterfaces(raw)

	want := []models.InterfaceState{
		{Name: "ethernet-1/1", AdminStatus: "UP", OperStatus: "UP", Speed: "25G", Type: "None", Description: "uplink to spine1"},
		{Name: "ethernet-1/2", AdminStatus: "DOWN", OperStatus: "DOWN", Speed: "10G", Type: "None"},
		{Name: "mgmt0", AdminStatus: "UP", OperStatus: "UP", Speed: "1G", Type: "None"},
	}

	if len(got) != len(want) {
		t.Fatalf("ParseSRLinuxInterfaces returned %d interfaces, want %d", len(got), len(want))
	}

	for i, w := range want {
		g := got[i]
		if g.Name != w.Name {
			t.Errorf("[%d] Name = %q, want %q", i, g.Name, w.Name)
		}
		if g.AdminStatus != w.AdminStatus {
			t.Errorf("[%d] AdminStatus = %q, want %q", i, g.AdminStatus, w.AdminStatus)
		}
		if g.OperStatus != w.OperStatus {
			t.Errorf("[%d] OperStatus = %q, want %q", i, g.OperStatus, w.OperStatus)
		}
		if g.Speed != w.Speed {
			t.Errorf("[%d] Speed = %q, want %q", i, g.Speed, w.Speed)
		}
		if g.Description != w.Description {
			t.Errorf("[%d] Description = %q, want %q", i, g.Description, w.Description)
		}
	}
}

func TestParseSRLinuxInterfaces_Empty(t *testing.T) {
	got := parsers.ParseSRLinuxInterfaces("")
	if len(got) != 0 {
		t.Errorf("ParseSRLinuxInterfaces(\"\") returned %d interfaces, want 0", len(got))
	}
}

func TestParseSRLinuxSystemVersion(t *testing.T) {
	raw := `----------------------------------------------------------------------
Hostname          : srl1
Software Version  : v24.3.2
System Type       : 7220 IXR-D2
Chassis Type      : 7220 IXR-D2
Last Booted       : 2024-01-15T10:30:00.000Z
----------------------------------------------------------------------`

	got := parsers.ParseSRLinuxSystemVersion(raw)

	if got.Hostname != "srl1" {
		t.Errorf("Hostname = %q, want %q", got.Hostname, "srl1")
	}
	if got.SoftwareVersion != "v24.3.2" {
		t.Errorf("SoftwareVersion = %q, want %q", got.SoftwareVersion, "v24.3.2")
	}
	if got.SystemType != "7220 IXR-D2" {
		t.Errorf("SystemType = %q, want %q", got.SystemType, "7220 IXR-D2")
	}
	if got.ChassisType != "7220 IXR-D2" {
		t.Errorf("ChassisType = %q, want %q", got.ChassisType, "7220 IXR-D2")
	}
	if got.Uptime != "2024-01-15T10:30:00.000Z" {
		t.Errorf("Uptime = %q, want %q", got.Uptime, "2024-01-15T10:30:00.000Z")
	}
}

func TestParseSROSInterfaces(t *testing.T) {
	raw := `===============================================================================
Interface Table (Router: Base)
===============================================================================
Interface-Name                  Adm       Opr(v4/v6)  Mode     Port/SapId
   IP-Address                                            PfxState
-------------------------------------------------------------------------------
system                          Up        Up/Down     Network  -
   10.0.0.1/32                                           n/a
toR2                            Up        Down/Down   Network  1/1/1
   10.1.1.1/30                                           n/a
loopback                        Down      Down/Down   Network  -
===============================================================================`

	got := parsers.ParseSROSInterfaces(raw)

	want := []models.InterfaceState{
		{Name: "system", AdminStatus: "UP", OperStatus: "UP"},
		{Name: "toR2", AdminStatus: "UP", OperStatus: "DOWN"},
		{Name: "loopback", AdminStatus: "DOWN", OperStatus: "DOWN"},
	}

	if len(got) != len(want) {
		t.Fatalf("ParseSROSInterfaces returned %d interfaces, want %d", len(got), len(want))
	}

	for i, w := range want {
		g := got[i]
		if g.Name != w.Name {
			t.Errorf("[%d] Name = %q, want %q", i, g.Name, w.Name)
		}
		if g.AdminStatus != w.AdminStatus {
			t.Errorf("[%d] AdminStatus = %q, want %q", i, g.AdminStatus, w.AdminStatus)
		}
		if g.OperStatus != w.OperStatus {
			t.Errorf("[%d] OperStatus = %q, want %q", i, g.OperStatus, w.OperStatus)
		}
	}
}

func TestParseSROSInterfaces_VendorExtensions(t *testing.T) {
	raw := `===============================================================================
Interface Table (Router: Base)
===============================================================================
Interface-Name                  Adm       Opr(v4/v6)  Mode     Port/SapId
   IP-Address                                            PfxState
-------------------------------------------------------------------------------
system                          Up        Up/Down     Network  -
   10.0.0.1/32                                           n/a
===============================================================================`

	got := parsers.ParseSROSInterfaces(raw)
	if len(got) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(got))
	}

	ext := got[0].VendorExtensions
	if ext == nil {
		t.Fatal("VendorExtensions is nil, expected mode field")
	}
	if mode, ok := ext["mode"].(string); !ok || mode != "Network" {
		t.Errorf("VendorExtensions[mode] = %v, want %q", ext["mode"], "Network")
	}
}

func TestParseSROSSystemVersion(t *testing.T) {
	raw := `===============================================================================
System Information
===============================================================================
System Name            : router-1
System Version         : TiMOS-B-24.3.R1
System Type            : 7750 SR-1
Chassis Type           : 7750 SR-1
System Up Time         : 45 days, 12:30:15
===============================================================================`

	got := parsers.ParseSROSSystemVersion(raw)

	if got.Hostname != "router-1" {
		t.Errorf("Hostname = %q, want %q", got.Hostname, "router-1")
	}
	if got.SoftwareVersion != "TiMOS-B-24.3.R1" {
		t.Errorf("SoftwareVersion = %q, want %q", got.SoftwareVersion, "TiMOS-B-24.3.R1")
	}
	if got.SystemType != "7750 SR-1" {
		t.Errorf("SystemType = %q, want %q", got.SystemType, "7750 SR-1")
	}
	if got.Uptime != "45 days, 12:30:15" {
		t.Errorf("Uptime = %q, want %q", got.Uptime, "45 days, 12:30:15")
	}
}
