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

package profiles

// Nokia SR Linux hardcoded profile. Contains only CLI paths; gNMI and
// NETCONF paths are generated from compiled YANG schemas at startup by
// schemas.BuildAndRegisterProfiles and merged via MergeProfiles.
func init() {
	Register(&Profile{
		Vendor:   "nokia",
		Platform: "srlinux",
		Operations: map[string]OperationDescriptor{
			OpGetInterfaces: {
				OperationID: OpGetInterfaces,
				Paths: []ProtocolPath{
					// JSON CLI path is preferred for structured output; falls back to
					// text if the device firmware does not support the | json flag.
					{
						Protocol:  ProtocolCLI,
						Command:   "show interface",
						Format:    "json",
						FormatArg: "| as json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show interface",
						Format:   "text",
					},
				},
			},
			OpGetSystemVersion: {
				OperationID: OpGetSystemVersion,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show version",
						Format:    "json",
						FormatArg: "| as json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show version",
						Format:   "text",
					},
				},
			},
			OpGetBGPNeighbors: {
				OperationID: OpGetBGPNeighbors,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show network-instance default protocols bgp neighbor",
						Format:    "json",
						FormatArg: "| as json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show network-instance default protocols bgp neighbor",
						Format:   "text",
					},
				},
			},
			OpGetRouteTable: {
				OperationID: OpGetRouteTable,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show network-instance default route-table",
						Format:    "json",
						FormatArg: "| as json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show network-instance default route-table",
						Format:   "text",
					},
				},
			},
			OpGetSystemAlarms: {
				OperationID: OpGetSystemAlarms,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show system alarm",
						Format:    "json",
						FormatArg: "| as json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show system alarm",
						Format:   "text",
					},
				},
			},
		},
	})
}
