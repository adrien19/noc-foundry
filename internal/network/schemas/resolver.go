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
	"strings"

	"github.com/openconfig/goyang/pkg/yang"
)

// ResolvedPaths holds the results of resolving a YANG path against a
// compiled schema tree.
type ResolvedPaths struct {
	GnmiPaths      []string // e.g. ["/srl_nokia-interfaces:interface"]
	NetconfFilter  string   // e.g. `<interface xmlns="urn:nokia.com:srlinux:chassis:interfaces"/>`
	Namespace      string   // YANG module namespace
	ModuleName     string   // YANG module name
	ModuleRevision string   // YANG module revision (latest)
}

// ResolvePath walks the compiled YANG entry tree to find the node at
// the given path and extracts metadata for gNMI/NETCONF operations.
// The path must use module-prefixed notation (e.g., "srl_nokia-interfaces:interface")
// or simple element names (e.g., "interfaces").
func (b *SchemaBundle) ResolvePath(yangPath string) (*ResolvedPaths, error) {
	if b.Root == nil {
		return nil, fmt.Errorf("schema bundle %s has no compiled root", b.Key.String())
	}

	entries := findEntryChain(b.Root, yangPath)
	if len(entries) == 0 {
		return nil, fmt.Errorf("path %q not found in schema %s", yangPath, b.Key.String())
	}

	leaf := entries[len(entries)-1]
	mod := entryModule(leaf)
	namespace := ""
	revision := ""
	moduleName := ""
	if mod != nil {
		namespace = mod.Namespace.Name
		moduleName = mod.Name
		if len(mod.Revision) > 0 {
			revision = latestRevision(mod.Revision)
		}
	}

	gnmiPath := buildGnmiPath(yangPath, moduleName)
	netconfFilter := buildNestedNetconfFilter(entries)

	return &ResolvedPaths{
		GnmiPaths:      []string{gnmiPath},
		NetconfFilter:  netconfFilter,
		Namespace:      namespace,
		ModuleName:     moduleName,
		ModuleRevision: revision,
	}, nil
}

// GenerateNetconfFilter generates the NETCONF subtree filter XML body
// for the given YANG path.
func (b *SchemaBundle) GenerateNetconfFilter(yangPath string) (string, error) {
	if b.Root == nil {
		return "", fmt.Errorf("schema bundle %s has no compiled root", b.Key.String())
	}

	entries := findEntryChain(b.Root, yangPath)
	if len(entries) == 0 {
		return "", fmt.Errorf("path %q not found in schema %s", yangPath, b.Key.String())
	}

	return buildNestedNetconfFilter(entries), nil
}

// PathValidationStatus indicates whether a YANG path was found in the schema.
type PathValidationStatus string

const (
	PathFound      PathValidationStatus = "found"
	PathNotFound   PathValidationStatus = "not_found"
	PathDeprecated PathValidationStatus = "deprecated"
)

// PathValidationResult records whether a single path was found in the schema.
type PathValidationResult struct {
	Path   string
	Status PathValidationStatus
}

// ValidateOperationPaths checks whether the YANG paths in the given
// operation mappings exist in the schema tree. This enables version
// drift detection at startup.
func (b *SchemaBundle) ValidateOperationPaths(mappings []OperationMapping) []PathValidationResult {
	var results []PathValidationResult
	for _, m := range mappings {
		for _, p := range m.NativePaths {
			results = append(results, b.validateSinglePath(p))
		}
		for _, p := range m.OCPaths {
			results = append(results, b.validateSinglePath(p))
		}
	}
	return results
}

func (b *SchemaBundle) validateSinglePath(path string) PathValidationResult {
	entries := findEntryChain(b.Root, path)
	if len(entries) == 0 {
		return PathValidationResult{Path: path, Status: PathNotFound}
	}
	entry := entries[len(entries)-1]
	if entry.Extra != nil {
		if vals, ok := entry.Extra["status"]; ok {
			for _, v := range vals {
				if s, ok := v.(string); ok && strings.Contains(strings.ToLower(s), "deprecated") {
					return PathValidationResult{Path: path, Status: PathDeprecated}
				}
			}
		}
	}
	return PathValidationResult{Path: path, Status: PathFound}
}

// findEntryChain walks the schema tree to find an entry matching the given
// path and returns the chain of entries from the first path segment to the
// leaf. Returns nil if any segment cannot be resolved.
// Supports both prefixed (module:element) and bare element names.
// Paths may be absolute (start with /) or relative.
func findEntryChain(root *yang.Entry, path string) []*yang.Entry {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return []*yang.Entry{root}
	}

	segments := strings.Split(path, "/")
	current := root
	chain := make([]*yang.Entry, 0, len(segments))

	for _, seg := range segments {
		if current.Dir == nil {
			return nil
		}

		// Try exact match first (includes module prefix like "srl_nokia-interfaces:interface").
		if child, ok := current.Dir[seg]; ok {
			chain = append(chain, child)
			current = child
			continue
		}

		// Try stripping module prefix (e.g., "srl-if:interface" -> "interface").
		bare := seg
		if idx := strings.Index(seg, ":"); idx >= 0 {
			bare = seg[idx+1:]
		}
		if bare != seg {
			if child, ok := current.Dir[bare]; ok {
				chain = append(chain, child)
				current = child
				continue
			}
		}

		// Try matching with any prefix in the Dir.
		found := false
		for name, child := range current.Dir {
			nameBare := name
			if idx := strings.Index(name, ":"); idx >= 0 {
				nameBare = name[idx+1:]
			}
			if nameBare == bare {
				chain = append(chain, child)
				current = child
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}

	return chain
}

// entryModule returns the yang.Module that owns the given entry.
func entryModule(entry *yang.Entry) *yang.Module {
	if entry == nil || entry.Node == nil {
		return nil
	}
	return moduleFromNode(entry.Node)
}

// moduleFromNode traverses parent nodes to find the enclosing module.
func moduleFromNode(node yang.Node) *yang.Module {
	for node != nil {
		if mod, ok := node.(*yang.Module); ok {
			return mod
		}
		node = node.ParentNode()
	}
	return nil
}

// latestRevision returns the most recent revision date string.
func latestRevision(revs []*yang.Revision) string {
	if len(revs) == 0 {
		return ""
	}
	latest := revs[0].Name
	for _, r := range revs[1:] {
		if r.Name > latest {
			latest = r.Name
		}
	}
	return latest
}

// buildGnmiPath constructs a gNMI path string with module prefix.
// Input: "interfaces" with module "test-module" -> "/test-module:interfaces"
// Input: "srl_nokia-interfaces:interface" -> "/srl_nokia-interfaces:interface"
func buildGnmiPath(yangPath, moduleName string) string {
	path := strings.TrimPrefix(yangPath, "/")
	segments := strings.Split(path, "/")

	var result []string
	for _, seg := range segments {
		if strings.Contains(seg, ":") {
			result = append(result, seg)
		} else if moduleName != "" {
			result = append(result, moduleName+":"+seg)
		} else {
			result = append(result, seg)
		}
	}

	return "/" + strings.Join(result, "/")
}

// buildNestedNetconfFilter builds a NETCONF subtree filter XML element
// from the full chain of entries (root to leaf). For single-segment paths
// it produces a self-closing element; for multi-segment paths it produces
// nested elements so the NETCONF server can match the full hierarchy.
//
// Example single-segment: <interface xmlns="urn:nokia..."/>
// Example multi-segment:  <system xmlns="ns1"><information xmlns="ns2"/></system>
func buildNestedNetconfFilter(entries []*yang.Entry) string {
	if len(entries) == 0 {
		return ""
	}

	// Build from innermost (leaf) to outermost.
	result := ""
	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		ns := ""
		if mod := entryModule(entry); mod != nil {
			ns = mod.Namespace.Name
		}

		if result == "" {
			// Leaf element: self-closing.
			if ns != "" {
				result = fmt.Sprintf(`<%s xmlns="%s"/>`, entry.Name, ns)
			} else {
				result = fmt.Sprintf(`<%s/>`, entry.Name)
			}
		} else {
			// Parent element: wraps the inner content.
			if ns != "" {
				result = fmt.Sprintf(`<%s xmlns="%s">%s</%s>`, entry.Name, ns, result, entry.Name)
			} else {
				result = fmt.Sprintf(`<%s>%s</%s>`, entry.Name, result, entry.Name)
			}
		}
	}
	return result
}
