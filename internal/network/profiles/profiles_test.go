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

package profiles_test

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

func TestLookupSRLinux(t *testing.T) {
	p, ok := profiles.Lookup("nokia", "srlinux")
	if !ok {
		t.Fatal("expected nokia.srlinux profile to be registered")
	}
	if p.Vendor != "nokia" {
		t.Errorf("Vendor = %q, want %q", p.Vendor, "nokia")
	}
	if p.Platform != "srlinux" {
		t.Errorf("Platform = %q, want %q", p.Platform, "srlinux")
	}
}

func TestLookupSROS(t *testing.T) {
	p, ok := profiles.Lookup("nokia", "sros")
	if !ok {
		t.Fatal("expected nokia.sros profile to be registered")
	}
	if p.Vendor != "nokia" {
		t.Errorf("Vendor = %q, want %q", p.Vendor, "nokia")
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	_, ok := profiles.Lookup("Nokia", "SRLinux")
	if !ok {
		t.Fatal("expected case-insensitive lookup to work")
	}
}

func TestLookupUnknown(t *testing.T) {
	_, ok := profiles.Lookup("unknown", "platform")
	if ok {
		t.Fatal("expected unknown profile lookup to return false")
	}
}

func TestSRLinuxOperations(t *testing.T) {
	p, _ := profiles.Lookup("nokia", "srlinux")

	ops := []string{profiles.OpGetInterfaces, profiles.OpGetSystemVersion}
	for _, op := range ops {
		if _, exists := p.Operations[op]; !exists {
			t.Errorf("SRLinux profile missing operation %q", op)
		}
	}
}

func TestSROSOperations(t *testing.T) {
	p, _ := profiles.Lookup("nokia", "sros")

	ops := []string{profiles.OpGetInterfaces, profiles.OpGetSystemVersion}
	for _, op := range ops {
		if _, exists := p.Operations[op]; !exists {
			t.Errorf("SROS profile missing operation %q", op)
		}
	}
}

func TestProtocolPathCanExecute(t *testing.T) {
	tcs := []struct {
		desc     string
		path     profiles.ProtocolPath
		caps     capabilities.SourceCapabilities
		wantExec bool
	}{
		{
			desc:     "gnmi_openconfig with full gnmi caps",
			path:     profiles.ProtocolPath{Protocol: profiles.ProtocolGnmiOpenConfig, Paths: []string{"/interfaces"}},
			caps:     capabilities.SourceCapabilities{GnmiSnapshot: true, OpenConfigPaths: true},
			wantExec: true,
		},
		{
			desc:     "gnmi_openconfig without openconfig support",
			path:     profiles.ProtocolPath{Protocol: profiles.ProtocolGnmiOpenConfig, Paths: []string{"/interfaces"}},
			caps:     capabilities.SourceCapabilities{GnmiSnapshot: true, OpenConfigPaths: false},
			wantExec: false,
		},
		{
			desc:     "gnmi_native with native yang",
			path:     profiles.ProtocolPath{Protocol: profiles.ProtocolGnmiNative, Paths: []string{"/srl_nokia-interfaces"}},
			caps:     capabilities.SourceCapabilities{GnmiSnapshot: true, NativeYang: true},
			wantExec: true,
		},
		{
			desc:     "gnmi_native without gnmi snapshot",
			path:     profiles.ProtocolPath{Protocol: profiles.ProtocolGnmiNative, Paths: []string{"/srl_nokia-interfaces"}},
			caps:     capabilities.SourceCapabilities{GnmiSnapshot: false, NativeYang: true},
			wantExec: false,
		},
		{
			desc:     "cli with cli caps",
			path:     profiles.ProtocolPath{Protocol: profiles.ProtocolCLI, Command: "show interface"},
			caps:     capabilities.SourceCapabilities{CLI: true},
			wantExec: true,
		},
		{
			desc:     "cli without cli caps",
			path:     profiles.ProtocolPath{Protocol: profiles.ProtocolCLI, Command: "show interface"},
			caps:     capabilities.SourceCapabilities{CLI: false},
			wantExec: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got := tc.path.CanExecute(tc.caps)
			if got != tc.wantExec {
				t.Errorf("CanExecute() = %v, want %v", got, tc.wantExec)
			}
		})
	}
}

func TestAllProfiles(t *testing.T) {
	all := profiles.AllProfiles()
	if len(all) < 2 {
		t.Errorf("AllProfiles() returned %d profiles, expected at least 2 (nokia.srlinux, nokia.sros)", len(all))
	}
}

func TestProtocolPreferenceOrder(t *testing.T) {
	p, _ := profiles.Lookup("nokia", "srlinux")
	op := p.Operations[profiles.OpGetInterfaces]

	if len(op.Paths) < 2 {
		t.Fatalf("expected at least 2 protocol paths, got %d", len(op.Paths))
	}

	// First path should be gnmi_openconfig (highest preference).
	if op.Paths[0].Protocol != profiles.ProtocolGnmiOpenConfig {
		t.Errorf("first protocol path = %q, want %q", op.Paths[0].Protocol, profiles.ProtocolGnmiOpenConfig)
	}

	// Last path should be CLI (fallback).
	last := op.Paths[len(op.Paths)-1]
	if last.Protocol != profiles.ProtocolCLI {
		t.Errorf("last protocol path = %q, want %q", last.Protocol, profiles.ProtocolCLI)
	}
}
