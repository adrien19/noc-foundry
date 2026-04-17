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
	"os"
	"path/filepath"
	"testing"
)

func TestTryLoadSidecar_Absent(t *testing.T) {
	dir := t.TempDir()
	ops, ok, err := TryLoadSidecar(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatal("expected ok=false for absent sidecar")
	}
	if ops != nil {
		t.Fatal("expected nil ops for absent sidecar")
	}
}

func TestTryLoadSidecar_Valid(t *testing.T) {
	dir := "testdata/sidecar-test/juniper/mx/v22.4"
	ops, ok, err := TryLoadSidecar(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected ok=true for valid sidecar")
	}
	if len(ops.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(ops.Operations))
	}

	// Verify ToOperationMappings conversion.
	mappings := ops.ToOperationMappings()
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(mappings))
	}
	if mappings[0].OperationID != "get_interfaces" {
		t.Errorf("expected get_interfaces, got %s", mappings[0].OperationID)
	}
	if len(mappings[0].NativePaths) != 1 || mappings[0].NativePaths[0] != "/junos-if:interfaces/interface" {
		t.Errorf("unexpected native paths: %v", mappings[0].NativePaths)
	}
	if mappings[1].OperationID != "get_system_version" {
		t.Errorf("expected get_system_version, got %s", mappings[1].OperationID)
	}
}

func TestTryLoadSidecar_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SidecarFileName)
	if err := os.WriteFile(path, []byte("{{{{invalid yaml"), 0644); err != nil {
		t.Fatal(err)
	}

	_, ok, err := TryLoadSidecar(dir)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	if ok {
		t.Fatal("expected ok=false for invalid YAML")
	}
}

func TestGetOperationMappings_SidecarFirst(t *testing.T) {
	t.Cleanup(func() { ResetSidecarMappings() })

	key := SchemaKey{Vendor: "juniper", Platform: "mx", Version: "v22.4"}
	sidecarMaps := []OperationMapping{
		{OperationID: "get_interfaces", NativePaths: []string{"/junos:interfaces"}},
	}
	RegisterSidecarMappings(key, sidecarMaps)

	got := GetOperationMappings("juniper", "mx", "v22.4")
	if got == nil {
		t.Fatal("expected non-nil mappings from sidecar")
	}
	if len(got) != 1 || got[0].NativePaths[0] != "/junos:interfaces" {
		t.Errorf("unexpected mappings: %+v", got)
	}

	// Hardcoded fallback returns nil for juniper.
	fallback := OperationMappingsForVendor("juniper", "mx")
	if fallback != nil {
		t.Fatal("expected nil fallback for juniper")
	}
}

func TestGetOperationMappings_HardcodedFallback(t *testing.T) {
	t.Cleanup(func() { ResetSidecarMappings() })

	// No sidecar registered for Nokia — should fall back to hardcoded.
	got := GetOperationMappings("nokia", "srlinux", "v25.10")
	if got == nil {
		t.Fatal("expected non-nil mappings from hardcoded fallback")
	}
	if got[0].OperationID != "get_interfaces" {
		t.Errorf("unexpected first mapping: %s", got[0].OperationID)
	}
}

func TestExtendCanonicalMap_NoDuplicates(t *testing.T) {
	m, ok := LookupCanonicalMap("get_interfaces")
	if !ok {
		t.Fatal("get_interfaces canonical map not registered")
	}
	originalLen := len(m.Fields)

	// Extend with a new leaf.
	ExtendCanonicalMap("get_interfaces", []FieldMapping{
		{YANGLeaf: "test-sidecar-leaf", CanonicalField: "TestField"},
	})

	m2, _ := LookupCanonicalMap("get_interfaces")
	if len(m2.Fields) != originalLen+1 {
		t.Fatalf("expected %d fields, got %d", originalLen+1, len(m2.Fields))
	}

	// Extend again with the same leaf — should not add a duplicate.
	ExtendCanonicalMap("get_interfaces", []FieldMapping{
		{YANGLeaf: "test-sidecar-leaf", CanonicalField: "TestField"},
	})

	m3, _ := LookupCanonicalMap("get_interfaces")
	if len(m3.Fields) != originalLen+1 {
		t.Fatalf("duplicate added: expected %d fields, got %d", originalLen+1, len(m3.Fields))
	}

	// Clean up: remove the test leaf to avoid polluting other tests.
	m3.Fields = m3.Fields[:originalLen]
}

func TestExtendCanonicalMap_UnknownOperation(t *testing.T) {
	// Should be a no-op, not panic.
	ExtendCanonicalMap("nonexistent_operation", []FieldMapping{
		{YANGLeaf: "leaf", CanonicalField: "Field"},
	})
}

func TestSidecarExtendCanonicalMaps(t *testing.T) {
	ops, ok, err := TryLoadSidecar("testdata/sidecar-test/juniper/mx/v22.4")
	if err != nil || !ok {
		t.Fatalf("failed to load sidecar: err=%v ok=%v", err, ok)
	}

	m, _ := LookupCanonicalMap("get_interfaces")
	originalLen := len(m.Fields)

	ops.ExtendCanonicalMaps()

	m2, _ := LookupCanonicalMap("get_interfaces")
	// Should have added oper-link-status and junos-admin-status.
	if len(m2.Fields) < originalLen+2 {
		t.Errorf("expected at least %d fields after extend, got %d", originalLen+2, len(m2.Fields))
	}

	// Clean up.
	m2.Fields = m2.Fields[:originalLen]
}
