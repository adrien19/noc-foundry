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

// Package models defines shared data types for network tool results.
// The canonical schema uses OpenConfig semantics as its baseline.
// All ingestion paths (OpenConfig, native YANG, CLI) normalize into
// these types. Fields not mappable to the canonical core go into
// VendorExtensions with explicit MappingQuality metadata.
package models

import "time"

// Schema version for the canonical record envelope.
const SchemaVersion = "1.0"

// MappingQuality indicates how reliably a native field was mapped
// to the canonical schema.
type MappingQuality string

const (
	MappingExact   MappingQuality = "exact"   // 1:1 semantic match
	MappingDerived MappingQuality = "derived" // computed/transformed from source data
	MappingPartial MappingQuality = "partial" // some information loss
)

// CollectionMode describes how data was collected.
type CollectionMode string

const (
	CollectionSnapshot CollectionMode = "snapshot"
)

// Protocol identifies the retrieval protocol used.
type Protocol string

const (
	ProtocolGnmiOpenConfig    Protocol = "gnmi_openconfig"
	ProtocolGnmiNative        Protocol = "gnmi_native"
	ProtocolCLI               Protocol = "cli"
	ProtocolNetconfOpenConfig Protocol = "netconf_openconfig"
	ProtocolNetconfNative     Protocol = "netconf_native"
)

// ---------------------------------------------------------------------------
// Record envelope — wraps every canonical domain payload.
// ---------------------------------------------------------------------------

// Record is the top-level envelope returned by all query operations.
type Record struct {
	RecordType    string         `json:"record_type"`
	SchemaVersion string         `json:"schema_version"`
	Source        SourceMeta     `json:"source"`
	Collection    CollectionMeta `json:"collection"`
	Payload       any            `json:"payload"`
	Quality       QualityMeta    `json:"quality"`
	Native        *NativeMeta    `json:"native,omitempty"`
}

// SourceMeta identifies the device and transport that produced the data.
type SourceMeta struct {
	DeviceID  string `json:"device_id"`
	Vendor    string `json:"vendor"`
	Platform  string `json:"platform"`
	OSVersion string `json:"os_version,omitempty"`
	Transport string `json:"transport"` // gnmi, cli
}

// CollectionMeta describes when and how data was retrieved.
type CollectionMeta struct {
	Mode        CollectionMode `json:"mode"`
	Protocol    Protocol       `json:"protocol"`
	CollectedAt time.Time      `json:"collected_at"`
}

// QualityMeta conveys mapping fidelity and any caveats.
type QualityMeta struct {
	MappingQuality MappingQuality `json:"mapping_quality"`
	Warnings       []string       `json:"warnings,omitempty"`
}

// NativeMeta preserves provenance of the original data source.
type NativeMeta struct {
	ModelName     string `json:"model_name,omitempty"`
	ModelRevision string `json:"model_revision,omitempty"`
	NativePath    string `json:"native_path,omitempty"`
}

// ---------------------------------------------------------------------------
// Domain schemas — OpenConfig-informed canonical types.
// ---------------------------------------------------------------------------

// InterfaceState represents a single interface record.
// Field semantics follow openconfig-interfaces:interfaces/interface/state.
type InterfaceState struct {
	Name             string             `json:"name"`
	Type             string             `json:"type,omitempty"`
	AdminStatus      string             `json:"admin_status"` // UP, DOWN
	OperStatus       string             `json:"oper_status"`  // UP, DOWN
	Description      string             `json:"description,omitempty"`
	MTU              int                `json:"mtu,omitempty"`
	Speed            string             `json:"speed,omitempty"`
	Counters         *InterfaceCounters `json:"counters,omitempty"`
	VendorExtensions map[string]any     `json:"vendor_extensions,omitempty"`
}

// InterfaceCounters holds traffic statistics for an interface.
// Field semantics follow openconfig-interfaces counters container.
type InterfaceCounters struct {
	InOctets    uint64 `json:"in_octets,omitempty"`
	OutOctets   uint64 `json:"out_octets,omitempty"`
	InPackets   uint64 `json:"in_packets,omitempty"`
	OutPackets  uint64 `json:"out_packets,omitempty"`
	InErrors    uint64 `json:"in_errors,omitempty"`
	OutErrors   uint64 `json:"out_errors,omitempty"`
	InDiscards  uint64 `json:"in_discards,omitempty"`
	OutDiscards uint64 `json:"out_discards,omitempty"`
}

// SystemVersion represents device identity and software information.
// Field semantics follow openconfig-system:system/state.
type SystemVersion struct {
	Hostname         string         `json:"hostname"`
	SoftwareVersion  string         `json:"software_version"`
	SystemType       string         `json:"system_type,omitempty"`
	ChassisType      string         `json:"chassis_type,omitempty"`
	Uptime           string         `json:"uptime,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

// ---------------------------------------------------------------------------
// Legacy type — kept for backward compatibility with existing CLI-only tools.
// ---------------------------------------------------------------------------

// CommandResult holds structured output from a device command execution.
type CommandResult struct {
	RawOutput string
	Command   string
	ToolKind  string
	Source    string
}
