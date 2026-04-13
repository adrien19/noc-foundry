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
	"path/filepath"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

func TestBuildProfile_FromMinimalSchema(t *testing.T) {
	store := NewSchemaStore()
	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatal(err)
	}
	b, _ := store.Lookup("test", "device", "1.0")

	// Use mappings that target paths in our test YANG module.
	mappings := []OperationMapping{
		{
			OperationID: "get_interfaces",
			NativePaths: []string{"interfaces"},
		},
		{
			OperationID: "get_system_version",
			NativePaths: []string{"system/information"},
		},
	}

	profile, warnings := BuildProfile(b, mappings)
	if len(warnings) > 0 {
		t.Logf("warnings: %v", warnings)
	}

	if profile.Vendor != "test" || profile.Platform != "device" {
		t.Errorf("profile vendor/platform = %s/%s; want test/device", profile.Vendor, profile.Platform)
	}

	if len(profile.Operations) != 2 {
		t.Fatalf("operations count = %d; want 2", len(profile.Operations))
	}

	ifOp, ok := profile.Operations["get_interfaces"]
	if !ok {
		t.Fatal("missing get_interfaces operation")
	}
	if len(ifOp.Paths) == 0 {
		t.Fatal("get_interfaces has no protocol paths")
	}

	// Should have gNMI native + NETCONF native paths.
	var hasGnmi, hasNetconf bool
	for _, pp := range ifOp.Paths {
		if pp.Protocol == profiles.ProtocolGnmiNative {
			hasGnmi = true
		}
		if pp.Protocol == profiles.ProtocolNetconfNative {
			hasNetconf = true
		}
	}
	if !hasGnmi {
		t.Error("get_interfaces missing gNMI native path")
	}
	if !hasNetconf {
		t.Error("get_interfaces missing NETCONF native path")
	}
}

func TestBuildProfile_UnresolvablePaths(t *testing.T) {
	store := NewSchemaStore()
	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatal(err)
	}
	b, _ := store.Lookup("test", "device", "1.0")

	mappings := []OperationMapping{
		{
			OperationID: "get_nonexistent",
			NativePaths: []string{"does/not/exist"},
		},
	}

	profile, warnings := BuildProfile(b, mappings)
	if len(warnings) == 0 {
		t.Error("expected warnings for unresolvable paths")
	}
	if len(profile.Operations) != 0 {
		t.Errorf("operations count = %d; want 0 for unresolvable paths", len(profile.Operations))
	}
}

func TestMergeProfiles(t *testing.T) {
	schemaProfile := &profiles.Profile{
		Vendor:   "nokia",
		Platform: "srlinux",
		Operations: map[string]profiles.OperationDescriptor{
			"get_interfaces": {
				OperationID: "get_interfaces",
				Paths: []profiles.ProtocolPath{
					{Protocol: profiles.ProtocolGnmiNative, Paths: []string{"/schema-derived:interface"}},
					{Protocol: profiles.ProtocolNetconfNative, Filter: `<interface xmlns="urn:schema"/>`},
				},
			},
		},
	}

	fallback := &profiles.Profile{
		Vendor:   "nokia",
		Platform: "srlinux",
		Operations: map[string]profiles.OperationDescriptor{
			"get_interfaces": {
				OperationID: "get_interfaces",
				Paths: []profiles.ProtocolPath{
					{Protocol: profiles.ProtocolGnmiNative, Paths: []string{"/hardcoded:interface"}},
					{Protocol: profiles.ProtocolCLI, Command: "show interface", Format: "json"},
					{Protocol: profiles.ProtocolCLI, Command: "show interface", Format: "text"},
				},
			},
			"get_system_version": {
				OperationID: "get_system_version",
				Paths: []profiles.ProtocolPath{
					{Protocol: profiles.ProtocolCLI, Command: "show version"},
				},
			},
		},
	}

	merged := MergeProfiles(schemaProfile, fallback)

	// get_interfaces should have schema gNMI/NETCONF + fallback CLI.
	ifOp := merged.Operations["get_interfaces"]
	var gnmiCount, netconfCount, cliCount int
	for _, pp := range ifOp.Paths {
		switch pp.Protocol {
		case profiles.ProtocolGnmiNative:
			gnmiCount++
			if pp.Paths[0] != "/schema-derived:interface" {
				t.Errorf("gNMI path = %q; want /schema-derived:interface", pp.Paths[0])
			}
		case profiles.ProtocolNetconfNative:
			netconfCount++
		case profiles.ProtocolCLI:
			cliCount++
		}
	}
	if gnmiCount != 1 {
		t.Errorf("gnmi paths = %d; want 1", gnmiCount)
	}
	if netconfCount != 1 {
		t.Errorf("netconf paths = %d; want 1", netconfCount)
	}
	if cliCount != 2 {
		t.Errorf("cli paths = %d; want 2", cliCount)
	}

	// get_system_version should be preserved from fallback.
	svOp, ok := merged.Operations["get_system_version"]
	if !ok {
		t.Fatal("merged profile missing get_system_version from fallback")
	}
	if len(svOp.Paths) != 1 {
		t.Errorf("get_system_version paths = %d; want 1", len(svOp.Paths))
	}
}

func TestMergeProfiles_NilInputs(t *testing.T) {
	p := &profiles.Profile{Vendor: "v", Platform: "p"}

	if MergeProfiles(nil, p) != p {
		t.Error("MergeProfiles(nil, fallback) should return fallback")
	}
	if MergeProfiles(p, nil) != p {
		t.Error("MergeProfiles(schema, nil) should return schema")
	}
}
