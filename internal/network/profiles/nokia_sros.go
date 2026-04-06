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

func init() {
	Register(&Profile{
		Vendor:   "nokia",
		Platform: "sros",
		Operations: map[string]OperationDescriptor{
			OpGetInterfaces: {
				OperationID: OpGetInterfaces,
				Paths: []ProtocolPath{
					{
						Protocol: ProtocolGnmiOpenConfig,
						Paths:    []string{"/openconfig-interfaces:interfaces/interface"},
					},
					{
						Protocol: ProtocolGnmiNative,
						Paths:    []string{"/nokia-state:state/router[router-name=Base]/interface"},
					},
					{
						Protocol: ProtocolNetconfOpenConfig,
						Filter:   `<interfaces xmlns="http://openconfig.net/yang/interfaces"/>`,
					},
					{
						Protocol: ProtocolNetconfNative,
						Filter:   `<state xmlns="urn:nokia.com:sros:ns:yang:sr:state"><router><router-name>Base</router-name><interface/></router></state>`,
					},
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
						Protocol: ProtocolGnmiOpenConfig,
						Paths: []string{
							"/openconfig-system:system/state",
							"/openconfig-platform:components/component[name=chassis]/state",
						},
					},
					{
						Protocol: ProtocolGnmiNative,
						Paths:    []string{"/nokia-state:state/system/information"},
					},
					{
						Protocol: ProtocolNetconfOpenConfig,
						Filter:   `<system xmlns="http://openconfig.net/yang/system"/><components xmlns="http://openconfig.net/yang/platform"/>`,
					},
					{
						Protocol: ProtocolNetconfNative,
						Filter:   `<state xmlns="urn:nokia.com:sros:ns:yang:sr:state"><system><information/></system></state>`,
					},
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
		},
	})
}
