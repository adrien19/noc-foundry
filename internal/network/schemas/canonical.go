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
	"reflect"
	"strings"

	"github.com/adrien19/noc-foundry/internal/network/models"
)

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

	// ChildMappings describe nested child collections/singletons populated
	// from sub-containers under the parent object.
	ChildMappings []ChildMapping
}

// ChildMapping maps a nested source container into a canonical child field.
type ChildMapping struct {
	SourceContainer string
	CanonicalField  string
	ModelType       string
	List            bool
	Fields          []FieldMapping
}

// ---------------------------------------------------------------------------
// Canonical map registry
// ---------------------------------------------------------------------------

var canonicalMaps = map[string]*OperationCanonicalMap{}

// CanonicalModelDescriptor describes the model shape for a registered
// operation. The schema mapper uses it to build typed payloads without
// operation-specific switch statements.
type CanonicalModelDescriptor struct {
	OperationID    string
	ModelType      string
	Type           reflect.Type
	List           bool
	RequiredFields []string
}

var canonicalModels = map[string]CanonicalModelDescriptor{}

// RegisterCanonicalModel registers the payload type expected for an operation.
func RegisterCanonicalModel(operationID string, sample any, list bool, requiredFields ...string) {
	t := reflect.TypeOf(sample)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	canonicalModels[operationID] = CanonicalModelDescriptor{
		OperationID:    operationID,
		ModelType:      t.Name(),
		Type:           t,
		List:           list,
		RequiredFields: requiredFields,
	}
}

// LookupCanonicalModel returns the registered model descriptor for an operation.
func LookupCanonicalModel(operationID string) (CanonicalModelDescriptor, bool) {
	d, ok := canonicalModels[operationID]
	return d, ok
}

// RegisterCanonicalMap registers a canonical mapping for an operation.
func RegisterCanonicalMap(m *OperationCanonicalMap) {
	canonicalMaps[m.OperationID] = m
}

// LookupCanonicalMap returns the canonical mapping for the given operation.
func LookupCanonicalMap(operationID string) (*OperationCanonicalMap, bool) {
	m, ok := canonicalMaps[operationID]
	return m, ok
}

// ExtendCanonicalMap appends additional field mappings to an existing
// canonical map. Duplicate YANGLeaf entries are silently skipped to
// ensure idempotency. If the operationID is not yet registered, the
// call is a no-op (the sidecar may reference an operation that hasn't
// been registered yet; this is not an error).
func ExtendCanonicalMap(operationID string, fields []FieldMapping) {
	m, ok := canonicalMaps[operationID]
	if !ok {
		return
	}
	existing := make(map[string]bool, len(m.Fields))
	for _, f := range m.Fields {
		existing[f.YANGLeaf] = true
	}
	for _, f := range fields {
		if existing[f.YANGLeaf] {
			continue
		}
		m.Fields = append(m.Fields, f)
		existing[f.YANGLeaf] = true
	}
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

func registerBuiltInCanonicalModels() {
	RegisterCanonicalModel("get_interfaces", models.InterfaceState{}, true, "Name")
	RegisterCanonicalModel("get_interface_counters", models.InterfaceCounters{}, true)
	RegisterCanonicalModel("get_lldp_neighbors", models.LLDPNeighbor{}, true, "LocalInterface")
	RegisterCanonicalModel("get_vlans", models.VLAN{}, true, "ID")
	RegisterCanonicalModel("get_lacp", models.LACPInterface{}, true, "Name")
	RegisterCanonicalModel("get_mac_table", models.MACEntry{}, true, "Address")
	RegisterCanonicalModel("get_stp", models.STPInstance{}, true)
	RegisterCanonicalModel("get_bgp_neighbors", models.BGPNeighbor{}, true, "NeighborAddress")
	RegisterCanonicalModel("get_bgp_rib", models.BGPRoute{}, true, "Prefix")
	RegisterCanonicalModel("get_ospf_neighbors", models.OSPFNeighbor{}, true, "NeighborID")
	RegisterCanonicalModel("get_ospf_database", models.OSPFLSDBEntry{}, true, "LSAType")
	RegisterCanonicalModel("get_isis_adjacencies", models.ISISAdjacency{}, true, "SystemID")
	RegisterCanonicalModel("get_route_table", models.Route{}, true, "Prefix")
	RegisterCanonicalModel("get_static_routes", models.StaticRoute{}, true, "Prefix")
	RegisterCanonicalModel("get_mpls_lsps", models.MPLSPath{}, true, "Name")
	RegisterCanonicalModel("get_segment_routing", models.SegmentRoutingEntry{}, true)
	RegisterCanonicalModel("get_multicast", models.MulticastEntry{}, true, "Group")
	RegisterCanonicalModel("get_bfd_sessions", models.BFDSession{}, true)
	RegisterCanonicalModel("get_network_instances", models.NetworkInstance{}, true, "Name")
	RegisterCanonicalModel("get_system_version", models.SystemVersion{}, false)
	RegisterCanonicalModel("get_system_alarms", models.Alarm{}, true, "ID")
	RegisterCanonicalModel("get_system_cpu", models.CPUUtilization{}, true)
	RegisterCanonicalModel("get_system_memory", models.MemoryUtilization{}, false)
	RegisterCanonicalModel("get_platform_components", models.PlatformComponent{}, true, "Name")
	RegisterCanonicalModel("get_transceiver_state", models.TransceiverState{}, true)
	RegisterCanonicalModel("get_system_ntp", models.NTPStatus{}, false)
	RegisterCanonicalModel("get_system_dns", models.DNSConfig{}, false)
	RegisterCanonicalModel("get_system_aaa", models.AAAConfig{}, false)
	RegisterCanonicalModel("get_system_logging", models.LoggingConfig{}, false)
	RegisterCanonicalModel("get_system_processes", models.Process{}, true, "Name")
	RegisterCanonicalModel("get_acl", models.ACL{}, true, "Name")
	RegisterCanonicalModel("get_acl_counters", models.ACL{}, true, "Name")
	RegisterCanonicalModel("get_policy_forwarding", models.PolicyForwardingRule{}, true)
	RegisterCanonicalModel("get_qos_interfaces", models.QoSInterface{}, true, "Interface")
	RegisterCanonicalModel("get_qos_classifiers", models.QoSClassifier{}, true, "Name")
	RegisterCanonicalModel("get_routing_policy", models.RoutingPolicy{}, true, "Name")
	RegisterCanonicalModel("get_prefix_sets", models.PrefixSet{}, true, "Name")
	RegisterCanonicalModel("get_community_sets", models.CommunitySet{}, true, "Name")
	RegisterCanonicalModel("get_evpn_instances", models.EVPNInstance{}, true)
	RegisterCanonicalModel("get_vxlan_tunnels", models.VXLANTunnel{}, true)
	RegisterCanonicalModel("get_ethernet_segments", models.EthernetSegment{}, true, "ESI")
	RegisterCanonicalModel("get_log_entries", models.LogEntry{}, true)
	RegisterCanonicalModel("get_environment", models.EnvironmentSensor{}, true, "Name")
	RegisterCanonicalModel("get_sflow", models.SFlowConfig{}, false)
	RegisterCanonicalModel("get_running_config", models.ConfigSection{}, false)
	RegisterCanonicalModel("get_config_section", models.ConfigSection{}, false)
}

func registerMinimalCanonicalMaps() {
	for opID, d := range canonicalModels {
		if _, ok := LookupCanonicalMap(opID); ok {
			continue
		}
		RegisterCanonicalMap(&OperationCanonicalMap{
			OperationID: opID,
			ModelType:   d.ModelType,
			Fields:      defaultFieldMappings(d.Type),
			ContainerAliases: []ContainerAlias{
				{Name: "state", MergeUp: true},
				{Name: "config", MergeUp: true},
				{Name: "counters", MergeUp: true},
			},
		})
	}
}

func registerBuiltInChildMappings() {
	addChildMappings("get_acl",
		ChildMapping{SourceContainer: "entries/entry", CanonicalField: "Entries", ModelType: "ACLEntry", List: true, Fields: []FieldMapping{
			{YANGLeaf: "sequence", CanonicalField: "Sequence"},
			{YANGLeaf: "action", CanonicalField: "Action"},
			{YANGLeaf: "protocol", CanonicalField: "Protocol"},
			{YANGLeaf: "matched-packets", CanonicalField: "MatchedPackets"},
			{YANGLeaf: "matched-octets", CanonicalField: "MatchedOctets"},
		}},
		ChildMapping{SourceContainer: "acl-entries/acl-entry", CanonicalField: "Entries", ModelType: "ACLEntry", List: true, Fields: []FieldMapping{
			{YANGLeaf: "sequence-id", CanonicalField: "Sequence"},
			{YANGLeaf: "forwarding-action", CanonicalField: "Action"},
			{YANGLeaf: "protocol", CanonicalField: "Protocol"},
			{YANGLeaf: "matched-packets", CanonicalField: "MatchedPackets"},
			{YANGLeaf: "matched-octets", CanonicalField: "MatchedOctets"},
		}},
	)
	addChildMappings("get_qos_interfaces",
		ChildMapping{SourceContainer: "queues/queue", CanonicalField: "Queues", ModelType: "QoSQueue", List: true, Fields: []FieldMapping{
			{YANGLeaf: "name", CanonicalField: "Name"},
			{YANGLeaf: "transmit-packets", CanonicalField: "TransmitPackets"},
			{YANGLeaf: "transmit-octets", CanonicalField: "TransmitOctets"},
			{YANGLeaf: "dropped-packets", CanonicalField: "DroppedPackets"},
			{YANGLeaf: "dropped-octets", CanonicalField: "DroppedOctets"},
		}},
		ChildMapping{SourceContainer: "schedulers/scheduler", CanonicalField: "Schedulers", ModelType: "QoSScheduler", List: true, Fields: []FieldMapping{
			{YANGLeaf: "sequence", CanonicalField: "Sequence"},
			{YANGLeaf: "type", CanonicalField: "Type"},
			{YANGLeaf: "priority", CanonicalField: "Priority"},
			{YANGLeaf: "weight", CanonicalField: "Weight"},
		}},
	)
	addChildMappings("get_routing_policy",
		ChildMapping{SourceContainer: "statements/statement", CanonicalField: "Statements", ModelType: "RoutingPolicyStatement", List: true, Fields: []FieldMapping{
			{YANGLeaf: "name", CanonicalField: "Name"},
			{YANGLeaf: "conditions", CanonicalField: "Conditions"},
			{YANGLeaf: "actions", CanonicalField: "Actions"},
		}},
	)
	addChildMappings("get_lacp",
		ChildMapping{SourceContainer: "members/member", CanonicalField: "Members", ModelType: "LACPMember", List: true, Fields: []FieldMapping{
			{YANGLeaf: "port", CanonicalField: "Port"},
			{YANGLeaf: "activity", CanonicalField: "Activity"},
			{YANGLeaf: "timeout", CanonicalField: "Timeout"},
			{YANGLeaf: "aggregatable", CanonicalField: "Aggregatable"},
			{YANGLeaf: "synchronization", CanonicalField: "Synchronization"},
		}},
	)
}

func addChildMappings(operationID string, children ...ChildMapping) {
	m, ok := LookupCanonicalMap(operationID)
	if !ok {
		return
	}
	m.ChildMappings = append(m.ChildMappings, children...)
}

func defaultFieldMappings(t reflect.Type) []FieldMapping {
	fields := make([]FieldMapping, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if sf.Name == "VendorExtensions" {
			continue
		}
		jsonName := strings.Split(sf.Tag.Get("json"), ",")[0]
		if jsonName == "" || jsonName == "-" {
			continue
		}
		fields = append(fields,
			FieldMapping{YANGLeaf: jsonName, CanonicalField: sf.Name},
			FieldMapping{YANGLeaf: strings.ReplaceAll(jsonName, "_", "-"), CanonicalField: sf.Name},
			FieldMapping{YANGLeaf: strings.ToLower(sf.Name), CanonicalField: sf.Name},
		)
	}
	return fields
}

// ---------------------------------------------------------------------------
// Built-in normalizers — cover OpenConfig, Nokia SRL, Nokia SROS enum values.
// ---------------------------------------------------------------------------

func init() {
	registerBuiltInCanonicalModels()
	registerMinimalCanonicalMaps()
	registerBuiltInChildMappings()

	// Unified admin-status normalizer covering all known vendor values.
	RegisterStatusNormalizer("interface_name", func(value string) string {
		// TODO(normalizers): Add vendor-aware canonical interface name handling
		// for aliases such as ethernet-1/1 vs 1/1/1 and breakout channel forms.
		return strings.TrimSpace(value)
	})

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

	RegisterStatusNormalizer("route_protocol", func(value string) string {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "bgp", "openconfig-policy-types:bgp", "bgp-route":
			return "bgp"
		case "ospf", "ospfv2", "openconfig-policy-types:ospf":
			return "ospf"
		case "isis", "is-is":
			return "isis"
		case "static":
			return "static"
		case "direct", "connected":
			return "connected"
		case "local":
			return "local"
		default:
			// TODO(normalizers): Expand route protocol normalization with real
			// SR Linux/SR OS/OpenConfig enum samples for all supported RIBs.
			return strings.ToLower(strings.TrimSpace(value))
		}
	})

	RegisterStatusNormalizer("timestamp", func(value string) string {
		// TODO(normalizers): Parse common vendor timestamp layouts and emit
		// RFC3339 when timezone context is available. Returning trimmed input
		// keeps provenance until clock/timezone semantics are reliable.
		return strings.TrimSpace(value)
	})

	RegisterStatusNormalizer("duration", func(value string) string {
		// TODO(normalizers): Normalize vendor uptimes/durations to ISO-8601
		// durations after parser samples cover SR Linux, SR OS, and OC forms.
		return strings.TrimSpace(value)
	})

	RegisterStatusNormalizer("optical_power_dbm", func(value string) string {
		// TODO(normalizers): Convert mW, micro-dBm, and vendor display units to
		// canonical dBm once transceiver telemetry samples are available.
		return strings.TrimSpace(value)
	})

	RegisterStatusNormalizer("percentage", func(value string) string {
		return strings.TrimSuffix(strings.TrimSpace(value), "%")
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
		ChildMappings: []ChildMapping{
			{
				SourceContainer: "state/counters",
				CanonicalField:  "Counters",
				ModelType:       "InterfaceCounters",
				Fields: []FieldMapping{
					{YANGLeaf: "in-octets", CanonicalField: "InOctets"},
					{YANGLeaf: "out-octets", CanonicalField: "OutOctets"},
					{YANGLeaf: "in-pkts", CanonicalField: "InPackets"},
					{YANGLeaf: "out-pkts", CanonicalField: "OutPackets"},
					{YANGLeaf: "in-errors", CanonicalField: "InErrors"},
					{YANGLeaf: "out-errors", CanonicalField: "OutErrors"},
					{YANGLeaf: "in-discards", CanonicalField: "InDiscards"},
					{YANGLeaf: "out-discards", CanonicalField: "OutDiscards"},
				},
			},
			{
				SourceContainer: "counters",
				CanonicalField:  "Counters",
				ModelType:       "InterfaceCounters",
				Fields: []FieldMapping{
					{YANGLeaf: "in-octets", CanonicalField: "InOctets"},
					{YANGLeaf: "out-octets", CanonicalField: "OutOctets"},
					{YANGLeaf: "in-packets", CanonicalField: "InPackets"},
					{YANGLeaf: "out-packets", CanonicalField: "OutPackets"},
					{YANGLeaf: "in-errors", CanonicalField: "InErrors"},
					{YANGLeaf: "out-errors", CanonicalField: "OutErrors"},
					{YANGLeaf: "in-discards", CanonicalField: "InDiscards"},
					{YANGLeaf: "out-discards", CanonicalField: "OutDiscards"},
				},
			},
		},
	})

	RegisterCanonicalMap(&OperationCanonicalMap{
		OperationID: "get_lldp_neighbors",
		ModelType:   "LLDPNeighbor",
		Fields: []FieldMapping{
			{YANGLeaf: "local-interface", CanonicalField: "LocalInterface"},
			{YANGLeaf: "local-port", CanonicalField: "LocalInterface"},
			{YANGLeaf: "remote-system-name", CanonicalField: "RemoteSystemName"},
			{YANGLeaf: "system-name", CanonicalField: "RemoteSystemName"},
			{YANGLeaf: "port-id", CanonicalField: "RemotePortID"},
			{YANGLeaf: "remote-port-id", CanonicalField: "RemotePortID"},
			{YANGLeaf: "port-description", CanonicalField: "RemotePortDescription"},
			{YANGLeaf: "remote-port-description", CanonicalField: "RemotePortDescription"},
			{YANGLeaf: "chassis-id", CanonicalField: "RemoteChassisID"},
			{YANGLeaf: "remote-chassis-id", CanonicalField: "RemoteChassisID"},
			{YANGLeaf: "management-address", CanonicalField: "ManagementAddress"},
			{YANGLeaf: "system-capabilities", CanonicalField: "SystemCapabilities"},
		},
		ContainerAliases: []ContainerAlias{
			{Name: "state", MergeUp: true},
			{Name: "config", MergeUp: true},
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

	// Unified BGP session-state normalizer.
	RegisterStatusNormalizer("bgp_session_state", func(value string) string {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "ESTABLISHED", "established":
			return "ESTABLISHED"
		case "IDLE", "idle":
			return "IDLE"
		case "ACTIVE", "active":
			return "ACTIVE"
		case "CONNECT", "connect":
			return "CONNECT"
		case "OPENSENT", "open-sent", "openSent":
			return "OPENSENT"
		case "OPENCONFIRM", "open-confirm", "openConfirm":
			return "OPENCONFIRM"
		default:
			return strings.ToUpper(value)
		}
	})

	// Unified alarm severity normalizer.
	RegisterStatusNormalizer("alarm_severity", func(value string) string {
		switch strings.ToUpper(strings.TrimSpace(value)) {
		case "CRITICAL", "2":
			return "CRITICAL"
		case "MAJOR", "3":
			return "MAJOR"
		case "MINOR", "4":
			return "MINOR"
		case "WARNING", "5":
			return "WARNING"
		default:
			return strings.ToUpper(value)
		}
	})

	// get_bgp_neighbors — covers OpenConfig, Nokia SRL, Nokia SROS.
	RegisterCanonicalMap(&OperationCanonicalMap{
		OperationID: "get_bgp_neighbors",
		ModelType:   "BGPNeighbor",
		Fields: []FieldMapping{
			// Neighbor address
			{YANGLeaf: "neighbor-address", CanonicalField: "NeighborAddress"},
			{YANGLeaf: "peer-address", CanonicalField: "NeighborAddress"}, // SRL

			// Peer AS
			{YANGLeaf: "peer-as", CanonicalField: "PeerAS"},

			// Local AS
			{YANGLeaf: "local-as", CanonicalField: "LocalAS"},

			// Session state
			{YANGLeaf: "session-state", CanonicalField: "SessionState", Normalizer: "bgp_session_state"},

			// Peer type
			{YANGLeaf: "peer-type", CanonicalField: "PeerType"},

			// Description
			{YANGLeaf: "description", CanonicalField: "Description"},

			// Prefix counts
			{YANGLeaf: "received-pre-policy", CanonicalField: "PrefixesReceived"}, // OC: prefixes/received-pre-policy
			{YANGLeaf: "received-routes", CanonicalField: "PrefixesReceived"},     // SRL
			{YANGLeaf: "sent", CanonicalField: "PrefixesSent"},                    // OC: prefixes/sent
			{YANGLeaf: "sent-routes", CanonicalField: "PrefixesSent"},             // SRL
			{YANGLeaf: "installed", CanonicalField: "PrefixesInstalled"},          // OC: prefixes/installed
			{YANGLeaf: "active-routes", CanonicalField: "PrefixesInstalled"},      // SRL

			// Uptime / last established
			{YANGLeaf: "last-established", CanonicalField: "LastEstablished"},

			// Message counters
			{YANGLeaf: "messages-received", CanonicalField: "MessagesReceived"},
			{YANGLeaf: "messages-sent", CanonicalField: "MessagesSent"},
		},
		ContainerAliases: []ContainerAlias{
			{Name: "state", MergeUp: true},    // OpenConfig
			{Name: "prefixes", MergeUp: true}, // OC: neighbor/state/prefixes/*
		},
	})

	// get_route_table — covers OpenConfig, Nokia SRL, Nokia SROS.
	RegisterCanonicalMap(&OperationCanonicalMap{
		OperationID: "get_route_table",
		ModelType:   "Route",
		Fields: []FieldMapping{
			// Prefix
			{YANGLeaf: "prefix", CanonicalField: "Prefix"},
			{YANGLeaf: "ip-prefix", CanonicalField: "Prefix"}, // SRL

			// Next hop
			{YANGLeaf: "next-hop", CanonicalField: "NextHop"},
			{YANGLeaf: "next-hop-address", CanonicalField: "NextHop"}, // OC AFT
			{YANGLeaf: "ip-address", CanonicalField: "NextHop"},       // SRL next-hop

			// Protocol / origin
			{YANGLeaf: "origin-protocol", CanonicalField: "Protocol"},                             // OC
			{YANGLeaf: "route-owner", CanonicalField: "Protocol", Normalizer: "route_protocol"},   // SRL
			{YANGLeaf: "protocol-name", CanonicalField: "Protocol", Normalizer: "route_protocol"}, // SROS

			// Preference / admin distance
			{YANGLeaf: "preference", CanonicalField: "Preference"},

			// Metric
			{YANGLeaf: "metric", CanonicalField: "Metric"},

			// Outgoing interface
			{YANGLeaf: "interface-ref", CanonicalField: "Interface"},
			{YANGLeaf: "interface", CanonicalField: "Interface"},

			// Active flag
			{YANGLeaf: "active", CanonicalField: "Active"},
			{YANGLeaf: "fib-active", CanonicalField: "Active"}, // SRL

			// Network instance
			{YANGLeaf: "network-instance", CanonicalField: "NetworkInstance"},
		},
		ContainerAliases: []ContainerAlias{
			{Name: "state", MergeUp: true},    // OpenConfig
			{Name: "next-hop", MergeUp: true}, // OC AFT: next-hops/next-hop/state
		},
	})

	// get_system_alarms — covers OpenConfig, Nokia SRL, Nokia SROS.
	RegisterCanonicalMap(&OperationCanonicalMap{
		OperationID: "get_system_alarms",
		ModelType:   "Alarm",
		Fields: []FieldMapping{
			// Alarm ID
			{YANGLeaf: "id", CanonicalField: "ID"},

			// Resource (what triggered the alarm)
			{YANGLeaf: "resource", CanonicalField: "Resource"},

			// Severity
			{YANGLeaf: "severity", CanonicalField: "Severity", Normalizer: "alarm_severity"},

			// Text / description
			{YANGLeaf: "text", CanonicalField: "Text"},
			{YANGLeaf: "description", CanonicalField: "Text"}, // some vendors

			// Time created
			{YANGLeaf: "time-created", CanonicalField: "TimeCreated"},

			// Type / type-id
			{YANGLeaf: "type-id", CanonicalField: "Type"},
			{YANGLeaf: "type", CanonicalField: "Type"},

			// State (active / cleared)
			{YANGLeaf: "state", CanonicalField: "State"},
		},
		ContainerAliases: []ContainerAlias{
			{Name: "state", MergeUp: true}, // OpenConfig: alarm/state/*
		},
	})
}
