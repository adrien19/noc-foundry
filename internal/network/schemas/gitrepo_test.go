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
	"os/exec"
	"path/filepath"
	"testing"
)

// initBareRepo creates a bare git repo with a single commit containing a test
// .yang file on the given branch, returning the repo path.
func initBareRepo(t *testing.T, branch string) string {
	t.Helper()

	// Create a working repo, commit a file, then clone as bare.
	workDir := t.TempDir()
	run(t, workDir, "git", "init", "-b", branch)
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test")

	yangFile := filepath.Join(workDir, "test-mod.yang")
	if err := os.WriteFile(yangFile, []byte("module test-mod { namespace \"urn:test\"; prefix t; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial")

	bareDir := t.TempDir()
	run(t, bareDir, "git", "clone", "--bare", workDir, bareDir+"/repo.git")
	return bareDir + "/repo.git"
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestCloneOrPull_FreshClone(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	cacheDir := t.TempDir()

	dir, err := CloneOrPull(bareRepo, cacheDir, "main", GitAuth{Type: "none"})
	if err != nil {
		t.Fatalf("CloneOrPull failed: %v", err)
	}

	// Verify the yang file exists in the checkout.
	if _, err := os.Stat(filepath.Join(dir, "test-mod.yang")); err != nil {
		t.Fatalf("expected test-mod.yang in checkout: %v", err)
	}
}

func TestCloneOrPull_Idempotent(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	cacheDir := t.TempDir()

	dir1, err := CloneOrPull(bareRepo, cacheDir, "main", GitAuth{Type: "none"})
	if err != nil {
		t.Fatalf("first clone failed: %v", err)
	}

	dir2, err := CloneOrPull(bareRepo, cacheDir, "main", GitAuth{Type: "none"})
	if err != nil {
		t.Fatalf("second clone (pull) failed: %v", err)
	}

	if dir1 != dir2 {
		t.Errorf("expected same directory, got %q and %q", dir1, dir2)
	}
}

func TestCloneOrPull_TagRef(t *testing.T) {
	// Create repo with a tag.
	workDir := t.TempDir()
	run(t, workDir, "git", "init", "-b", "main")
	run(t, workDir, "git", "config", "user.email", "test@test.com")
	run(t, workDir, "git", "config", "user.name", "Test")

	yangFile := filepath.Join(workDir, "mod.yang")
	if err := os.WriteFile(yangFile, []byte("module mod { namespace \"urn:mod\"; prefix m; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(t, workDir, "git", "add", ".")
	run(t, workDir, "git", "commit", "-m", "initial")
	run(t, workDir, "git", "tag", "v1.0")

	bareDir := t.TempDir()
	run(t, bareDir, "git", "clone", "--bare", workDir, bareDir+"/repo.git")

	cacheDir := t.TempDir()
	dir, err := CloneOrPull(bareDir+"/repo.git", cacheDir, "v1.0", GitAuth{Type: "none"})
	if err != nil {
		t.Fatalf("CloneOrPull with tag failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "mod.yang")); err != nil {
		t.Fatalf("expected mod.yang in checkout: %v", err)
	}
}

func TestCloneOrPull_InvalidURL(t *testing.T) {
	cacheDir := t.TempDir()
	_, err := CloneOrPull("/nonexistent/repo", cacheDir, "main", GitAuth{Type: "none"})
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestCacheKey_Deterministic(t *testing.T) {
	k1 := cacheKey("https://example.com/repo", "v1.0")
	k2 := cacheKey("https://example.com/repo", "v1.0")
	if k1 != k2 {
		t.Errorf("expected identical keys, got %q and %q", k1, k2)
	}
}

func TestCacheKey_DifferentRefs(t *testing.T) {
	k1 := cacheKey("https://example.com/repo", "v1.0")
	k2 := cacheKey("https://example.com/repo", "v2.0")
	if k1 == k2 {
		t.Error("expected different keys for different refs")
	}
}
