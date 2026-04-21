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

	"github.com/adrien19/noc-foundry/internal/prebuiltconfigs"
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
	ID                   string               `yaml:"id"`
	Data                 OperationDataKind    `yaml:"data,omitempty"`
	Datastore            string               `yaml:"datastore,omitempty"`
	Preferred            []string             `yaml:"preferred,omitempty"`
	Parameters           []OperationParameter `yaml:"parameters,omitempty"`
	Limits               *OperationLimits     `yaml:"limits,omitempty"`
	NativePaths          []string             `yaml:"native_paths"`
	OCPaths              []string             `yaml:"oc_paths"`
	CanonicalLeafAliases []SidecarFieldAlias  `yaml:"canonical_leaf_aliases"`
}

// OperationParameter describes how an operation parameter constrains a YANG
// path. Parameters are intentionally declarative so gNMI/NETCONF paths can be
// rendered from sidecar contracts instead of hardcoded in tool packages.
type OperationParameter struct {
	Name                  string   `yaml:"name"`
	PathKey               string   `yaml:"path_key,omitempty"`
	TargetPath            string   `yaml:"target_path,omitempty"`
	TargetContainer       string   `yaml:"target_container,omitempty"`
	GnmiPathTemplate      string   `yaml:"gnmi_path_template,omitempty"`
	NetconfFilterTemplate string   `yaml:"netconf_filter_template,omitempty"`
	Default               string   `yaml:"default,omitempty"`
	Required              bool     `yaml:"required,omitempty"`
	Allowed               []string `yaml:"allowed,omitempty"`
	Description           string   `yaml:"description,omitempty"`
}

// OperationLimits captures operator-safety defaults for high-volume
// operations such as route tables, logs, and large counters.
type OperationLimits struct {
	DefaultCount int `yaml:"default_count,omitempty"`
	MaxCount     int `yaml:"max_count,omitempty"`
	MaxBytes     int `yaml:"max_bytes,omitempty"`
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

// LoadPrebuiltSidecar loads the embedded operations sidecar for vendor/platform.
func LoadPrebuiltSidecar(vendor, platform string) (*SidecarOps, bool, error) {
	data, err := prebuiltconfigs.GetSidecar(vendor, platform)
	if err != nil {
		return nil, false, nil
	}
	var ops SidecarOps
	if err := yaml.Unmarshal(data, &ops); err != nil {
		return nil, false, fmt.Errorf("parsing prebuilt sidecar for %s/%s: %w", vendor, platform, err)
	}
	return &ops, true, nil
}

// MergeSidecars overlays operation definitions by operation ID. The base
// sidecar is the operations reference, while overlay entries replace or add
// only the operations they declare.
func MergeSidecars(base, overlay *SidecarOps) *SidecarOps {
	if base == nil {
		return overlay
	}
	if overlay == nil {
		return base
	}
	merged := &SidecarOps{Operations: make([]SidecarOperation, 0, len(base.Operations)+len(overlay.Operations))}
	index := make(map[string]int, len(base.Operations))
	for _, op := range base.Operations {
		index[op.ID] = len(merged.Operations)
		merged.Operations = append(merged.Operations, op)
	}
	for _, op := range overlay.Operations {
		if i, ok := index[op.ID]; ok {
			merged.Operations[i] = op
			continue
		}
		index[op.ID] = len(merged.Operations)
		merged.Operations = append(merged.Operations, op)
	}
	return merged
}

// ToOperationMappings converts the sidecar operations to the internal
// OperationMapping type used by the profile builder.
func (s *SidecarOps) ToOperationMappings() []OperationMapping {
	mappings := make([]OperationMapping, 0, len(s.Operations))
	for _, op := range s.Operations {
		mappings = append(mappings, OperationMapping{
			OperationID: op.ID,
			Data:        op.Data.withDefault(),
			Datastore:   op.Datastore,
			Preferred:   op.Preferred,
			Parameters:  op.Parameters,
			Limits:      op.Limits,
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
