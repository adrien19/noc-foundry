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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// RepoVersion describes a single version entry to load from a cloned repo.
type RepoVersion struct {
	Ref      string
	Vendor   string
	Platform string
	Version  string
	Path     string
	OpsFile  string // optional local nocfoundry-ops.yaml override
}

// RepoConfig describes a remote Git repository containing vendor YANG models.
type RepoConfig struct {
	Name     string
	URL      string
	Auth     GitAuth
	Versions []RepoVersion
}

// LoadFromRepos clones or updates Git repositories described by repos, then
// loads YANG schemas from each version entry into store.  cacheDir is the
// root directory used to cache cloned repositories.
//
// It returns the number of successfully loaded schema bundles and any errors
// encountered (non-fatal errors are accumulated so that one failing repo does
// not prevent others from loading).
func LoadFromRepos(store *SchemaStore, repos []RepoConfig, cacheDir string) (int, []error) {
	if len(repos) == 0 {
		return 0, nil
	}

	// Ensure the cache directory exists.
	if err := os.MkdirAll(cacheDir, 0o750); err != nil {
		return 0, []error{fmt.Errorf("creating schema cache directory %q: %w", cacheDir, err)}
	}

	var loaded int
	var errs []error

	for _, repoCfg := range repos {
		for _, v := range repoCfg.Versions {
			worktree, err := CloneOrPull(repoCfg.URL, cacheDir, v.Ref, repoCfg.Auth)
			if err != nil {
				errs = append(errs, fmt.Errorf("schemaRepo %q version %q: %w", repoCfg.Name, v.Ref, err))
				continue
			}

			yangDir := worktree
			if v.Path != "" && v.Path != "." {
				yangDir = filepath.Join(worktree, filepath.FromSlash(v.Path))
			}

			key := SchemaKey{
				Vendor:   v.Vendor,
				Platform: v.Platform,
				Version:  v.Version,
			}

			if err := store.Load(key, []string{yangDir}); err != nil {
				errs = append(errs, fmt.Errorf("loading schema %s from repo %q: %w", key.String(), repoCfg.Name, err))
				continue
			}
			loaded++

			// Resolve operations sidecar using the priority chain:
			//   1. Explicit opsFile from config
			//   2. Prebuilt embedded default plus nocfoundry-ops.yaml overlay
			//      from the cloned repo
			//   3. Prebuilt embedded default for vendor/platform
			// The hardcoded fallback in GetOperationMappings is the final
			// safety net and is checked at profile-build time.
			if sidecar, source, serr := resolveSidecar(v, yangDir, repoCfg.Name); serr != nil {
				errs = append(errs, serr)
			} else if sidecar != nil {
				RegisterSidecarMappingsWithOrigin(key, sidecar.ToOperationMappings(), source)
				sidecar.ExtendCanonicalMaps()
				slog.Info("loaded vendor sidecar",
					"source", source,
					"vendor", key.Vendor,
					"platform", key.Platform,
					"version", key.Version,
					"repo", repoCfg.Name,
				)
			}
		}
	}

	return loaded, errs
}

// BuildGitAuth constructs a GitAuth from a schema repo auth config, resolving
// environment variable references for secrets.
func BuildGitAuth(authType, tokenEnv, sshKeyPath, sshKeyPassphraseEnv string) GitAuth {
	authType = strings.ToLower(strings.TrimSpace(authType))
	if authType == "" {
		authType = "none"
	}

	ga := GitAuth{Type: authType}

	switch authType {
	case "token":
		ga.Token = os.Getenv(tokenEnv)
	case "ssh":
		ga.SSHKeyPath = sshKeyPath
		if sshKeyPassphraseEnv != "" {
			ga.SSHKeyPassphrase = os.Getenv(sshKeyPassphraseEnv)
		}
	}

	return ga
}

// resolveSidecar applies the priority chain to find a SidecarOps for the
// given version. It returns the sidecar, a human-readable source label
// (for logging), and an optional error.
func resolveSidecar(v RepoVersion, yangDir, repoName string) (*SidecarOps, string, error) {
	key := SchemaKey{Vendor: v.Vendor, Platform: v.Platform, Version: v.Version}

	// 1. Explicit opsFile from config (replace semantics).
	if v.OpsFile != "" {
		sidecar, err := loadSidecarFromFile(v.OpsFile)
		if err != nil {
			return nil, "", fmt.Errorf("sidecar opsFile for %s from repo %q: %w", key.String(), repoName, err)
		}
		return sidecar, "opsFile:" + v.OpsFile, nil
	}

	// 2. Prebuilt embedded default plus nocfoundry-ops.yaml overlay in the
	// cloned repo directory.
	prebuilt, hasPrebuilt, perr := LoadPrebuiltSidecar(v.Vendor, v.Platform)
	if perr != nil {
		return nil, "", perr
	}
	if sidecar, ok, err := TryLoadSidecar(yangDir); err != nil {
		return nil, "", fmt.Errorf("sidecar for %s from repo %q: %w", key.String(), repoName, err)
	} else if ok {
		return MergeSidecars(prebuilt, sidecar), "repo:" + repoName, nil
	}

	// 3. Prebuilt embedded default for vendor/platform.
	if hasPrebuilt {
		return prebuilt, "prebuilt:" + v.Vendor + "/" + v.Platform, nil
	}

	// No sidecar found at any level; the hardcoded fallback in
	// GetOperationMappings will be checked at profile-build time.
	return nil, "", nil
}

// loadSidecarFromFile reads and parses a nocfoundry-ops.yaml from an
// explicit file path.
func loadSidecarFromFile(path string) (*SidecarOps, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading opsFile %s: %w", path, err)
	}
	var ops SidecarOps
	if err := yaml.Unmarshal(data, &ops); err != nil {
		return nil, fmt.Errorf("parsing opsFile %s: %w", path, err)
	}
	return &ops, nil
}
