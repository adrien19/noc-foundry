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

// Package profiles implements a model profile registry keyed by
// vendor.platform. Each profile defines per-operation descriptors
// that specify protocol preference, path/command templates, and
// required source capabilities.
package profiles

import (
	"fmt"
	"strings"
	"sync"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
)

// Well-known operation IDs.
const (
	OpGetInterfaces         = "get_interfaces"
	OpGetInterfaceCounters  = "get_interface_counters"
	OpGetLLDPNeighbors      = "get_lldp_neighbors"
	OpGetVLANs              = "get_vlans"
	OpGetLACP               = "get_lacp"
	OpGetMACTable           = "get_mac_table"
	OpGetSTP                = "get_stp"
	OpGetBGPNeighbors       = "get_bgp_neighbors"
	OpGetBGPRIB             = "get_bgp_rib"
	OpGetOSPFNeighbors      = "get_ospf_neighbors"
	OpGetOSPFDatabase       = "get_ospf_database"
	OpGetISISAdjacencies    = "get_isis_adjacencies"
	OpGetRouteTable         = "get_route_table"
	OpGetStaticRoutes       = "get_static_routes"
	OpGetMPLSLSPs           = "get_mpls_lsps"
	OpGetSegmentRouting     = "get_segment_routing"
	OpGetMulticast          = "get_multicast"
	OpGetBFDSessions        = "get_bfd_sessions"
	OpGetNetworkInstances   = "get_network_instances"
	OpGetSystemVersion      = "get_system_version"
	OpGetSystemAlarms       = "get_system_alarms"
	OpGetSystemCPU          = "get_system_cpu"
	OpGetSystemMemory       = "get_system_memory"
	OpGetPlatformComponents = "get_platform_components"
	OpGetTransceiverState   = "get_transceiver_state"
	OpGetSystemNTP          = "get_system_ntp"
	OpGetSystemDNS          = "get_system_dns"
	OpGetSystemAAA          = "get_system_aaa"
	OpGetSystemLogging      = "get_system_logging"
	OpGetSystemProcesses    = "get_system_processes"
	OpGetACL                = "get_acl"
	OpGetACLCounters        = "get_acl_counters"
	OpGetPolicyForwarding   = "get_policy_forwarding"
	OpGetQoSInterfaces      = "get_qos_interfaces"
	OpGetQoSClassifiers     = "get_qos_classifiers"
	OpGetRoutingPolicy      = "get_routing_policy"
	OpGetPrefixSets         = "get_prefix_sets"
	OpGetCommunitySets      = "get_community_sets"
	OpGetEVPNInstances      = "get_evpn_instances"
	OpGetVXLANTunnels       = "get_vxlan_tunnels"
	OpGetEthernetSegments   = "get_ethernet_segments"
	OpRunCommand            = "run_command"
	OpRunPing               = "run_ping"
	OpRunTraceroute         = "run_traceroute"
	OpGetLogEntries         = "get_log_entries"
	OpGetConfigurationDiff  = "get_configuration_diff"
	OpGetEnvironment        = "get_environment"
	OpGetSFlow              = "get_sflow"
	OpGetTopologyMap        = "get_topology_map"
	OpCompareDevices        = "compare_devices"
	OpGetRunningConfig      = "get_running_config"
	OpGetConfigSection      = "get_config_section"
)

// Protocol identifies a retrieval method.
type Protocol string

const (
	ProtocolGnmiOpenConfig    Protocol = "gnmi_openconfig"
	ProtocolGnmiNative        Protocol = "gnmi_native"
	ProtocolCLI               Protocol = "cli"
	ProtocolNetconfOpenConfig Protocol = "netconf_openconfig"
	ProtocolNetconfNative     Protocol = "netconf_native"
)

// ProtocolPath describes one way to retrieve data for an operation.
type ProtocolPath struct {
	Protocol Protocol
	// Paths holds gNMI paths (for gNMI protocols) or is empty for CLI.
	Paths []string
	// Command holds the CLI command template (for CLI protocol).
	Command string
	// Format declares the expected output encoding when using the CLI
	// protocol. Valid values: "text" (default), "json", "xml".
	// The declared format is used to select the appropriate parser from
	// the registry and to inform any user-defined jq transform.
	Format string
	// FormatArg is a vendor-specific argument appended to Command to
	// request the declared Format. Empty means the command is used as-is.
	// Examples: "| json", "--format json", "output-format json".
	FormatArg string
	// Filter is the NETCONF subtree filter XML body. Used for NETCONF protocols.
	// An empty string means no filter (retrieve the full datastore).
	Filter string
	// UseGetConfig, when true, issues a NETCONF <get-config> instead of <get>.
	// GetConfig retrieves only configuration data; Get retrieves config + state.
	// Defaults to false (uses <get>).
	UseGetConfig bool
	// Datastore is the NETCONF source datastore for GetConfig RPCs.
	// Valid values: "running" (default), "candidate", "startup".
	Datastore string
	// Operation metadata copied from the sidecar/profile contract. Execution
	// uses these fields for parameter-aware path rendering and safety limits.
	Parameters []OperationParameter
	Limits     *OperationLimits
}

// OperationDataKind describes what kind of data an operation retrieves.
type OperationDataKind string

const (
	OperationDataState       OperationDataKind = "state"
	OperationDataConfig      OperationDataKind = "config"
	OperationDataConfigState OperationDataKind = "config_state"
	OperationDataRPC         OperationDataKind = "rpc"
	OperationDataRaw         OperationDataKind = "raw"
)

// OperationParameter describes one runtime parameter accepted by a
// profile-routed operation.
type OperationParameter struct {
	Name                  string
	PathKey               string
	TargetPath            string
	TargetContainer       string
	GnmiPathTemplate      string
	NetconfFilterTemplate string
	Default               string
	Required              bool
	Allowed               []string
	Description           string
}

// OperationLimits captures operator-safety defaults for large operations.
type OperationLimits struct {
	DefaultCount int
	MaxCount     int
	MaxBytes     int
}

// DiagnosticCommandTemplate describes a vendor/platform-owned read-only
// command template for CLI/RPC diagnostic operations.
type DiagnosticCommandTemplate struct {
	OperationID string
	Command     string
	Optional    []DiagnosticCommandFragment
}

// DiagnosticCommandFragment appends Template only when Parameter is present.
type DiagnosticCommandFragment struct {
	Parameter string
	Template  string
}

// OutputFormat returns the effective output format, defaulting to "text"
// when the Format field is empty.
func (pp ProtocolPath) OutputFormat() string {
	if pp.Format == "" {
		return "text"
	}
	return pp.Format
}

// CanExecute returns true if the source capabilities satisfy this path.
func (pp ProtocolPath) CanExecute(caps capabilities.SourceCapabilities) bool {
	switch pp.Protocol {
	case ProtocolGnmiOpenConfig:
		return caps.GnmiSnapshot && caps.OpenConfigPaths
	case ProtocolGnmiNative:
		return caps.GnmiSnapshot && caps.NativeYang
	case ProtocolCLI:
		return caps.CLI
	case ProtocolNetconfOpenConfig:
		return caps.Netconf && caps.OpenConfigPaths
	case ProtocolNetconfNative:
		return caps.Netconf && caps.NativeYang
	default:
		return false
	}
}

// OperationDescriptor defines how to execute a specific read operation
// on a given device profile, with protocol paths in preference order.
type OperationDescriptor struct {
	OperationID string
	Data        OperationDataKind
	Datastore   string
	Parameters  []OperationParameter
	Limits      *OperationLimits
	Paths       []ProtocolPath // ordered by preference (best first)
}

// Profile represents a vendor+platform model profile containing
// the operations it supports and how to execute each one.
type Profile struct {
	Vendor             string
	Platform           string
	Version            string // optional; empty for unversioned (init()-registered) profiles
	Operations         map[string]OperationDescriptor
	DiagnosticCommands map[string]DiagnosticCommandTemplate
}

// profileKey creates the registry lookup key (version-blind).
func profileKey(vendor, platform string) string {
	return strings.ToLower(vendor) + "." + strings.ToLower(platform)
}

// versionedProfileKey creates a version-specific registry lookup key.
func versionedProfileKey(vendor, platform, version string) string {
	return strings.ToLower(vendor) + "." + strings.ToLower(platform) + "." + strings.ToLower(version)
}

var (
	mu       sync.RWMutex
	registry = map[string]*Profile{}
)

// Register adds a profile to the registry. Panics on duplicate.
func Register(profile *Profile) {
	key := profileKey(profile.Vendor, profile.Platform)
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[key]; exists {
		panic(fmt.Sprintf("profile %q already registered", key))
	}
	registry[key] = profile
}

// Lookup returns the profile for the given vendor and platform.
func Lookup(vendor, platform string) (*Profile, bool) {
	key := profileKey(vendor, platform)
	mu.RLock()
	defer mu.RUnlock()
	p, ok := registry[key]
	return p, ok
}

// RegisterOrReplace registers a profile, replacing any existing one.
// Used by schema-driven profile builder to override init()-registered
// hardcoded profiles when a schema is available.
func RegisterOrReplace(profile *Profile) {
	key := profileKey(profile.Vendor, profile.Platform)
	mu.Lock()
	defer mu.Unlock()
	registry[key] = profile
}

// AllProfiles returns a snapshot of all registered profiles.
func AllProfiles() map[string]*Profile {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]*Profile, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

// RegisterVersioned adds a version-specific profile to the registry.
// Panics on duplicate versioned key. The profile's Version field must be set.
func RegisterVersioned(profile *Profile) {
	if profile.Version == "" {
		panic("RegisterVersioned called with empty version")
	}
	key := versionedProfileKey(profile.Vendor, profile.Platform, profile.Version)
	mu.Lock()
	defer mu.Unlock()
	if _, exists := registry[key]; exists {
		panic(fmt.Sprintf("versioned profile %q already registered", key))
	}
	registry[key] = profile
}

// LookupForDevice returns the best profile for a device. When version is
// non-empty it tries a version-specific profile first, then falls back to
// the unversioned vendor.platform profile.
func LookupForDevice(vendor, platform, version string) (*Profile, bool) {
	mu.RLock()
	defer mu.RUnlock()
	if version != "" {
		if p, ok := registry[versionedProfileKey(vendor, platform, version)]; ok {
			return p, true
		}
	}
	p, ok := registry[profileKey(vendor, platform)]
	return p, ok
}
