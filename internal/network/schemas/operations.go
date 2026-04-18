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
	"strings"
	"sync"

	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

// OperationDataKind describes what kind of data an operation retrieves.
// It lets schema-derived profile generation choose the correct NETCONF RPC
// and lets tools apply operator-safety limits for large or raw operations.
type OperationDataKind string

const (
	OperationDataState       OperationDataKind = "state"
	OperationDataConfig      OperationDataKind = "config"
	OperationDataConfigState OperationDataKind = "config_state"
	OperationDataRPC         OperationDataKind = "rpc"
	OperationDataRaw         OperationDataKind = "raw"
)

func (k OperationDataKind) withDefault() OperationDataKind {
	if k == "" {
		return OperationDataState
	}
	return k
}

// OperationMapping defines the mapping from a well-known operation ID
// to the YANG paths used to resolve that operation against a compiled
// schema tree. NativePaths are vendor-native YANG paths; OCPaths are
// OpenConfig paths.
type OperationMapping struct {
	OperationID string
	Data        OperationDataKind
	Datastore   string
	Preferred   []string
	Parameters  []OperationParameter
	Limits      *OperationLimits
	NativePaths []string
	OCPaths     []string
}

func (m OperationMapping) profileParameters() []profiles.OperationParameter {
	out := make([]profiles.OperationParameter, 0, len(m.Parameters))
	for _, p := range m.Parameters {
		out = append(out, profiles.OperationParameter{
			Name:                  p.Name,
			PathKey:               p.PathKey,
			TargetPath:            p.TargetPath,
			TargetContainer:       p.TargetContainer,
			GnmiPathTemplate:      p.GnmiPathTemplate,
			NetconfFilterTemplate: p.NetconfFilterTemplate,
			Default:               p.Default,
			Required:              p.Required,
			Allowed:               p.Allowed,
			Description:           p.Description,
		})
	}
	return out
}

func (m OperationMapping) profileLimits() *profiles.OperationLimits {
	if m.Limits == nil {
		return nil
	}
	return &profiles.OperationLimits{
		DefaultCount: m.Limits.DefaultCount,
		MaxCount:     m.Limits.MaxCount,
		MaxBytes:     m.Limits.MaxBytes,
	}
}

func (m OperationMapping) profileDataKind() profiles.OperationDataKind {
	switch m.dataKind() {
	case OperationDataConfig:
		return profiles.OperationDataConfig
	case OperationDataConfigState:
		return profiles.OperationDataConfigState
	case OperationDataRPC:
		return profiles.OperationDataRPC
	case OperationDataRaw:
		return profiles.OperationDataRaw
	default:
		return profiles.OperationDataState
	}
}

func (m OperationMapping) dataKind() OperationDataKind {
	return m.Data.withDefault()
}

func (m OperationMapping) datastore() string {
	if strings.TrimSpace(m.Datastore) != "" {
		return m.Datastore
	}
	if m.dataKind() == OperationDataConfig {
		return "running"
	}
	return ""
}

// NokiaSRLinuxMappings defines the YANG path mappings for Nokia SR Linux
// devices. These paths are validated against the loaded schema at startup.
//
// TODO(schema-ops): Prefer the prebuilt or repo-provided nocfoundry-ops.yaml
// sidecar for Nokia SR Linux operation mappings. This hardcoded fallback is
// only for deployments without a sidecar and should not grow as the primary
// mapping mechanism; new vendors should ship sidecars or configure opsFile.
var NokiaSRLinuxMappings = []OperationMapping{
	{
		OperationID: "get_interfaces",
		Data:        OperationDataState,
		NativePaths: []string{"/srl_nokia-interfaces:interface"},
		OCPaths:     []string{"/openconfig-interfaces:interfaces/interface"},
	},
	{
		OperationID: "get_system_version",
		Data:        OperationDataState,
		NativePaths: []string{
			"/srl_nokia-system:system/information",
			"/srl_nokia-system:system/name",
		},
		OCPaths: []string{
			"/openconfig-system:system/state",
		},
	},
	{
		OperationID: "get_bgp_neighbors",
		Data:        OperationDataState,
		NativePaths: []string{
			"/srl_nokia-network-instance:network-instance/protocols/srl_nokia-bgp:bgp/neighbor",
		},
		OCPaths: []string{
			"/openconfig-network-instance:network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state",
		},
	},
	{
		OperationID: "get_route_table",
		Data:        OperationDataState,
		Limits:      &OperationLimits{DefaultCount: 1000, MaxCount: 10000},
		NativePaths: []string{
			"/srl_nokia-network-instance:network-instance/route-table/srl_nokia-ip-route-tables:ipv4-unicast/route",
		},
		OCPaths: []string{
			"/openconfig-network-instance:network-instances/network-instance/afts/ipv4-unicast/ipv4-entry",
		},
	},
	{
		OperationID: "get_system_alarms",
		Data:        OperationDataState,
		NativePaths: []string{
			"/srl_nokia-system:system/srl_nokia-system-alarm:alarm",
		},
		OCPaths: []string{
			"/openconfig-system:system/alarms/alarm/state",
		},
	},
}

// NokiaSROSMappings defines the YANG path mappings for Nokia SR OS
// (7x50) devices.
//
// TODO(schema-ops): Replace this fallback with a prebuilt SR OS sidecar once
// SR OS native YANG operation contracts have been validated. Hardcoded YANG
// fallbacks are a compatibility safety net, while sidecars/opsFile are the
// intended source of truth.
var NokiaSROSMappings = []OperationMapping{
	{
		OperationID: "get_interfaces",
		Data:        OperationDataState,
		NativePaths: []string{"/nokia-state:state/port"},
		OCPaths:     []string{"/openconfig-interfaces:interfaces/interface"},
	},
	{
		OperationID: "get_system_version",
		Data:        OperationDataState,
		NativePaths: []string{
			"/nokia-state:state/system/information",
		},
		OCPaths: []string{
			"/openconfig-system:system/state",
		},
	},
	{
		OperationID: "get_bgp_neighbors",
		Data:        OperationDataState,
		NativePaths: []string{
			"/nokia-state:state/router/bgp/neighbor",
		},
		OCPaths: []string{
			"/openconfig-network-instance:network-instances/network-instance/protocols/protocol/bgp/neighbors/neighbor/state",
		},
	},
	{
		OperationID: "get_route_table",
		Data:        OperationDataState,
		Limits:      &OperationLimits{DefaultCount: 1000, MaxCount: 10000},
		NativePaths: []string{
			"/nokia-state:state/router/route-table/unicast/ipv4",
		},
		OCPaths: []string{
			"/openconfig-network-instance:network-instances/network-instance/afts/ipv4-unicast/ipv4-entry",
		},
	},
	{
		OperationID: "get_system_alarms",
		Data:        OperationDataState,
		NativePaths: []string{
			"/nokia-state:state/system/alarm",
		},
		OCPaths: []string{
			"/openconfig-system:system/alarms/alarm/state",
		},
	},
}

// OperationMappingsForVendor returns the hardcoded operation mappings for the
// given vendor and platform. Returns nil if no mappings are defined.
// This is the fallback used when no sidecar file is present.
func OperationMappingsForVendor(vendor, platform string) []OperationMapping {
	switch {
	case vendor == "nokia" && platform == "srlinux":
		return NokiaSRLinuxMappings
	case vendor == "nokia" && platform == "sros":
		return NokiaSROSMappings
	default:
		return nil
	}
}

// ---------------------------------------------------------------------------
// Sidecar mapping registry
// ---------------------------------------------------------------------------

// sidecarMappings stores operation mappings loaded from nocfoundry-ops.yaml
// sidecar files. Key: SchemaKey.String() → []OperationMapping.
var sidecarMappings sync.Map

// RegisterSidecarMappings stores sidecar-provided operation mappings for
// the given schema key. These take priority over hardcoded mappings.
func RegisterSidecarMappings(key SchemaKey, mappings []OperationMapping) {
	sidecarMappings.Store(key.String(), mappings)
}

// GetOperationMappings returns the operation mappings for a vendor/platform/version.
// It first checks the sidecar registry (exact key match), then falls back
// to the hardcoded OperationMappingsForVendor switch.
func GetOperationMappings(vendor, platform, version string) []OperationMapping {
	key := SchemaKey{Vendor: vendor, Platform: platform, Version: version}
	if v, ok := sidecarMappings.Load(key.String()); ok {
		return v.([]OperationMapping)
	}
	return OperationMappingsForVendor(vendor, platform)
}

// ResetSidecarMappings clears the sidecar mapping registry. For testing only.
func ResetSidecarMappings() {
	sidecarMappings.Range(func(key, _ any) bool {
		sidecarMappings.Delete(key)
		return true
	})
}
