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

import "strings"

// FieldMapping binds a YANG leaf name (as it appears in JSON keys or XML
// element names) to a canonical model field. The optional Normalizer
// identifies a registered StatusNormalizerFunc that post-processes the
// raw value (e.g. "enable" → "UP").
//
// A single OperationCanonicalMap contains field mappings for ALL known
// vendor leaf names — this is intentional. The mapper tries each mapping
// against the keys present in the response and ignores those that don't
// match. This means one table covers OpenConfig, Nokia SRL, Nokia SROS,
// and any future vendor without per-vendor branching.
type FieldMapping struct {
	// YANGLeaf is the JSON key / XML local-name to match.
	YANGLeaf string
	// CanonicalField is the target field name on the canonical model struct
	// (e.g. "Name", "AdminStatus", "MTU").
	CanonicalField string
	// Normalizer is the optional name of a registered StatusNormalizerFunc.
	// When empty, the raw string value is used as-is.
	Normalizer string
}

// ContainerAlias allows the mapper to descend into known sub-containers
// (e.g. OpenConfig "state" or "config") and apply the same field mappings
// there. This handles the structural difference between flat models (SRL)
// and nested models (OpenConfig).
type ContainerAlias struct {
	// Name is the JSON key / XML local-name of the sub-container.
	Name string
	// MergeUp when true means fields found inside this container are
	// treated as if they appeared at the parent level.
	MergeUp bool
}

// OperationCanonicalMap defines how to extract a canonical model from
// a parsed JSON/XML response for a specific operation. It is intentionally
// vendor-agnostic: the Fields slice contains ALL known leaf names across
// all supported vendors, and the mapper applies whichever ones match.
type OperationCanonicalMap struct {
	// OperationID matches the well-known operation ID (e.g. "get_interfaces").
	OperationID string

	// ModelType identifies the canonical model struct: "InterfaceState",
	// "SystemVersion", etc. Used by the mapper to construct the right type.
	ModelType string

	// Fields maps YANG leaf names to canonical model fields.
	Fields []FieldMapping

	// ContainerAliases lists sub-containers that the mapper should descend
	// into and merge fields upward (e.g. OpenConfig "state", "config").
	ContainerAliases []ContainerAlias
}

// ---------------------------------------------------------------------------
// Canonical map registry
// ---------------------------------------------------------------------------

var canonicalMaps = map[string]*OperationCanonicalMap{}

// RegisterCanonicalMap registers a canonical mapping for an operation.
func RegisterCanonicalMap(m *OperationCanonicalMap) {
	canonicalMaps[m.OperationID] = m
}

// LookupCanonicalMap returns the canonical mapping for the given operation.
func LookupCanonicalMap(operationID string) (*OperationCanonicalMap, bool) {
	m, ok := canonicalMaps[operationID]
	return m, ok
}

// ---------------------------------------------------------------------------
// Status normalizer registry
// ---------------------------------------------------------------------------

// StatusNormalizerFunc transforms a raw vendor-specific status value into
// a canonical value (e.g. "enable" → "UP", "outOfService" → "DOWN").
type StatusNormalizerFunc func(value string) string

var statusNormalizers = map[string]StatusNormalizerFunc{}

// RegisterStatusNormalizer registers a named normalizer function.
func RegisterStatusNormalizer(name string, fn StatusNormalizerFunc) {
	statusNormalizers[name] = fn
}

// NormalizeValue applies the named normalizer to value. If no normalizer
// is registered under that name, it returns the value unchanged.
func NormalizeValue(normalizerName, value string) string {
	if fn, ok := statusNormalizers[normalizerName]; ok {
		return fn(value)
	}
	return value
}

// ---------------------------------------------------------------------------
// Built-in normalizers — cover OpenConfig, Nokia SRL, Nokia SROS enum values.
// ---------------------------------------------------------------------------

func init() {
	// Unified admin-status normalizer covering all known vendor values.
	RegisterStatusNormalizer("admin_status", func(value string) string {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "up", "enable", "enabled", "inservice":
			return "UP"
		case "down", "disable", "disabled", "outofservice", "shutdown":
			return "DOWN"
		default:
			return value
		}
	})

	// Unified oper-status normalizer.
	RegisterStatusNormalizer("oper_status", func(value string) string {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "up", "inservice":
			return "UP"
		case "down", "outofservice", "shutdown":
			return "DOWN"
		default:
			return value
		}
	})

	// ---------------------------------------------------------------------------
	// Built-in canonical maps
	// ---------------------------------------------------------------------------

	// get_interfaces — covers OpenConfig, Nokia SRL, Nokia SROS leaf names.
	RegisterCanonicalMap(&OperationCanonicalMap{
		OperationID: "get_interfaces",
		ModelType:   "InterfaceState",
		Fields: []FieldMapping{
			// Name — universal across all vendors.
			{YANGLeaf: "name", CanonicalField: "Name"},
			{YANGLeaf: "interface-name", CanonicalField: "Name"}, // SROS

			// Type
			{YANGLeaf: "type", CanonicalField: "Type"},

			// Admin status — different leaf names and enum values per vendor.
			{YANGLeaf: "admin-status", CanonicalField: "AdminStatus", Normalizer: "admin_status"},
			{YANGLeaf: "admin-state", CanonicalField: "AdminStatus", Normalizer: "admin_status"},

			// Oper status
			{YANGLeaf: "oper-status", CanonicalField: "OperStatus", Normalizer: "oper_status"},
			{YANGLeaf: "oper-state", CanonicalField: "OperStatus", Normalizer: "oper_status"},

			// Description
			{YANGLeaf: "description", CanonicalField: "Description"},

			// MTU
			{YANGLeaf: "mtu", CanonicalField: "MTU"},

			// Speed
			{YANGLeaf: "speed", CanonicalField: "Speed"},
		},
		ContainerAliases: []ContainerAlias{
			{Name: "state", MergeUp: true},  // OpenConfig: interface/state/{admin-status,...}
			{Name: "config", MergeUp: true}, // OpenConfig: interface/config/{description,...}
		},
	})

	// get_system_version — covers OpenConfig, Nokia SRL, Nokia SROS.
	RegisterCanonicalMap(&OperationCanonicalMap{
		OperationID: "get_system_version",
		ModelType:   "SystemVersion",
		Fields: []FieldMapping{
			// Hostname
			{YANGLeaf: "hostname", CanonicalField: "Hostname"},
			{YANGLeaf: "host-name", CanonicalField: "Hostname"}, // SRL

			// Software version
			{YANGLeaf: "software-version", CanonicalField: "SoftwareVersion"},
			{YANGLeaf: "version", CanonicalField: "SoftwareVersion"}, // SRL

			// System type
			{YANGLeaf: "system-type", CanonicalField: "SystemType"},
			{YANGLeaf: "type", CanonicalField: "SystemType"},

			// Chassis type
			{YANGLeaf: "chassis-type", CanonicalField: "ChassisType"},

			// Uptime
			{YANGLeaf: "uptime", CanonicalField: "Uptime"},
			{YANGLeaf: "current-datetime", CanonicalField: "Uptime"},
		},
		ContainerAliases: []ContainerAlias{
			{Name: "state", MergeUp: true},       // OpenConfig
			{Name: "information", MergeUp: true}, // SRL: system/information/version
			{Name: "name", MergeUp: true},        // SRL: system/name/host-name
		},
	})
}
