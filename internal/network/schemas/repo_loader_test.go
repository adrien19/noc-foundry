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

// initRepoWithYANG creates a bare git repo with a valid YANG module on the
// given branch, optionally including a nocfoundry-ops.yaml sidecar.
func initRepoWithYANG(t *testing.T, branch string, includeSidecar bool) string {
	t.Helper()

	workDir := t.TempDir()
	run(t, workDir, "git", "init", "-b", branch)
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test")

	// Write a minimal YANG module.
	yangContent := `module test-iface {
  namespace "urn:test:iface";
  prefix ti;
  container interface {
    leaf name { type string; }
    leaf admin-state { type string; }
  }
}`
	if err := os.WriteFile(filepath.Join(workDir, "test-iface.yang"), []byte(yangContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if includeSidecar {
		sidecar := `operations:
  - id: get_interfaces
    native_paths:
      - /test-iface:interface
`
		if err := os.WriteFile(filepath.Join(workDir, SidecarFileName), []byte(sidecar), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "add yang")

	bareDir := t.TempDir()
	run(t, bareDir, "git", "clone", "--bare", workDir, bareDir+"/repo.git")
	return bareDir + "/repo.git"
}

func TestLoadFromRepos_SingleVersion(t *testing.T) {
	bareRepo := initRepoWithYANG(t, "main", false)
	cacheDir := t.TempDir()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "test-repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "acme", Platform: "router", Version: "v1.0"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded, got %d", loaded)
	}

	// Verify the bundle is in the store.
	bundle, ok := store.Lookup("acme", "router", "v1.0")
	if !ok {
		t.Fatal("expected bundle in store")
	}
	if bundle == nil {
		t.Fatal("bundle is nil")
	}
}

func TestLoadFromRepos_WithSidecar(t *testing.T) {
	bareRepo := initRepoWithYANG(t, "main", true)
	cacheDir := t.TempDir()

	// Reset sidecar state.
	ResetSidecarMappings()
	defer ResetSidecarMappings()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "test-repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "acme", Platform: "router", Version: "v1.0"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded, got %d", loaded)
	}

	// Verify sidecar was registered.
	mappings := GetOperationMappings("acme", "router", "v1.0")
	if mappings == nil {
		t.Fatal("expected sidecar mappings to be registered")
	}
}

func TestLoadFromRepos_RepoSidecarOverlaysPrebuilt(t *testing.T) {
	bareRepo := initRepoWithYANG(t, "main", true)
	cacheDir := t.TempDir()

	ResetSidecarMappings()
	defer ResetSidecarMappings()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "nokia-repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "nokia", Platform: "srlinux", Version: "v99.1"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded, got %d", loaded)
	}

	mappings := GetOperationMappings("nokia", "srlinux", "v99.1")
	var gotInterfaces, gotLLDP bool
	for _, m := range mappings {
		switch m.OperationID {
		case "get_interfaces":
			gotInterfaces = true
			if len(m.NativePaths) == 0 || m.NativePaths[0] != "/test-iface:interface" {
				t.Fatalf("expected repo sidecar to overlay get_interfaces, got %+v", m)
			}
		case "get_lldp_neighbors":
			gotLLDP = true
		}
	}
	if !gotInterfaces || !gotLLDP {
		t.Fatalf("expected overlay get_interfaces and preserved prebuilt LLDP, got %+v", mappings)
	}
}

func TestLoadFromRepos_MultiVersion(t *testing.T) {
	bareV1 := initRepoWithYANG(t, "v1", false)
	bareV2 := initRepoWithYANG(t, "v2", false)
	cacheDir := t.TempDir()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "v1-repo",
			URL:  bareV1,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "v1", Vendor: "acme", Platform: "router", Version: "v1.0"},
			},
		},
		{
			Name: "v2-repo",
			URL:  bareV2,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "v2", Vendor: "acme", Platform: "router", Version: "v2.0"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 2 {
		t.Fatalf("expected 2 loaded, got %d", loaded)
	}

	for _, ver := range []string{"v1.0", "v2.0"} {
		if _, ok := store.Lookup("acme", "router", ver); !ok {
			t.Errorf("expected bundle for version %s", ver)
		}
	}
}

func TestLoadFromRepos_InvalidURL(t *testing.T) {
	cacheDir := t.TempDir()
	store := NewSchemaStore()

	repos := []RepoConfig{
		{
			Name: "bad-repo",
			URL:  "/nonexistent/path",
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "v", Platform: "p", Version: "1"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if loaded != 0 {
		t.Errorf("expected 0 loaded, got %d", loaded)
	}
	if len(errs) == 0 {
		t.Fatal("expected errors for invalid URL")
	}
}

func TestLoadFromRepos_EmptyRepos(t *testing.T) {
	store := NewSchemaStore()
	loaded, errs := LoadFromRepos(store, nil, t.TempDir())
	if loaded != 0 || len(errs) != 0 {
		t.Fatalf("expected 0 loaded 0 errs, got %d/%d", loaded, len(errs))
	}
}

func TestLoadFromRepos_WithSubpath(t *testing.T) {
	// Create a repo with YANG files in a subdirectory.
	workDir := t.TempDir()
	run(t, workDir, "git", "init", "-b", "main")
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test")

	subDir := filepath.Join(workDir, "yang", "models")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yangContent := `module sub-mod {
  namespace "urn:test:sub";
  prefix sm;
  leaf name { type string; }
}`
	if err := os.WriteFile(filepath.Join(subDir, "sub-mod.yang"), []byte(yangContent), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "add yang in subdir")

	bareDir := t.TempDir()
	run(t, bareDir, "git", "clone", "--bare", workDir, bareDir+"/repo.git")

	cacheDir := t.TempDir()
	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "subpath-repo",
			URL:  bareDir + "/repo.git",
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "acme", Platform: "switch", Version: "v3.0", Path: "yang/models"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded, got %d", loaded)
	}

	if _, ok := store.Lookup("acme", "switch", "v3.0"); !ok {
		t.Fatal("expected bundle in store with subpath")
	}
}

func TestBuildGitAuth_Types(t *testing.T) {
	// Test none.
	ga := BuildGitAuth("", "", "", "")
	if ga.Type != "none" {
		t.Errorf("expected 'none', got %q", ga.Type)
	}

	// Test token reads from env.
	t.Setenv("TEST_TOKEN_VAR", "secret123")
	ga = BuildGitAuth("token", "TEST_TOKEN_VAR", "", "")
	if ga.Token != "secret123" {
		t.Errorf("expected token 'secret123', got %q", ga.Token)
	}

	// Test ssh.
	ga = BuildGitAuth("ssh", "", "/path/to/key", "")
	if ga.SSHKeyPath != "/path/to/key" {
		t.Errorf("expected key path '/path/to/key', got %q", ga.SSHKeyPath)
	}
}

func TestLoadFromRepos_CoexistsWithDirectory(t *testing.T) {
	// Load from repo.
	bareRepo := initRepoWithYANG(t, "main", false)
	cacheDir := t.TempDir()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "repovendor", Platform: "p", Version: "v1"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("repo errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 from repo, got %d", loaded)
	}

	// Now load from a local directory into the same store.
	localDir := t.TempDir()
	vendorDir := filepath.Join(localDir, "localvendor", "plat", "v2")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	yangContent := `module local-mod {
  namespace "urn:test:local";
  prefix lm;
  leaf name { type string; }
}`
	if err := os.WriteFile(filepath.Join(vendorDir, "local-mod.yang"), []byte(yangContent), 0o644); err != nil {
		t.Fatal(err)
	}

	dirLoaded, dirErrs := LoadFromDirectory(store, localDir)
	if len(dirErrs) > 0 {
		t.Fatalf("directory errors: %v", dirErrs)
	}
	if dirLoaded != 1 {
		t.Fatalf("expected 1 from directory, got %d", dirLoaded)
	}

	// Both should be in the store.
	if _, ok := store.Lookup("repovendor", "p", "v1"); !ok {
		t.Error("missing repo bundle")
	}
	if _, ok := store.Lookup("localvendor", "plat", "v2"); !ok {
		t.Error("missing directory bundle")
	}
}

func TestLoadFromRepos_OpsFileOverridesRepoSidecar(t *testing.T) {
	// Create a repo WITH a sidecar (get_interfaces).
	bareRepo := initRepoWithYANG(t, "main", true)
	cacheDir := t.TempDir()

	// Write an opsFile that defines a different operation.
	opsDir := t.TempDir()
	opsFile := filepath.Join(opsDir, "custom-ops.yaml")
	opsContent := `operations:
  - id: custom_op
    native_paths:
      - /custom:path
`
	if err := os.WriteFile(opsFile, []byte(opsContent), 0o644); err != nil {
		t.Fatal(err)
	}

	ResetSidecarMappings()
	defer ResetSidecarMappings()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "test-repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "acme", Platform: "router", Version: "v1.0", OpsFile: opsFile},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded, got %d", loaded)
	}

	// The opsFile should win over the repo sidecar.
	mappings := GetOperationMappings("acme", "router", "v1.0")
	if mappings == nil {
		t.Fatal("expected mappings to be registered")
	}
	if len(mappings) != 1 || mappings[0].OperationID != "custom_op" {
		t.Errorf("expected custom_op from opsFile, got %+v", mappings)
	}
}

func TestLoadFromRepos_PrebuiltFallback(t *testing.T) {
	// Create a repo WITHOUT a sidecar, using nokia/srlinux which has a
	// prebuilt default embedded in the binary.
	bareRepo := initRepoWithYANG(t, "main", false)
	cacheDir := t.TempDir()

	ResetSidecarMappings()
	defer ResetSidecarMappings()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "nokia-repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "nokia", Platform: "srlinux", Version: "v99.0"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if loaded != 1 {
		t.Fatalf("expected 1 loaded, got %d", loaded)
	}

	// Prebuilt sidecar for nokia/srlinux should have been used.
	mappings := GetOperationMappings("nokia", "srlinux", "v99.0")
	if mappings == nil {
		t.Fatal("expected prebuilt sidecar mappings to be registered")
	}
	// The prebuilt sidecar must define get_interfaces.
	found := false
	for _, m := range mappings {
		if m.OperationID == "get_interfaces" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected get_interfaces in prebuilt mappings, got %+v", mappings)
	}
}

func TestLoadFromRepos_OpsFileNotFound(t *testing.T) {
	bareRepo := initRepoWithYANG(t, "main", false)
	cacheDir := t.TempDir()

	store := NewSchemaStore()
	repos := []RepoConfig{
		{
			Name: "test-repo",
			URL:  bareRepo,
			Auth: GitAuth{Type: "none"},
			Versions: []RepoVersion{
				{Ref: "main", Vendor: "acme", Platform: "router", Version: "v1.0", OpsFile: "/nonexistent/ops.yaml"},
			},
		},
	}

	loaded, errs := LoadFromRepos(store, repos, cacheDir)
	// Schema itself still loads, but sidecar resolution should error.
	if loaded != 1 {
		t.Errorf("expected schema to load, got %d", loaded)
	}
	if len(errs) == 0 {
		t.Fatal("expected error for missing opsFile")
	}
}
