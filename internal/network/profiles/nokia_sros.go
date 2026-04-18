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

// Nokia SR OS hardcoded profile. Contains only CLI paths; gNMI and
// NETCONF paths are generated from compiled YANG schemas at startup by
// schemas.BuildAndRegisterProfiles and merged via MergeProfiles.
func init() {
	Register(&Profile{
		Vendor:   "nokia",
		Platform: "sros",
		DiagnosticCommands: map[string]DiagnosticCommandTemplate{
			OpRunPing: {
				OperationID: OpRunPing,
				Command:     "ping {target} count {count}",
				Optional: []DiagnosticCommandFragment{
					{Parameter: "vrf", Template: " router-instance {vrf}"},
					{Parameter: "source", Template: " source-address {source}"},
				},
			},
			OpRunTraceroute: {
				OperationID: OpRunTraceroute,
				Command:     "traceroute {target} max-ttl {max_hops}",
				Optional: []DiagnosticCommandFragment{
					{Parameter: "vrf", Template: " router-instance {vrf}"},
					{Parameter: "source", Template: " source-address {source}"},
				},
			},
			OpGetConfigurationDiff: {
				OperationID: OpGetConfigurationDiff,
				Command:     "show configuration compare {source} {target}",
			},
		},
		Operations: map[string]OperationDescriptor{
			OpGetInterfaces: {
				OperationID: OpGetInterfaces,
				Paths: []ProtocolPath{
					// JSON CLI path requires SROS 20.x+ with MD-CLI enabled.
					// Falls back to text table format on older releases.
					{
						Protocol:  ProtocolCLI,
						Command:   "show router interface",
						Format:    "json",
						FormatArg: "| json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show router interface",
						Format:   "text",
					},
				},
			},
			OpGetSystemVersion: {
				OperationID: OpGetSystemVersion,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show system information",
						Format:    "json",
						FormatArg: "| json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show system information",
						Format:   "text",
					},
				},
			},
			OpGetBGPNeighbors: {
				OperationID: OpGetBGPNeighbors,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show router bgp summary",
						Format:    "json",
						FormatArg: "| json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show router bgp summary",
						Format:   "text",
					},
				},
			},
			OpGetRouteTable: {
				OperationID: OpGetRouteTable,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show router route-table",
						Format:    "json",
						FormatArg: "| json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show router route-table",
						Format:   "text",
					},
				},
			},
			OpGetSystemAlarms: {
				OperationID: OpGetSystemAlarms,
				Paths: []ProtocolPath{
					{
						Protocol:  ProtocolCLI,
						Command:   "show log event-control",
						Format:    "json",
						FormatArg: "| json",
					},
					{
						Protocol: ProtocolCLI,
						Command:  "show log event-control",
						Format:   "text",
					},
				},
			},
		},
	})
}
