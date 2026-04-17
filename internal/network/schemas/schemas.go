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

// Package schemas implements a YANG schema store that compiles vendor
// YANG models at startup using openconfig/goyang. The compiled schema
// tree enables schema-driven path resolution, NETCONF filter generation,
// and version drift detection.
package schemas

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/openconfig/goyang/pkg/yang"
)

// SchemaKey uniquely identifies a vendor YANG model bundle.
type SchemaKey struct {
	Vendor   string
	Platform string
	Version  string
}

// String returns the dot-separated key used for map lookups.
func (k SchemaKey) String() string {
	return strings.ToLower(k.Vendor) + "." + strings.ToLower(k.Platform) + "." + k.Version
}

// VendorPlatformKey returns the key without version for best-match lookups.
func (k SchemaKey) VendorPlatformKey() string {
	return strings.ToLower(k.Vendor) + "." + strings.ToLower(k.Platform)
}

// SchemaBundle holds the compiled YANG schema tree for a single
// vendor/platform/version triple.
type SchemaBundle struct {
	Key       SchemaKey
	Root      *yang.Entry // compiled schema tree root
	Modules   []*yang.Module
	SourceDir string
	LoadedAt  time.Time
}

// SchemaStore manages compiled YANG schema bundles, keyed by
// vendor.platform.version. It is an explicit instance (not a global
// registry) to support dependency injection and testability.
type SchemaStore struct {
	mu      sync.RWMutex
	bundles map[string]*SchemaBundle
}

var (
	defaultMu    sync.RWMutex
	defaultStore *SchemaStore
)

// SetDefault sets the package-level default schema store. Called once
// at server startup so that components (e.g., query.Executor) can
// access the compiled schemas without explicit dependency injection.
func SetDefault(store *SchemaStore) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultStore = store
}

// Default returns the package-level default schema store, or nil if
// none has been set.
func Default() *SchemaStore {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultStore
}

// NewSchemaStore creates an empty schema store.
func NewSchemaStore() *SchemaStore {
	return &SchemaStore{
		bundles: make(map[string]*SchemaBundle),
	}
}

// Load parses and compiles YANG files from the given directories,
// storing the result under the provided key. Returns an error on
// parse/compile failure. Duplicate keys are rejected.
func (s *SchemaStore) Load(key SchemaKey, yangDirs []string) error {
	if key.Vendor == "" || key.Platform == "" || key.Version == "" {
		return fmt.Errorf("schema key requires vendor, platform, and version; got %+v", key)
	}

	keyStr := key.String()
	s.mu.RLock()
	_, exists := s.bundles[keyStr]
	s.mu.RUnlock()
	if exists {
		return fmt.Errorf("schema bundle %q already loaded", keyStr)
	}

	ms := yang.NewModules()

	// Recursively discover all .yang files and collect unique directories
	// for import path resolution. This supports vendor repos where models
	// are organized in subdirectories (e.g., Nokia SRL: srl_nokia/models/interfaces/).
	var yangFiles []string
	importDirs := make(map[string]struct{})
	for _, dir := range yangDirs {
		// Resolve symlinks so WalkDir can traverse the real directory tree.
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return fmt.Errorf("resolving YANG directory %q: %w", dir, err)
		}
		err = filepath.WalkDir(resolved, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".yang") {
				yangFiles = append(yangFiles, path)
				importDirs[filepath.Dir(path)] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("walking YANG directory %q: %w", dir, err)
		}
	}
	// Add all directories containing .yang files to the import path so
	// cross-module imports (e.g., ietf-*, openconfig-*) resolve correctly.
	for d := range importDirs {
		ms.Path = append(ms.Path, d)
	}

	if len(yangFiles) == 0 {
		return fmt.Errorf("no .yang files found in directories %v", yangDirs)
	}

	for _, f := range yangFiles {
		if err := ms.Read(f); err != nil {
			return fmt.Errorf("parsing YANG file %q: %w", f, err)
		}
	}

	errs := ms.Process()
	if len(errs) > 0 {
		// Real-world YANG model sets (e.g., Nokia SRL) contain deviation
		// modules that reference nodes goyang cannot always resolve.
		// Log these as warnings rather than failing, because the core
		// schema tree is still usable for path resolution.
		for _, e := range errs {
			slog.Warn("YANG compilation warning",
				"key", keyStr,
				"error", e.Error(),
			)
		}
	}

	modules := ms.Modules
	root := buildEntryTree(modules)

	bundle := &SchemaBundle{
		Key:       key,
		Root:      root,
		Modules:   flattenModules(modules),
		SourceDir: yangDirs[0],
		LoadedAt:  time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.bundles[keyStr]; exists {
		return fmt.Errorf("schema bundle %q already loaded (race)", keyStr)
	}
	s.bundles[keyStr] = bundle
	return nil
}

// Lookup returns the schema bundle for an exact vendor.platform.version match.
func (s *SchemaStore) Lookup(vendor, platform, version string) (*SchemaBundle, bool) {
	key := SchemaKey{Vendor: vendor, Platform: platform, Version: version}
	s.mu.RLock()
	defer s.mu.RUnlock()
	b, ok := s.bundles[key.String()]
	return b, ok
}

// LookupBestMatch tries an exact match first, then returns the latest
// loaded version for the vendor.platform pair.
func (s *SchemaStore) LookupBestMatch(vendor, platform, version string) (*SchemaBundle, bool) {
	// Try exact match first.
	if version != "" {
		if b, ok := s.Lookup(vendor, platform, version); ok {
			return b, true
		}
	}

	vpKey := SchemaKey{Vendor: vendor, Platform: platform}.VendorPlatformKey()
	s.mu.RLock()
	defer s.mu.RUnlock()

	var candidates []*SchemaBundle
	for _, b := range s.bundles {
		if b.Key.VendorPlatformKey() == vpKey {
			candidates = append(candidates, b)
		}
	}
	if len(candidates) == 0 {
		return nil, false
	}

	// Return the most recently loaded bundle.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].LoadedAt.After(candidates[j].LoadedAt)
	})
	return candidates[0], true
}

// All returns a snapshot of all loaded schema bundles.
func (s *SchemaStore) All() map[string]*SchemaBundle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*SchemaBundle, len(s.bundles))
	for k, v := range s.bundles {
		out[k] = v
	}
	return out
}

// buildEntryTree merges all top-level module entries into a single root
// Entry so that the full schema can be traversed from one point.
// Entries are stored with both bare names and module-prefixed names
// (e.g., "interface" and "srl_nokia-interfaces:interface") to allow
// disambiguation when multiple modules define the same top-level node
// (e.g., srl_nokia-interfaces vs srl_nokia-tools-interfaces).
func buildEntryTree(modules map[string]*yang.Module) *yang.Entry {
	root := &yang.Entry{
		Kind: yang.DirectoryEntry,
		Dir:  make(map[string]*yang.Entry),
	}
	for _, mod := range modules {
		if mod == nil {
			continue
		}
		entry := safeToEntry(mod)
		if entry == nil {
			continue
		}
		for name, child := range entry.Dir {
			// Store with module-prefixed key for disambiguation.
			root.Dir[mod.Name+":"+name] = child
			// Store with bare name (last module wins for ambiguous lookups).
			root.Dir[name] = child
		}
	}
	return root
}

// safeToEntry calls yang.ToEntry with panic recovery because goyang
// may panic on modules with unresolved imports or groupings.
func safeToEntry(mod *yang.Module) (entry *yang.Entry) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn("goyang panicked converting module to entry",
				"module", mod.Name,
				"panic", fmt.Sprint(r),
			)
			entry = nil
		}
	}()
	return yang.ToEntry(mod)
}

// flattenModules returns a slice of all modules from the modules map.
func flattenModules(modules map[string]*yang.Module) []*yang.Module {
	out := make([]*yang.Module, 0, len(modules))
	for _, m := range modules {
		out = append(out, m)
	}
	return out
}

// isDir returns true if the entry is a directory, following symlinks.
func isDir(parentDir string, entry os.DirEntry) bool {
	// Skip hidden directories (e.g., .git).
	if strings.HasPrefix(entry.Name(), ".") {
		return false
	}
	if entry.IsDir() {
		return true
	}
	// DirEntry.IsDir() returns false for symlinks; check the target.
	if entry.Type()&os.ModeSymlink != 0 {
		info, err := os.Stat(filepath.Join(parentDir, entry.Name()))
		if err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// LoadFromDirectory discovers and loads YANG schema bundles from a
// directory with the expected structure: <baseDir>/<vendor>/<platform>/<version>/*.yang
// It returns the number of successfully loaded bundles and any errors
// encountered (individual bundle failures do not block other bundles).
func LoadFromDirectory(store *SchemaStore, baseDir string) (int, []error) {
	var loaded int
	var errs []error

	vendors, err := os.ReadDir(baseDir)
	if err != nil {
		return 0, []error{fmt.Errorf("reading schema base directory %q: %w", baseDir, err)}
	}

	for _, vendorEntry := range vendors {
		if !isDir(baseDir, vendorEntry) {
			continue
		}
		vendorDir := filepath.Join(baseDir, vendorEntry.Name())
		platforms, err := os.ReadDir(vendorDir)
		if err != nil {
			errs = append(errs, fmt.Errorf("reading vendor directory %q: %w", vendorDir, err))
			continue
		}
		for _, platformEntry := range platforms {
			if !isDir(vendorDir, platformEntry) {
				continue
			}
			platformDir := filepath.Join(vendorDir, platformEntry.Name())
			versions, err := os.ReadDir(platformDir)
			if err != nil {
				errs = append(errs, fmt.Errorf("reading platform directory %q: %w", platformDir, err))
				continue
			}
			for _, versionEntry := range versions {
				if !isDir(platformDir, versionEntry) {
					continue
				}
				versionDir := filepath.Join(platformDir, versionEntry.Name())
				key := SchemaKey{
					Vendor:   vendorEntry.Name(),
					Platform: platformEntry.Name(),
					Version:  versionEntry.Name(),
				}
				if err := store.Load(key, []string{versionDir}); err != nil {
					errs = append(errs, fmt.Errorf("loading schema %s: %w", key.String(), err))
					continue
				}
				loaded++

				// Try loading vendor sidecar (nocfoundry-ops.yaml).
				if sidecar, ok, serr := TryLoadSidecar(versionDir); serr != nil {
					errs = append(errs, fmt.Errorf("sidecar for %s: %w", key.String(), serr))
				} else if ok {
					RegisterSidecarMappings(key, sidecar.ToOperationMappings())
					sidecar.ExtendCanonicalMaps()
					slog.Info("loaded vendor sidecar",
						"vendor", key.Vendor,
						"platform", key.Platform,
						"version", key.Version,
					)
				}
			}
		}
	}

	return loaded, errs
}
