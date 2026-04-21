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

	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

// BuildProfile generates a profiles.Profile from a compiled SchemaBundle
// using the given operation mappings. For each mapping, it resolves
// native and OpenConfig YANG paths against the schema tree and
// generates gNMI paths and NETCONF filters.
//
// CLI paths are NOT generated — they must come from the hardcoded
// profile fallback.
//
// Returns the built profile and a list of warnings for paths that
// couldn't be resolved (version drift, missing models, etc.).
func BuildProfile(bundle *SchemaBundle, mappings []OperationMapping) (*profiles.Profile, []string) {
	var warnings []string
	ops := make(map[string]profiles.OperationDescriptor)

	for _, m := range mappings {
		// Collect resolved paths by protocol, combining gNMI paths and
		// NETCONF filters so each protocol appears as a single ProtocolPath.
		var ocGnmiPaths []string
		var ocNetconfFilters []string
		var nativeGnmiPaths []string
		var nativeNetconfFilters []string

		// Resolve OpenConfig paths.
		for _, ocPath := range m.OCPaths {
			resolved, err := bundle.ResolvePath(ocPath)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("operation %s: OC path %s: %v", m.OperationID, ocPath, err))
				continue
			}
			ocGnmiPaths = append(ocGnmiPaths, resolved.GnmiPaths...)
			if resolved.NetconfFilter != "" {
				ocNetconfFilters = append(ocNetconfFilters, resolved.NetconfFilter)
			}
		}

		// Resolve native YANG paths.
		for _, nativePath := range m.NativePaths {
			resolved, err := bundle.ResolvePath(nativePath)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("operation %s: native path %s: %v", m.OperationID, nativePath, err))
				continue
			}
			nativeGnmiPaths = append(nativeGnmiPaths, resolved.GnmiPaths...)
			if resolved.NetconfFilter != "" {
				nativeNetconfFilters = append(nativeNetconfFilters, resolved.NetconfFilter)
			}
		}

		byProtocol := make(map[profiles.Protocol]profiles.ProtocolPath)
		if len(ocGnmiPaths) > 0 {
			byProtocol[profiles.ProtocolGnmiOpenConfig] = profiles.ProtocolPath{
				Protocol: profiles.ProtocolGnmiOpenConfig,
				Paths:    ocGnmiPaths,
			}
		}
		if len(ocNetconfFilters) > 0 {
			byProtocol[profiles.ProtocolNetconfOpenConfig] = profiles.ProtocolPath{
				Protocol: profiles.ProtocolNetconfOpenConfig,
				Filter:   combineNetconfFilters(ocNetconfFilters),
			}
		}
		if len(nativeGnmiPaths) > 0 {
			byProtocol[profiles.ProtocolGnmiNative] = profiles.ProtocolPath{
				Protocol: profiles.ProtocolGnmiNative,
				Paths:    nativeGnmiPaths,
			}
		}
		if len(nativeNetconfFilters) > 0 {
			byProtocol[profiles.ProtocolNetconfNative] = profiles.ProtocolPath{
				Protocol: profiles.ProtocolNetconfNative,
				Filter:   combineNetconfFilters(nativeNetconfFilters),
			}
		}

		for proto, pp := range byProtocol {
			pp.Parameters = m.profileParameters()
			pp.Limits = m.profileLimits()
			if isNetconfProtocol(proto) && m.dataKind() == OperationDataConfig {
				pp.UseGetConfig = true
				pp.Datastore = m.datastore()
				byProtocol[proto] = pp
			}
			if isNetconfProtocol(proto) && m.dataKind() == OperationDataConfigState {
				// TODO(schema-ops): Split config_state operations into paired
				// config and state retrieval paths when both are declared in the
				// sidecar. For now NETCONF <get> preserves current behavior and
				// returns available config+state, but pure config datastores may
				// need an additional get-config RPC for exact config provenance.
				byProtocol[proto] = pp
			}
		}

		paths := orderedProtocolPaths(m, byProtocol)
		if len(paths) > 0 {
			ops[m.OperationID] = profiles.OperationDescriptor{
				OperationID: m.OperationID,
				Data:        m.profileDataKind(),
				Datastore:   m.datastore(),
				Parameters:  m.profileParameters(),
				Limits:      m.profileLimits(),
				Paths:       paths,
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("operation %s: no paths resolved", m.OperationID))
		}
	}

	profile := &profiles.Profile{
		Vendor:     bundle.Key.Vendor,
		Platform:   bundle.Key.Platform,
		Operations: ops,
	}

	return profile, warnings
}

func orderedProtocolPaths(m OperationMapping, byProtocol map[profiles.Protocol]profiles.ProtocolPath) []profiles.ProtocolPath {
	order := defaultProtocolPreference()
	if len(m.Preferred) > 0 {
		order = parseProtocolPreference(m.Preferred)
	}

	seen := make(map[profiles.Protocol]bool, len(order))
	var paths []profiles.ProtocolPath
	for _, proto := range order {
		pp, ok := byProtocol[proto]
		if !ok {
			continue
		}
		paths = append(paths, pp)
		seen[proto] = true
	}

	// Preserve any protocols omitted from a custom preference after preferred
	// entries. This keeps sidecar preference order additive, not destructive.
	for _, proto := range defaultProtocolPreference() {
		if seen[proto] {
			continue
		}
		if pp, ok := byProtocol[proto]; ok {
			paths = append(paths, pp)
		}
	}
	return paths
}

func defaultProtocolPreference() []profiles.Protocol {
	return []profiles.Protocol{
		profiles.ProtocolGnmiOpenConfig,
		profiles.ProtocolNetconfOpenConfig,
		profiles.ProtocolGnmiNative,
		profiles.ProtocolNetconfNative,
	}
}

func parseProtocolPreference(preferred []string) []profiles.Protocol {
	out := make([]profiles.Protocol, 0, len(preferred))
	for _, raw := range preferred {
		switch strings.ToLower(strings.TrimSpace(raw)) {
		case string(profiles.ProtocolGnmiOpenConfig), "gnmi_oc", "openconfig_gnmi":
			out = append(out, profiles.ProtocolGnmiOpenConfig)
		case string(profiles.ProtocolNetconfOpenConfig), "netconf_oc", "openconfig_netconf":
			out = append(out, profiles.ProtocolNetconfOpenConfig)
		case string(profiles.ProtocolGnmiNative), "native_gnmi":
			out = append(out, profiles.ProtocolGnmiNative)
		case string(profiles.ProtocolNetconfNative), "native_netconf":
			out = append(out, profiles.ProtocolNetconfNative)
		}
	}
	if len(out) == 0 {
		return defaultProtocolPreference()
	}
	return out
}

// combineNetconfFilters merges multiple NETCONF subtree filters.
// If filters share the same outermost element (same tag+namespace), their
// inner content is merged into one element. Otherwise filters are concatenated.
func combineNetconfFilters(filters []string) string {
	if len(filters) == 0 {
		return ""
	}
	if len(filters) == 1 {
		return filters[0]
	}

	// Try to merge filters that share the same root element.
	// A nested filter looks like: <system xmlns="ns">..inner..</system>
	// A leaf filter looks like: <interface xmlns="ns"/>
	type parsedFilter struct {
		openTag  string // e.g., `<system xmlns="ns">`
		closeTag string // e.g., `</system>`
		inner    string // inner content (empty for self-closing)
		raw      string // original string
	}

	parsed := make([]parsedFilter, 0, len(filters))
	for _, f := range filters {
		pf := parsedFilter{raw: f}
		// Find the end of the first tag.
		tagEnd := strings.Index(f, ">")
		if tagEnd < 0 {
			parsed = append(parsed, pf)
			continue
		}

		firstTag := f[:tagEnd+1]
		// Self-closing: <element xmlns="ns"/>
		if strings.HasSuffix(firstTag, "/>") {
			pf.openTag = strings.TrimSuffix(firstTag, "/>") + ">"
			// Extract element name for close tag.
			name := extractElementName(firstTag)
			pf.closeTag = "</" + name + ">"
			pf.inner = ""
		} else {
			pf.openTag = firstTag
			// Find the matching close tag.
			name := extractElementName(firstTag)
			pf.closeTag = "</" + name + ">"
			closeIdx := strings.LastIndex(f, pf.closeTag)
			if closeIdx > tagEnd {
				pf.inner = f[tagEnd+1 : closeIdx]
			}
		}
		parsed = append(parsed, pf)
	}

	// Group by openTag.
	groups := make(map[string][]parsedFilter)
	var order []string
	for _, pf := range parsed {
		if pf.openTag == "" {
			// Unparseable, keep raw.
			groups[pf.raw] = append(groups[pf.raw], pf)
			order = append(order, pf.raw)
			continue
		}
		if _, exists := groups[pf.openTag]; !exists {
			order = append(order, pf.openTag)
		}
		groups[pf.openTag] = append(groups[pf.openTag], pf)
	}

	var result []string
	for _, key := range order {
		group := groups[key]
		if len(group) == 1 {
			result = append(result, group[0].raw)
			continue
		}
		// Merge inner contents.
		var inners []string
		for _, pf := range group {
			if pf.inner != "" {
				inners = append(inners, pf.inner)
			}
		}
		if len(inners) > 0 {
			result = append(result, group[0].openTag+strings.Join(inners, "")+group[0].closeTag)
		} else {
			result = append(result, group[0].raw)
		}
	}

	return strings.Join(result, "")
}

// extractElementName extracts the element name from an XML open tag.
// e.g., `<system xmlns="ns">` -> "system", `<interface/>` -> "interface"
func extractElementName(tag string) string {
	s := strings.TrimPrefix(tag, "<")
	// Find end of element name (space, /, or >).
	for i, c := range s {
		if c == ' ' || c == '/' || c == '>' {
			return s[:i]
		}
	}
	return s
}

// MergeProfiles merges a schema-derived profile with a hardcoded fallback
// profile. Schema-derived gNMI and NETCONF paths replace the hardcoded
// ones, while CLI paths are preserved from the fallback.
func MergeProfiles(schemaProfile, fallback *profiles.Profile) *profiles.Profile {
	if fallback == nil {
		return schemaProfile
	}
	if schemaProfile == nil {
		return fallback
	}

	merged := &profiles.Profile{
		Vendor:             schemaProfile.Vendor,
		Platform:           schemaProfile.Platform,
		Version:            schemaProfile.Version,
		Operations:         make(map[string]profiles.OperationDescriptor),
		DiagnosticCommands: mergeDiagnosticCommands(schemaProfile.DiagnosticCommands, fallback.DiagnosticCommands),
	}

	// Start with all operations from the fallback.
	for opID, op := range fallback.Operations {
		merged.Operations[opID] = op
	}

	// For each schema-derived operation, replace gNMI/NETCONF paths
	// but keep CLI paths from the fallback.
	for opID, schemaOp := range schemaProfile.Operations {
		fallbackOp, hasFallback := fallback.Operations[opID]

		var mergedPaths []profiles.ProtocolPath

		// Add schema-derived gNMI/NETCONF paths first (preferred).
		for _, pp := range schemaOp.Paths {
			if !isCLIProtocol(pp.Protocol) {
				mergedPaths = append(mergedPaths, pp)
			}
		}

		// Carry over CLI paths from the fallback.
		if hasFallback {
			for _, pp := range fallbackOp.Paths {
				if isCLIProtocol(pp.Protocol) {
					mergedPaths = append(mergedPaths, pp)
				}
			}
		}

		merged.Operations[opID] = profiles.OperationDescriptor{
			OperationID: opID,
			Data:        schemaOp.Data,
			Datastore:   schemaOp.Datastore,
			Parameters:  schemaOp.Parameters,
			Limits:      schemaOp.Limits,
			Paths:       mergedPaths,
		}
	}

	return merged
}

func mergeDiagnosticCommands(primary, fallback map[string]profiles.DiagnosticCommandTemplate) map[string]profiles.DiagnosticCommandTemplate {
	if len(primary) == 0 && len(fallback) == 0 {
		return nil
	}
	merged := make(map[string]profiles.DiagnosticCommandTemplate, len(primary)+len(fallback))
	for k, v := range fallback {
		merged[k] = v
	}
	for k, v := range primary {
		merged[k] = v
	}
	return merged
}

func isCLIProtocol(p profiles.Protocol) bool {
	return p == profiles.ProtocolCLI
}

func isNetconfProtocol(p profiles.Protocol) bool {
	return p == profiles.ProtocolNetconfOpenConfig || p == profiles.ProtocolNetconfNative
}
