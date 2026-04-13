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
	"log/slog"

	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

// BuildAndRegisterProfiles iterates over all loaded schema bundles,
// builds schema-derived profiles, merges them with hardcoded fallback
// profiles, and registers the result into the global profile registry.
//
// When multiple versions exist for the same vendor.platform, the version
// that resolves the most operation paths is used. This prevents a newer
// (or older) schema from degrading the profile when some paths only
// resolve in one version.
//
// This function should be called once at server startup after the
// schema store has been populated.
func BuildAndRegisterProfiles(store *SchemaStore) {
	// Group bundles by vendor.platform and pick the best per group.
	type candidate struct {
		bundle  *SchemaBundle
		profile *profiles.Profile
		score   int
	}
	best := make(map[string]*candidate) // key: vendor.platform

	for _, bundle := range store.All() {
		mappings := OperationMappingsForVendor(bundle.Key.Vendor, bundle.Key.Platform)
		if mappings == nil {
			slog.Warn("no operation mappings defined",
				"vendor", bundle.Key.Vendor,
				"platform", bundle.Key.Platform,
				"version", bundle.Key.Version,
			)
			continue
		}

		// Validate paths against the schema for drift detection.
		validations := bundle.ValidateOperationPaths(mappings)
		for _, v := range validations {
			switch v.Status {
			case PathNotFound:
				slog.Warn("YANG path not found in schema (version drift?)",
					"path", v.Path,
					"vendor", bundle.Key.Vendor,
					"platform", bundle.Key.Platform,
					"version", bundle.Key.Version,
				)
			case PathDeprecated:
				slog.Warn("YANG path deprecated in schema",
					"path", v.Path,
					"vendor", bundle.Key.Vendor,
					"platform", bundle.Key.Platform,
					"version", bundle.Key.Version,
				)
			}
		}

		schemaProfile, warnings := BuildProfile(bundle, mappings)
		for _, w := range warnings {
			slog.Warn("schema profile build warning",
				"warning", w,
				"vendor", bundle.Key.Vendor,
				"platform", bundle.Key.Platform,
			)
		}

		// Score: count total resolved protocol paths across operations.
		score := 0
		for _, op := range schemaProfile.Operations {
			score += len(op.Paths)
		}

		vpKey := bundle.Key.VendorPlatformKey()
		if prev, exists := best[vpKey]; !exists || score > prev.score {
			best[vpKey] = &candidate{
				bundle:  bundle,
				profile: schemaProfile,
				score:   score,
			}
		}
	}

	// Register the best profile for each vendor.platform.
	for _, c := range best {
		fallback, _ := profiles.Lookup(c.bundle.Key.Vendor, c.bundle.Key.Platform)
		merged := MergeProfiles(c.profile, fallback)

		if len(merged.Operations) > 0 {
			profiles.RegisterOrReplace(merged)
			slog.Info("registered schema-derived profile",
				"vendor", c.bundle.Key.Vendor,
				"platform", c.bundle.Key.Platform,
				"version", c.bundle.Key.Version,
				"operations", len(merged.Operations),
			)
		}
	}
}
