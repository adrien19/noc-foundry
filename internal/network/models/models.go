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

// BGPNeighbor represents a single BGP peer adjacency.
// Field semantics follow openconfig-bgp:bgp/neighbors/neighbor/state.
type BGPNeighbor struct {
	NeighborAddress   string         `json:"neighbor_address"`
	PeerAS            uint32         `json:"peer_as"`
	LocalAS           uint32         `json:"local_as,omitempty"`
	SessionState      string         `json:"session_state"`       // ESTABLISHED, IDLE, ACTIVE, CONNECT, OPENSENT, OPENCONFIRM
	PeerType          string         `json:"peer_type,omitempty"` // INTERNAL, EXTERNAL
	Description       string         `json:"description,omitempty"`
	PrefixesReceived  uint32         `json:"prefixes_received,omitempty"`
	PrefixesSent      uint32         `json:"prefixes_sent,omitempty"`
	PrefixesInstalled uint32         `json:"prefixes_installed,omitempty"`
	Uptime            string         `json:"uptime,omitempty"`
	LastEstablished   string         `json:"last_established,omitempty"`
	MessagesReceived  uint64         `json:"messages_received,omitempty"`
	MessagesSent      uint64         `json:"messages_sent,omitempty"`
	VendorExtensions  map[string]any `json:"vendor_extensions,omitempty"`
}

// Route represents a single IP routing table entry.
// Field semantics follow openconfig-network-instance AFT entries.
type Route struct {
	Prefix           string         `json:"prefix"`
	NextHop          string         `json:"next_hop"`
	Protocol         string         `json:"protocol,omitempty"` // bgp, ospf, isis, static, connected, local
	Preference       uint32         `json:"preference,omitempty"`
	Metric           uint32         `json:"metric,omitempty"`
	Interface        string         `json:"interface,omitempty"`
	Active           bool           `json:"active"`
	NetworkInstance  string         `json:"network_instance,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

// Alarm represents an active alarm or event entry on a device.
// Field semantics follow openconfig-system:system/alarms/alarm/state.
type Alarm struct {
	ID               string         `json:"id"`
	Resource         string         `json:"resource,omitempty"`
	Severity         string         `json:"severity"` // CRITICAL, MAJOR, MINOR, WARNING
	Text             string         `json:"text"`
	TimeCreated      string         `json:"time_created,omitempty"`
	Type             string         `json:"type,omitempty"`
	State            string         `json:"state,omitempty"` // active, cleared
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

// TODO(ops-readiness): The models below register the complete inventory
// surface as canonical targets. Each operation still needs schema path
// validation, vendor alias coverage, high-volume limits, and verified CLI
// parsers before it reaches Ops-ready status.

type LLDPNeighbor struct {
	LocalInterface        string         `json:"local_interface"`
	RemoteSystemName      string         `json:"remote_system_name,omitempty"`
	RemotePortID          string         `json:"remote_port_id,omitempty"`
	RemotePortDescription string         `json:"remote_port_description,omitempty"`
	RemoteChassisID       string         `json:"remote_chassis_id,omitempty"`
	ManagementAddress     string         `json:"management_address,omitempty"`
	SystemCapabilities    []string       `json:"system_capabilities,omitempty"`
	VendorExtensions      map[string]any `json:"vendor_extensions,omitempty"`
}

type VLAN struct {
	ID               string         `json:"id"`
	Name             string         `json:"name,omitempty"`
	Status           string         `json:"status,omitempty"`
	Members          []string       `json:"members,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type LACPInterface struct {
	Name             string         `json:"name"`
	Interval         string         `json:"interval,omitempty"`
	Mode             string         `json:"mode,omitempty"`
	SystemID         string         `json:"system_id,omitempty"`
	PartnerID        string         `json:"partner_id,omitempty"`
	Members          []LACPMember   `json:"members,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type LACPMember struct {
	Port            string `json:"port,omitempty"`
	Activity        string `json:"activity,omitempty"`
	Timeout         string `json:"timeout,omitempty"`
	Aggregatable    bool   `json:"aggregatable,omitempty"`
	Synchronization bool   `json:"synchronization,omitempty"`
}

type MACEntry struct {
	Address          string         `json:"address"`
	VLAN             string         `json:"vlan,omitempty"`
	Interface        string         `json:"interface,omitempty"`
	EntryType        string         `json:"entry_type,omitempty"`
	Age              string         `json:"age,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type STPInstance struct {
	Protocol         string         `json:"protocol,omitempty"`
	RootBridgeID     string         `json:"root_bridge_id,omitempty"`
	RootCost         uint32         `json:"root_cost,omitempty"`
	LocalBridgeID    string         `json:"local_bridge_id,omitempty"`
	Interfaces       []STPInterface `json:"interfaces,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type STPInterface struct {
	Port  string `json:"port,omitempty"`
	Role  string `json:"role,omitempty"`
	State string `json:"state,omitempty"`
	Cost  uint32 `json:"cost,omitempty"`
}

type BGPRoute struct {
	Prefix           string         `json:"prefix"`
	PathID           string         `json:"path_id,omitempty"`
	NextHop          string         `json:"next_hop,omitempty"`
	LocalPref        uint32         `json:"local_pref,omitempty"`
	MED              uint32         `json:"med,omitempty"`
	ASPath           string         `json:"as_path,omitempty"`
	Origin           string         `json:"origin,omitempty"`
	Communities      []string       `json:"communities,omitempty"`
	BestPath         bool           `json:"best_path,omitempty"`
	ValidRoute       bool           `json:"valid_route,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type OSPFNeighbor struct {
	NeighborID          string         `json:"neighbor_id"`
	Address             string         `json:"address,omitempty"`
	State               string         `json:"state,omitempty"`
	Priority            uint32         `json:"priority,omitempty"`
	DR                  string         `json:"dr,omitempty"`
	BDR                 string         `json:"bdr,omitempty"`
	Interface           string         `json:"interface,omitempty"`
	Area                string         `json:"area,omitempty"`
	DeadTimer           string         `json:"dead_timer,omitempty"`
	Uptime              string         `json:"uptime,omitempty"`
	RetransmissionCount uint32         `json:"retransmission_count,omitempty"`
	VendorExtensions    map[string]any `json:"vendor_extensions,omitempty"`
}

type OSPFLSDBEntry struct {
	LSAType           string         `json:"lsa_type"`
	LSID              string         `json:"ls_id,omitempty"`
	AdvertisingRouter string         `json:"advertising_router,omitempty"`
	SequenceNumber    string         `json:"sequence_number,omitempty"`
	Age               string         `json:"age,omitempty"`
	Checksum          string         `json:"checksum,omitempty"`
	Area              string         `json:"area,omitempty"`
	VendorExtensions  map[string]any `json:"vendor_extensions,omitempty"`
}

type ISISAdjacency struct {
	SystemID         string         `json:"system_id"`
	NeighborSNPA     string         `json:"neighbor_snpa,omitempty"`
	State            string         `json:"state,omitempty"`
	Level            string         `json:"level,omitempty"`
	Interface        string         `json:"interface,omitempty"`
	HoldTimer        string         `json:"hold_timer,omitempty"`
	Priority         uint32         `json:"priority,omitempty"`
	CircuitType      string         `json:"circuit_type,omitempty"`
	AreaAddress      string         `json:"area_address,omitempty"`
	Uptime           string         `json:"uptime,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type StaticRoute struct {
	Prefix           string         `json:"prefix"`
	NextHop          string         `json:"next_hop,omitempty"`
	Metric           uint32         `json:"metric,omitempty"`
	Preference       uint32         `json:"preference,omitempty"`
	Tag              string         `json:"tag,omitempty"`
	NetworkInstance  string         `json:"network_instance,omitempty"`
	Active           bool           `json:"active,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type MPLSPath struct {
	Name               string         `json:"name"`
	Type               string         `json:"type,omitempty"`
	SourceAddress      string         `json:"source_address,omitempty"`
	DestinationAddress string         `json:"destination_address,omitempty"`
	State              string         `json:"state,omitempty"`
	InLabel            string         `json:"in_label,omitempty"`
	OutLabel           string         `json:"out_label,omitempty"`
	OutInterface       string         `json:"out_interface,omitempty"`
	Metric             uint32         `json:"metric,omitempty"`
	VendorExtensions   map[string]any `json:"vendor_extensions,omitempty"`
}

type SegmentRoutingEntry struct {
	Prefix           string         `json:"prefix,omitempty"`
	SID              string         `json:"sid,omitempty"`
	Algorithm        string         `json:"algorithm,omitempty"`
	Flags            []string       `json:"flags,omitempty"`
	Interface        string         `json:"interface,omitempty"`
	AdjacencySID     string         `json:"adjacency_sid,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type MulticastEntry struct {
	Group              string         `json:"group"`
	Source             string         `json:"source,omitempty"`
	IncomingInterface  string         `json:"incoming_interface,omitempty"`
	OutgoingInterfaces []string       `json:"outgoing_interfaces,omitempty"`
	Protocol           string         `json:"protocol,omitempty"`
	Uptime             string         `json:"uptime,omitempty"`
	State              string         `json:"state,omitempty"`
	VendorExtensions   map[string]any `json:"vendor_extensions,omitempty"`
}

type BFDSession struct {
	Interface           string         `json:"interface,omitempty"`
	RemoteAddress       string         `json:"remote_address,omitempty"`
	State               string         `json:"state,omitempty"`
	Interval            string         `json:"interval,omitempty"`
	Multiplier          uint32         `json:"multiplier,omitempty"`
	LocalDiscriminator  string         `json:"local_discriminator,omitempty"`
	RemoteDiscriminator string         `json:"remote_discriminator,omitempty"`
	Uptime              string         `json:"uptime,omitempty"`
	AssociatedProtocols []string       `json:"associated_protocols,omitempty"`
	VendorExtensions    map[string]any `json:"vendor_extensions,omitempty"`
}

type NetworkInstance struct {
	Name               string         `json:"name"`
	Type               string         `json:"type,omitempty"`
	RouterID           string         `json:"router_id,omitempty"`
	RouteDistinguisher string         `json:"route_distinguisher,omitempty"`
	Interfaces         []string       `json:"interfaces,omitempty"`
	Protocols          []string       `json:"protocols,omitempty"`
	Enabled            bool           `json:"enabled,omitempty"`
	VendorExtensions   map[string]any `json:"vendor_extensions,omitempty"`
}

type CPUUtilization struct {
	Index            string         `json:"index,omitempty"`
	Total            float64        `json:"total,omitempty"`
	User             float64        `json:"user,omitempty"`
	System           float64        `json:"system,omitempty"`
	Idle             float64        `json:"idle,omitempty"`
	Softirq          float64        `json:"softirq,omitempty"`
	Nice             float64        `json:"nice,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type MemoryUtilization struct {
	Physical         uint64         `json:"physical,omitempty"`
	Used             uint64         `json:"used,omitempty"`
	Free             uint64         `json:"free,omitempty"`
	Utilized         float64        `json:"utilized,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type PlatformComponent struct {
	Name             string         `json:"name"`
	Type             string         `json:"type,omitempty"`
	PartNumber       string         `json:"part_number,omitempty"`
	SerialNumber     string         `json:"serial_number,omitempty"`
	HardwareVersion  string         `json:"hardware_version,omitempty"`
	FirmwareVersion  string         `json:"firmware_version,omitempty"`
	OperStatus       string         `json:"oper_status,omitempty"`
	Temperature      float64        `json:"temperature,omitempty"`
	Description      string         `json:"description,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type TransceiverState struct {
	Interface        string         `json:"interface,omitempty"`
	FormFactor       string         `json:"form_factor,omitempty"`
	VendorName       string         `json:"vendor_name,omitempty"`
	VendorPartNumber string         `json:"vendor_part_number,omitempty"`
	SerialNumber     string         `json:"serial_number,omitempty"`
	ModuleType       string         `json:"module_type,omitempty"`
	TxPowerDBm       float64        `json:"tx_power_dbm,omitempty"`
	RxPowerDBm       float64        `json:"rx_power_dbm,omitempty"`
	LaserBias        float64        `json:"laser_bias,omitempty"`
	Temperature      float64        `json:"temperature,omitempty"`
	Voltage          float64        `json:"voltage,omitempty"`
	ThresholdAlarms  []string       `json:"threshold_alarms,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type NTPStatus struct {
	Enabled          bool           `json:"enabled,omitempty"`
	AuthEnabled      bool           `json:"auth_enabled,omitempty"`
	Servers          []NTPServer    `json:"servers,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type NTPServer struct {
	Address   string  `json:"address,omitempty"`
	Stratum   uint32  `json:"stratum,omitempty"`
	Reachable bool    `json:"reachable,omitempty"`
	Offset    float64 `json:"offset,omitempty"`
	Delay     float64 `json:"delay,omitempty"`
	Jitter    float64 `json:"jitter,omitempty"`
	Preferred bool    `json:"preferred,omitempty"`
}

type DNSConfig struct {
	Servers          []DNSServer    `json:"servers,omitempty"`
	SearchDomains    []string       `json:"search_domains,omitempty"`
	HostEntries      []DNSHostEntry `json:"host_entries,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type DNSServer struct {
	Address string `json:"address,omitempty"`
	Port    uint32 `json:"port,omitempty"`
}

type DNSHostEntry struct {
	Hostname  string   `json:"hostname,omitempty"`
	Addresses []string `json:"addresses,omitempty"`
}

type AAAConfig struct {
	AuthenticationMethods []string         `json:"authentication_methods,omitempty"`
	AuthorizationMethods  []string         `json:"authorization_methods,omitempty"`
	AccountingMethods     []string         `json:"accounting_methods,omitempty"`
	ServerGroups          []AAAServerGroup `json:"server_groups,omitempty"`
	VendorExtensions      map[string]any   `json:"vendor_extensions,omitempty"`
}

type AAAServerGroup struct {
	Name    string      `json:"name,omitempty"`
	Type    string      `json:"type,omitempty"`
	Servers []AAAServer `json:"servers,omitempty"`
}

type AAAServer struct {
	Address string `json:"address,omitempty"`
	Port    uint32 `json:"port,omitempty"`
}

type LoggingConfig struct {
	Servers          []LoggingServer `json:"servers,omitempty"`
	Console          string          `json:"console,omitempty"`
	Buffer           string          `json:"buffer,omitempty"`
	VendorExtensions map[string]any  `json:"vendor_extensions,omitempty"`
}

type LoggingServer struct {
	Address  string `json:"address,omitempty"`
	Port     uint32 `json:"port,omitempty"`
	Protocol string `json:"protocol,omitempty"`
	Facility string `json:"facility,omitempty"`
	Severity string `json:"severity,omitempty"`
}

type Process struct {
	PID              string         `json:"pid,omitempty"`
	Name             string         `json:"name"`
	State            string         `json:"state,omitempty"`
	CPUUsage         float64        `json:"cpu_usage,omitempty"`
	MemoryUsage      float64        `json:"memory_usage,omitempty"`
	Uptime           string         `json:"uptime,omitempty"`
	StartTime        string         `json:"start_time,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type ACL struct {
	Name             string         `json:"name"`
	Type             string         `json:"type,omitempty"`
	Entries          []ACLEntry     `json:"entries,omitempty"`
	Interfaces       []ACLInterface `json:"interfaces,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type ACLEntry struct {
	Sequence       string `json:"sequence,omitempty"`
	Action         string `json:"action,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	SrcPrefix      string `json:"src_prefix,omitempty"`
	DstPrefix      string `json:"dst_prefix,omitempty"`
	SrcPort        string `json:"src_port,omitempty"`
	DstPort        string `json:"dst_port,omitempty"`
	MatchedPackets uint64 `json:"matched_packets,omitempty"`
	MatchedOctets  uint64 `json:"matched_octets,omitempty"`
}

type ACLInterface struct {
	Name      string `json:"name,omitempty"`
	Direction string `json:"direction,omitempty"`
}

type PolicyForwardingRule struct {
	Policy           string         `json:"policy,omitempty"`
	Sequence         string         `json:"sequence,omitempty"`
	MatchCriteria    string         `json:"match_criteria,omitempty"`
	Action           string         `json:"action,omitempty"`
	NextHop          string         `json:"next_hop,omitempty"`
	NetworkInstance  string         `json:"network_instance,omitempty"`
	MatchedPackets   uint64         `json:"matched_packets,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type QoSInterface struct {
	Interface        string         `json:"interface"`
	Queues           []QoSQueue     `json:"queues,omitempty"`
	Schedulers       []QoSScheduler `json:"schedulers,omitempty"`
	ClassifierPolicy string         `json:"classifier_policy,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type QoSQueue struct {
	Name            string `json:"name,omitempty"`
	TransmitPackets uint64 `json:"transmit_packets,omitempty"`
	TransmitOctets  uint64 `json:"transmit_octets,omitempty"`
	DroppedPackets  uint64 `json:"dropped_packets,omitempty"`
	DroppedOctets   uint64 `json:"dropped_octets,omitempty"`
}

type QoSScheduler struct {
	Sequence string `json:"sequence,omitempty"`
	Type     string `json:"type,omitempty"`
	Priority string `json:"priority,omitempty"`
	Weight   uint32 `json:"weight,omitempty"`
}

type QoSClassifier struct {
	Name             string              `json:"name"`
	Type             string              `json:"type,omitempty"`
	Terms            []QoSClassifierTerm `json:"terms,omitempty"`
	VendorExtensions map[string]any      `json:"vendor_extensions,omitempty"`
}

type QoSClassifierTerm struct {
	ID              string `json:"id,omitempty"`
	MatchConditions string `json:"match_conditions,omitempty"`
	Action          string `json:"action,omitempty"`
}

type RoutingPolicy struct {
	Name             string                   `json:"name"`
	Statements       []RoutingPolicyStatement `json:"statements,omitempty"`
	DefinedSets      RoutingPolicyDefinedSets `json:"defined_sets,omitempty"`
	VendorExtensions map[string]any           `json:"vendor_extensions,omitempty"`
}

type RoutingPolicyStatement struct {
	Name       string `json:"name,omitempty"`
	Conditions string `json:"conditions,omitempty"`
	Actions    string `json:"actions,omitempty"`
}

type RoutingPolicyDefinedSets struct {
	PrefixSets    []PrefixSet    `json:"prefix_sets,omitempty"`
	CommunitySets []CommunitySet `json:"community_sets,omitempty"`
	ASPathSets    []string       `json:"as_path_sets,omitempty"`
}

type PrefixSet struct {
	Name             string           `json:"name"`
	Mode             string           `json:"mode,omitempty"`
	Prefixes         []PrefixSetEntry `json:"prefixes,omitempty"`
	VendorExtensions map[string]any   `json:"vendor_extensions,omitempty"`
}

type PrefixSetEntry struct {
	Prefix          string `json:"prefix,omitempty"`
	MaskLengthRange string `json:"mask_length_range,omitempty"`
}

type CommunitySet struct {
	Name             string         `json:"name"`
	Members          []string       `json:"members,omitempty"`
	MatchSetOptions  string         `json:"match_set_options,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type EVPNInstance struct {
	EVI                string         `json:"evi,omitempty"`
	Type               string         `json:"type,omitempty"`
	RouteDistinguisher string         `json:"route_distinguisher,omitempty"`
	RouteTargets       []string       `json:"route_targets,omitempty"`
	VNI                string         `json:"vni,omitempty"`
	State              string         `json:"state,omitempty"`
	VendorExtensions   map[string]any `json:"vendor_extensions,omitempty"`
}

type VXLANTunnel struct {
	VNI                    string         `json:"vni,omitempty"`
	SourceVTEP             string         `json:"source_vtep,omitempty"`
	RemoteVTEPs            []string       `json:"remote_vteps,omitempty"`
	State                  string         `json:"state,omitempty"`
	Type                   string         `json:"type,omitempty"`
	AssociatedBridgeDomain string         `json:"associated_bridge_domain,omitempty"`
	VendorExtensions       map[string]any `json:"vendor_extensions,omitempty"`
}

type EthernetSegment struct {
	ESI              string         `json:"esi"`
	Type             string         `json:"type,omitempty"`
	Interface        string         `json:"interface,omitempty"`
	DFElection       string         `json:"df_election,omitempty"`
	ActiveMode       string         `json:"active_mode,omitempty"`
	State            string         `json:"state,omitempty"`
	Peers            []string       `json:"peers,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type PingResult struct {
	Target          string  `json:"target"`
	Source          string  `json:"source,omitempty"`
	PacketsSent     uint32  `json:"packets_sent,omitempty"`
	PacketsReceived uint32  `json:"packets_received,omitempty"`
	PacketLoss      float64 `json:"packet_loss,omitempty"`
	RTTMin          float64 `json:"rtt_min,omitempty"`
	RTTAvg          float64 `json:"rtt_avg,omitempty"`
	RTTMax          float64 `json:"rtt_max,omitempty"`
	RTTStdDev       float64 `json:"rtt_stddev,omitempty"`
}

type TracerouteResult struct {
	Target string          `json:"target"`
	Source string          `json:"source,omitempty"`
	Hops   []TracerouteHop `json:"hops,omitempty"`
}

type TracerouteHop struct {
	HopNumber uint32  `json:"hop_number,omitempty"`
	Address   string  `json:"address,omitempty"`
	Hostname  string  `json:"hostname,omitempty"`
	RTT1      float64 `json:"rtt1,omitempty"`
	RTT2      float64 `json:"rtt2,omitempty"`
	RTT3      float64 `json:"rtt3,omitempty"`
	MPLSLabel string  `json:"mpls_label,omitempty"`
}

type LogEntry struct {
	Timestamp        string         `json:"timestamp,omitempty"`
	Severity         string         `json:"severity,omitempty"`
	Facility         string         `json:"facility,omitempty"`
	Application      string         `json:"application,omitempty"`
	Message          string         `json:"message,omitempty"`
	EventID          string         `json:"event_id,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type ConfigDiff struct {
	Source      string           `json:"source,omitempty"`
	Target      string           `json:"target,omitempty"`
	Differences []ConfigDiffItem `json:"differences,omitempty"`
}

type ConfigDiffItem struct {
	Path      string `json:"path,omitempty"`
	OldValue  string `json:"old_value,omitempty"`
	NewValue  string `json:"new_value,omitempty"`
	Operation string `json:"operation,omitempty"`
}

type EnvironmentSensor struct {
	Name             string         `json:"name"`
	Type             string         `json:"type,omitempty"`
	Value            float64        `json:"value,omitempty"`
	Unit             string         `json:"unit,omitempty"`
	AlarmStatus      string         `json:"alarm_status,omitempty"`
	AlarmSeverity    string         `json:"alarm_severity,omitempty"`
	HighThreshold    float64        `json:"high_threshold,omitempty"`
	LowThreshold     float64        `json:"low_threshold,omitempty"`
	VendorExtensions map[string]any `json:"vendor_extensions,omitempty"`
}

type SFlowConfig struct {
	Enabled          bool             `json:"enabled,omitempty"`
	SamplingRate     uint64           `json:"sampling_rate,omitempty"`
	Collectors       []SFlowCollector `json:"collectors,omitempty"`
	Interfaces       []SFlowInterface `json:"interfaces,omitempty"`
	VendorExtensions map[string]any   `json:"vendor_extensions,omitempty"`
}

type SFlowCollector struct {
	Address string `json:"address,omitempty"`
	Port    uint32 `json:"port,omitempty"`
}

type SFlowInterface struct {
	Name    string `json:"name,omitempty"`
	Ingress bool   `json:"ingress,omitempty"`
	Egress  bool   `json:"egress,omitempty"`
}

type TopologyMap struct {
	Nodes []TopologyNode `json:"nodes,omitempty"`
	Links []TopologyLink `json:"links,omitempty"`
}

type TopologyNode struct {
	Device string            `json:"device,omitempty"`
	Role   string            `json:"role,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

type TopologyLink struct {
	LocalDevice     string   `json:"local_device,omitempty"`
	LocalInterface  string   `json:"local_interface,omitempty"`
	RemoteDevice    string   `json:"remote_device,omitempty"`
	RemoteInterface string   `json:"remote_interface,omitempty"`
	Speed           string   `json:"speed,omitempty"`
	State           string   `json:"state,omitempty"`
	Confidence      string   `json:"confidence,omitempty"`
	Evidence        []string `json:"evidence,omitempty"`
}

// TODO(ops-readiness): When network-show-topology is implemented, populate
// TopologyLink.Confidence and Evidence from bidirectional LLDP, single-sided
// LLDP, and inventory matching. Explicitly handle hostname/source-ID mismatch
// and unidirectional LLDP evidence instead of reporting those links as certain.

type ComparisonResult struct {
	Operation   string             `json:"operation,omitempty"`
	Devices     []ComparisonDevice `json:"devices,omitempty"`
	Differences []ComparisonDiff   `json:"differences,omitempty"`
}

type ComparisonDevice struct {
	Name  string `json:"name,omitempty"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

type ComparisonDiff struct {
	Field  string         `json:"field,omitempty"`
	Values map[string]any `json:"values,omitempty"`
}

type ConfigSection struct {
	Path             string         `json:"path,omitempty"`
	Section          string         `json:"section,omitempty"`
	Data             any            `json:"data,omitempty"`
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
