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

// OperationMapping defines the mapping from a well-known operation ID
// to the YANG paths used to resolve that operation against a compiled
// schema tree. NativePaths are vendor-native YANG paths; OCPaths are
// OpenConfig paths.
type OperationMapping struct {
	OperationID string
	NativePaths []string
	OCPaths     []string
}

// NokiaSRLinuxMappings defines the YANG path mappings for Nokia SR Linux
// devices. These paths are validated against the loaded schema at startup.
var NokiaSRLinuxMappings = []OperationMapping{
	{
		OperationID: "get_interfaces",
		NativePaths: []string{"/srl_nokia-interfaces:interface"},
		OCPaths:     []string{"/openconfig-interfaces:interfaces/interface"},
	},
	{
		OperationID: "get_system_version",
		NativePaths: []string{
			"/srl_nokia-system:system/information",
			"/srl_nokia-system:system/name",
		},
		OCPaths: []string{
			"/openconfig-system:system/state",
		},
	},
}

// NokiaSROSMappings defines the YANG path mappings for Nokia SR OS
// (7x50) devices.
var NokiaSROSMappings = []OperationMapping{
	{
		OperationID: "get_interfaces",
		NativePaths: []string{"/nokia-state:state/port"},
		OCPaths:     []string{"/openconfig-interfaces:interfaces/interface"},
	},
	{
		OperationID: "get_system_version",
		NativePaths: []string{
			"/nokia-state:state/system/information",
		},
		OCPaths: []string{
			"/openconfig-system:system/state",
		},
	},
}

// OperationMappingsForVendor returns the operation mappings for the given
// vendor and platform. Returns nil if no mappings are defined.
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
