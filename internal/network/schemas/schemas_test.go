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

func TestLoad_Success(t *testing.T) {
	store := NewSchemaStore()

	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	yangDir := filepath.Join("testdata", "minimal")

	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	b, ok := store.Lookup("test", "device", "1.0")
	if !ok {
		t.Fatal("Lookup() returned false after successful Load()")
	}
	if b.Root == nil {
		t.Fatal("SchemaBundle.Root is nil")
	}
	if len(b.Root.Dir) == 0 {
		t.Fatal("SchemaBundle.Root.Dir is empty")
	}
	if b.Key != key {
		t.Errorf("key = %v; want %v", b.Key, key)
	}
}

func TestLoad_DuplicateRejected(t *testing.T) {
	store := NewSchemaStore()

	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	yangDir := filepath.Join("testdata", "minimal")

	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatalf("first Load() failed: %v", err)
	}

	err := store.Load(key, []string{yangDir})
	if err == nil {
		t.Fatal("second Load() with same key should fail")
	}
}

func TestLoad_InvalidYANG(t *testing.T) {
	store := NewSchemaStore()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bad.yang"), []byte("not valid yang"), 0o644); err != nil {
		t.Fatal(err)
	}

	key := SchemaKey{Vendor: "test", Platform: "bad", Version: "1.0"}
	err := store.Load(key, []string{dir})
	if err == nil {
		t.Fatal("Load() with invalid YANG should fail")
	}
}

func TestLoad_EmptyDirectory(t *testing.T) {
	store := NewSchemaStore()

	dir := t.TempDir()
	key := SchemaKey{Vendor: "test", Platform: "empty", Version: "1.0"}
	err := store.Load(key, []string{dir})
	if err == nil {
		t.Fatal("Load() with empty directory should fail")
	}
}

func TestLoad_MissingFields(t *testing.T) {
	store := NewSchemaStore()

	err := store.Load(SchemaKey{Platform: "a", Version: "1"}, []string{"testdata/minimal"})
	if err == nil {
		t.Fatal("Load() with missing Vendor should fail")
	}
	err = store.Load(SchemaKey{Vendor: "a", Version: "1"}, []string{"testdata/minimal"})
	if err == nil {
		t.Fatal("Load() with missing Platform should fail")
	}
	err = store.Load(SchemaKey{Vendor: "a", Platform: "b"}, []string{"testdata/minimal"})
	if err == nil {
		t.Fatal("Load() with missing Version should fail")
	}
}

func TestLookup_NotFound(t *testing.T) {
	store := NewSchemaStore()

	_, ok := store.Lookup("nobody", "nothing", "0.0")
	if ok {
		t.Fatal("Lookup() should return false for nonexistent key")
	}
}

func TestLookupBestMatch_ExactFirst(t *testing.T) {
	store := NewSchemaStore()

	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(SchemaKey{Vendor: "v", Platform: "p", Version: "1.0"}, []string{yangDir}); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(SchemaKey{Vendor: "v", Platform: "p", Version: "2.0"}, []string{yangDir}); err != nil {
		t.Fatal(err)
	}

	b, ok := store.LookupBestMatch("v", "p", "1.0")
	if !ok {
		t.Fatal("LookupBestMatch() returned false")
	}
	if b.Key.Version != "1.0" {
		t.Errorf("version = %q; want 1.0", b.Key.Version)
	}
}

func TestLookupBestMatch_FallbackOnEmpty(t *testing.T) {
	store := NewSchemaStore()

	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(SchemaKey{Vendor: "v", Platform: "p", Version: "1.0"}, []string{yangDir}); err != nil {
		t.Fatal(err)
	}

	b, ok := store.LookupBestMatch("v", "p", "")
	if !ok {
		t.Fatal("LookupBestMatch() should fall back when version is empty")
	}
	if b.Key.Version != "1.0" {
		t.Errorf("version = %q; want 1.0", b.Key.Version)
	}
}

func TestLookupBestMatch_FallbackOnMismatch(t *testing.T) {
	store := NewSchemaStore()

	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(SchemaKey{Vendor: "v", Platform: "p", Version: "1.0"}, []string{yangDir}); err != nil {
		t.Fatal(err)
	}

	b, ok := store.LookupBestMatch("v", "p", "9.9")
	if !ok {
		t.Fatal("LookupBestMatch() should fall back on version mismatch")
	}
	if b.Key.Version != "1.0" {
		t.Errorf("version = %q; want 1.0", b.Key.Version)
	}
}

func TestAll(t *testing.T) {
	store := NewSchemaStore()

	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(SchemaKey{Vendor: "a", Platform: "b", Version: "1"}, []string{yangDir}); err != nil {
		t.Fatal(err)
	}
	if err := store.Load(SchemaKey{Vendor: "c", Platform: "d", Version: "2"}, []string{yangDir}); err != nil {
		t.Fatal(err)
	}

	all := store.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d bundles; want 2", len(all))
	}
}

func TestLoadFromDirectory(t *testing.T) {
	// Create a temp directory structure: base/vendor/platform/version/*.yang
	baseDir := t.TempDir()
	versionDir := filepath.Join(baseDir, "testvendor", "testplatform", "1.0")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Copy test YANG file into the structure.
	yangData, err := os.ReadFile(filepath.Join("testdata", "minimal", "test-module.yang"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versionDir, "test-module.yang"), yangData, 0o644); err != nil {
		t.Fatal(err)
	}

	store := NewSchemaStore()
	loaded, errs := LoadFromDirectory(store, baseDir)
	if len(errs) > 0 {
		t.Fatalf("LoadFromDirectory() errors: %v", errs)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d; want 1", loaded)
	}

	b, ok := store.Lookup("testvendor", "testplatform", "1.0")
	if !ok {
		t.Fatal("bundle not found after LoadFromDirectory()")
	}
	if b.Root == nil {
		t.Fatal("Root is nil")
	}
}

func TestSchemaBundle_RootHasExpectedEntries(t *testing.T) {
	store := NewSchemaStore()

	yangDir := filepath.Join("testdata", "minimal")
	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatal(err)
	}

	b, _ := store.Lookup("test", "device", "1.0")

	// The test-module.yang defines two top-level containers: interfaces, system.
	if _, ok := b.Root.Dir["interfaces"]; !ok {
		t.Error("Root.Dir missing 'interfaces'")
	}
	if _, ok := b.Root.Dir["system"]; !ok {
		t.Error("Root.Dir missing 'system'")
	}
}
