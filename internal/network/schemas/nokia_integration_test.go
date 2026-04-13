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

//go:build nokia_yang

package schemas

import (
	"os"
	"path/filepath"
	"testing"
)

// These tests require the Nokia SR Linux YANG models to be present at
// yang-models/nokia/srlinux/v25.10/ (symlinked to the cloned repo).
// Run with:
//
//	CGO_ENABLED=1 go test -tags nokia_yang -race -v -timeout 5m ./internal/network/schemas/

const nokiaYangDir = "../../../yang-models/nokia/srlinux/v25.10"

func nokiaModelsAvailable(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(nokiaYangDir); os.IsNotExist(err) {
		t.Skip("Nokia YANG models not available at", nokiaYangDir)
	}
}

func TestNokiaSRL_LoadFromDirectory(t *testing.T) {
	nokiaModelsAvailable(t)

	// Create a clean schema directory with the expected structure:
	//   <tmpdir>/nokia/srlinux/v25.10 -> <real nokia models>
	baseDir := t.TempDir()
	absModels, err := filepath.Abs(nokiaYangDir)
	if err != nil {
		t.Fatal(err)
	}
	versionDir := filepath.Join(baseDir, "nokia", "srlinux", "v25.10")
	if err := os.MkdirAll(filepath.Dir(versionDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(absModels, versionDir); err != nil {
		t.Fatal(err)
	}

	store := NewSchemaStore()
	loaded, errs := LoadFromDirectory(store, baseDir)
	for _, e := range errs {
		t.Logf("load error: %v", e)
	}
	if loaded == 0 {
		t.Fatal("expected at least 1 bundle to be loaded, got 0")
	}
	t.Logf("loaded %d bundle(s), %d error(s)", loaded, len(errs))

	bundle, ok := store.Lookup("nokia", "srlinux", "v25.10")
	if !ok {
		t.Fatal("expected to find nokia.srlinux.v25.10 bundle")
	}
	t.Logf("bundle key: %s, modules: %d, root entries: %d",
		bundle.Key.String(), len(bundle.Modules), len(bundle.Root.Dir))
}

func TestNokiaSRL_DirectLoad(t *testing.T) {
	nokiaModelsAvailable(t)

	store := NewSchemaStore()
	key := SchemaKey{Vendor: "nokia", Platform: "srlinux", Version: "v25.10"}
	err := store.Load(key, []string{nokiaYangDir})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	bundle, ok := store.Lookup("nokia", "srlinux", "v25.10")
	if !ok {
		t.Fatal("bundle not found after Load")
	}

	t.Logf("modules: %d, root entries: %d", len(bundle.Modules), len(bundle.Root.Dir))

	// Check that some expected top-level entries are present.
	expectedEntries := []string{"interface", "system", "network-instance", "acl", "platform"}
	for _, name := range expectedEntries {
		if _, ok := bundle.Root.Dir[name]; !ok {
			t.Logf("warning: expected root entry %q not found (available: %v)", name, rootDirKeys(bundle))
		} else {
			t.Logf("found root entry: %s", name)
		}
	}
}

func TestNokiaSRL_ResolvePath(t *testing.T) {
	nokiaModelsAvailable(t)

	store := NewSchemaStore()
	key := SchemaKey{Vendor: "nokia", Platform: "srlinux", Version: "v25.10"}
	if err := store.Load(key, []string{nokiaYangDir}); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	bundle, _ := store.Lookup("nokia", "srlinux", "v25.10")

	// Try resolving some expected paths.
	testPaths := []string{
		"/interface",
		"/system",
		"/network-instance",
		"/acl",
		"/platform",
		"/srl_nokia-interfaces:interface",
		"/srl_nokia-system:system",
	}

	for _, path := range testPaths {
		resolved, err := bundle.ResolvePath(path)
		if err != nil {
			t.Logf("ResolvePath(%q): error: %v", path, err)
		} else {
			t.Logf("ResolvePath(%q): found (gnmi=%v, ns=%s, module=%s)",
				path, resolved.GnmiPaths, resolved.Namespace, resolved.ModuleName)
		}
	}
}

func TestNokiaSRL_ValidateOperationPaths(t *testing.T) {
	nokiaModelsAvailable(t)

	store := NewSchemaStore()
	key := SchemaKey{Vendor: "nokia", Platform: "srlinux", Version: "v25.10"}
	if err := store.Load(key, []string{nokiaYangDir}); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	bundle, _ := store.Lookup("nokia", "srlinux", "v25.10")

	mappings := OperationMappingsForVendor("nokia", "srlinux")
	if mappings == nil {
		t.Skip("no operation mappings for nokia.srlinux")
	}

	results := bundle.ValidateOperationPaths(mappings)
	for _, r := range results {
		t.Logf("path=%s status=%s", r.Path, r.Status)
	}
}

func TestNokiaSRL_BuildProfile(t *testing.T) {
	nokiaModelsAvailable(t)

	store := NewSchemaStore()
	key := SchemaKey{Vendor: "nokia", Platform: "srlinux", Version: "v25.10"}
	if err := store.Load(key, []string{nokiaYangDir}); err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	bundle, _ := store.Lookup("nokia", "srlinux", "v25.10")

	mappings := OperationMappingsForVendor("nokia", "srlinux")
	if mappings == nil {
		t.Skip("no operation mappings for nokia.srlinux")
	}

	profile, warnings := BuildProfile(bundle, mappings)
	for _, w := range warnings {
		t.Logf("warning: %s", w)
	}
	t.Logf("profile: vendor=%s platform=%s operations=%d",
		profile.Vendor, profile.Platform, len(profile.Operations))

	for name, op := range profile.Operations {
		t.Logf("  operation=%s paths=%d", name, len(op.Paths))
	}
}

func rootDirKeys(bundle *SchemaBundle) []string {
	keys := make([]string, 0, len(bundle.Root.Dir))
	for k := range bundle.Root.Dir {
		keys = append(keys, k)
	}
	return keys
}
