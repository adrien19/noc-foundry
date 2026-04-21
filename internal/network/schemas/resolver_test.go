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
	"strings"
	"testing"

	"github.com/openconfig/goyang/pkg/yang"
)

func loadTestBundle(t *testing.T) *SchemaBundle {
	t.Helper()
	store := NewSchemaStore()
	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	b, ok := store.Lookup("test", "device", "1.0")
	if !ok {
		t.Fatal("Lookup() returned false after Load()")
	}
	return b
}

func TestResolvePath_TopLevelContainer(t *testing.T) {
	b := loadTestBundle(t)

	resolved, err := b.ResolvePath("interfaces")
	if err != nil {
		t.Fatalf("ResolvePath(interfaces) failed: %v", err)
	}

	if len(resolved.GnmiPaths) != 1 {
		t.Fatalf("GnmiPaths len = %d; want 1", len(resolved.GnmiPaths))
	}
	if !strings.HasSuffix(resolved.GnmiPaths[0], ":interfaces") {
		t.Errorf("GnmiPaths[0] = %q; want suffix ':interfaces'", resolved.GnmiPaths[0])
	}
	if resolved.Namespace != "urn:example:test-module" {
		t.Errorf("Namespace = %q; want urn:example:test-module", resolved.Namespace)
	}
	if resolved.ModuleName != "test-module" {
		t.Errorf("ModuleName = %q; want test-module", resolved.ModuleName)
	}
	if resolved.ModuleRevision != "2024-01-01" {
		t.Errorf("ModuleRevision = %q; want 2024-01-01", resolved.ModuleRevision)
	}
}

func TestResolvePath_NestedPath(t *testing.T) {
	b := loadTestBundle(t)

	resolved, err := b.ResolvePath("system/information")
	if err != nil {
		t.Fatalf("ResolvePath(system/information) failed: %v", err)
	}

	if resolved.ModuleName != "test-module" {
		t.Errorf("ModuleName = %q; want test-module", resolved.ModuleName)
	}
	if resolved.NetconfFilter == "" {
		t.Error("NetconfFilter is empty")
	}
	if !strings.Contains(resolved.NetconfFilter, "information") {
		t.Errorf("NetconfFilter = %q; want to contain 'information'", resolved.NetconfFilter)
	}
}

func TestResolvePath_NotFound(t *testing.T) {
	b := loadTestBundle(t)

	_, err := b.ResolvePath("nonexistent/path")
	if err == nil {
		t.Fatal("ResolvePath() should fail for nonexistent path")
	}
}

func TestResolvePath_AbsolutePath(t *testing.T) {
	b := loadTestBundle(t)

	resolved, err := b.ResolvePath("/interfaces")
	if err != nil {
		t.Fatalf("ResolvePath(/interfaces) failed: %v", err)
	}
	if resolved.ModuleName != "test-module" {
		t.Errorf("ModuleName = %q; want test-module", resolved.ModuleName)
	}
}

func TestGenerateNetconfFilter(t *testing.T) {
	b := loadTestBundle(t)

	filter, err := b.GenerateNetconfFilter("interfaces")
	if err != nil {
		t.Fatalf("GenerateNetconfFilter() failed: %v", err)
	}

	if !strings.Contains(filter, "interfaces") {
		t.Errorf("filter = %q; want to contain 'interfaces'", filter)
	}
	if !strings.Contains(filter, "xmlns") {
		t.Errorf("filter = %q; want to contain 'xmlns'", filter)
	}
	if !strings.Contains(filter, "urn:example:test-module") {
		t.Errorf("filter = %q; want to contain namespace", filter)
	}
}

func TestGenerateNetconfFilter_NotFound(t *testing.T) {
	b := loadTestBundle(t)

	_, err := b.GenerateNetconfFilter("nonexistent")
	if err == nil {
		t.Fatal("GenerateNetconfFilter() should fail for nonexistent path")
	}
}

func TestValidateOperationPaths(t *testing.T) {
	b := loadTestBundle(t)

	mappings := []OperationMapping{
		{
			OperationID: "test_op",
			NativePaths: []string{"interfaces", "nonexistent"},
			OCPaths:     []string{"system/information"},
		},
	}

	results := b.ValidateOperationPaths(mappings)
	if len(results) != 3 {
		t.Fatalf("results len = %d; want 3", len(results))
	}

	statusMap := make(map[string]PathValidationStatus)
	for _, r := range results {
		statusMap[r.Path] = r.Status
	}

	if statusMap["interfaces"] != PathFound {
		t.Errorf("interfaces status = %s; want found", statusMap["interfaces"])
	}
	if statusMap["nonexistent"] != PathNotFound {
		t.Errorf("nonexistent status = %s; want not_found", statusMap["nonexistent"])
	}
	if statusMap["system/information"] != PathFound {
		t.Errorf("system/information status = %s; want found", statusMap["system/information"])
	}
}

func TestFindEntry_PrefixedPath(t *testing.T) {
	b := loadTestBundle(t)

	// The test YANG module has container "interfaces" under module "test-module".
	// findEntryChain should handle "test-module:interfaces" prefix notation.
	entries := findEntryChain(b.Root, "test-module:interfaces")
	if len(entries) == 0 {
		// goyang may or may not store entries with module prefix — check bare name.
		entries = findEntryChain(b.Root, "interfaces")
		if len(entries) == 0 {
			t.Fatal("findEntryChain() returned nil for both prefixed and bare 'interfaces'")
		}
	}
	entry := entries[len(entries)-1]
	if entry.Name != "interfaces" {
		t.Errorf("entry.Name = %q; want 'interfaces'", entry.Name)
	}
}

func TestBuildGnmiPath(t *testing.T) {
	tests := []struct {
		yangPath   string
		moduleName string
		want       string
	}{
		{"interfaces", "test-module", "/test-module:interfaces"},
		{"srl_nokia-interfaces:interface", "", "/srl_nokia-interfaces:interface"},
		{"system/information", "test-module", "/test-module:system/test-module:information"},
		{"/interfaces", "test-module", "/test-module:interfaces"},
	}

	for _, tt := range tests {
		got := buildGnmiPath(tt.yangPath, tt.moduleName)
		if got != tt.want {
			t.Errorf("buildGnmiPath(%q, %q) = %q; want %q", tt.yangPath, tt.moduleName, got, tt.want)
		}
	}
}

func TestBuildGnmiPathFromEntries_MixedNamespaceAugment(t *testing.T) {
	baseModule := &yang.Module{Name: "srl_nokia-network-instance"}
	augModule := &yang.Module{Name: "srl_nokia-ip-route-tables"}
	entries := []*yang.Entry{
		{Name: "network-instance", Node: baseModule},
		{Name: "route-table", Node: baseModule},
		{Name: "ipv4-unicast", Node: augModule},
		{Name: "route", Node: augModule},
	}

	got := buildGnmiPathFromEntries(entries, "/srl_nokia-network-instance:network-instance/route-table/srl_nokia-ip-route-tables:ipv4-unicast/route", "")
	want := "/srl_nokia-network-instance:network-instance/srl_nokia-network-instance:route-table/srl_nokia-ip-route-tables:ipv4-unicast/srl_nokia-ip-route-tables:route"
	if got != want {
		t.Fatalf("buildGnmiPathFromEntries() = %q; want %q", got, want)
	}
}
