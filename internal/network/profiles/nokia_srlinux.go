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
		Platform: "srlinux",
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
						Paths:    []string{"/srl_nokia-interfaces:interface"},
					},
					{
						Protocol: ProtocolNetconfOpenConfig,
						Filter:   `<interfaces xmlns="http://openconfig.net/yang/interfaces"/>`,
					},
					{
						Protocol: ProtocolNetconfNative,
						Filter:   `<interface xmlns="urn:nokia.com:srlinux:chassis:interfaces"/>`,
					},
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
						Protocol: ProtocolGnmiOpenConfig,
						Paths: []string{
							"/openconfig-system:system/state",
							"/openconfig-platform:components/component[name=chassis]/state",
						},
					},
					{
						Protocol: ProtocolGnmiNative,
						// Fetch both the version info and the system name in one Get.
						Paths: []string{
							"/srl_nokia-system:system/information",
							"/srl_nokia-system:system/name",
						},
					},
					{
						Protocol: ProtocolNetconfOpenConfig,
						Filter:   `<system xmlns="http://openconfig.net/yang/system"/><components xmlns="http://openconfig.net/yang/platform"/>`,
					},
					{
						Protocol: ProtocolNetconfNative,
						// Each child element needs its own namespace since SR Linux
						// splits system info across separate YANG modules.
						Filter: `<system xmlns="urn:nokia.com:srlinux:general:system"><information xmlns="urn:nokia.com:srlinux:linux:system-info"/><name xmlns="urn:nokia.com:srlinux:chassis:system-name"/></system>`,
					},
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
		},
	})
}
