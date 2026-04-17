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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

// SidecarFileName is the well-known name for the operations sidecar file
// placed alongside YANG model files in a schema bundle directory.
const SidecarFileName = "nocfoundry-ops.yaml"

// SidecarOps represents the top-level structure of a nocfoundry-ops.yaml file.
type SidecarOps struct {
	Operations []SidecarOperation `yaml:"operations"`
}

// SidecarOperation defines one operation mapping in the sidecar file.
type SidecarOperation struct {
	ID                   string              `yaml:"id"`
	NativePaths          []string            `yaml:"native_paths"`
	OCPaths              []string            `yaml:"oc_paths"`
	CanonicalLeafAliases []SidecarFieldAlias `yaml:"canonical_leaf_aliases"`
}

// SidecarFieldAlias maps a vendor-specific YANG leaf name to a canonical
// model field, with an optional normalizer reference.
type SidecarFieldAlias struct {
	YANGLeaf       string `yaml:"yang_leaf"`
	CanonicalField string `yaml:"canonical_field"`
	Normalizer     string `yaml:"normalizer,omitempty"`
}

// TryLoadSidecar attempts to read and parse a nocfoundry-ops.yaml file
// from the given directory. Returns:
//   - (*SidecarOps, true, nil) when the file exists and parses successfully.
//   - (nil, false, nil) when the file does not exist.
//   - (nil, false, error) when the file exists but cannot be parsed.
//
// This function has no side effects — registration is the caller's responsibility.
func TryLoadSidecar(dir string) (*SidecarOps, bool, error) {
	path := filepath.Join(dir, SidecarFileName)

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("reading sidecar %s: %w", path, err)
	}

	var ops SidecarOps
	if err := yaml.Unmarshal(data, &ops); err != nil {
		return nil, false, fmt.Errorf("parsing sidecar %s: %w", path, err)
	}

	return &ops, true, nil
}

// ToOperationMappings converts the sidecar operations to the internal
// OperationMapping type used by the profile builder.
func (s *SidecarOps) ToOperationMappings() []OperationMapping {
	mappings := make([]OperationMapping, 0, len(s.Operations))
	for _, op := range s.Operations {
		mappings = append(mappings, OperationMapping{
			OperationID: op.ID,
			NativePaths: op.NativePaths,
			OCPaths:     op.OCPaths,
		})
	}
	return mappings
}

// ExtendCanonicalMaps registers any vendor-specific YANG leaf aliases
// declared in the sidecar into the global canonical map registry.
func (s *SidecarOps) ExtendCanonicalMaps() {
	for _, op := range s.Operations {
		if len(op.CanonicalLeafAliases) == 0 {
			continue
		}
		fields := make([]FieldMapping, 0, len(op.CanonicalLeafAliases))
		for _, alias := range op.CanonicalLeafAliases {
			fields = append(fields, FieldMapping(alias))
		}
		ExtendCanonicalMap(op.ID, fields)
	}
}
