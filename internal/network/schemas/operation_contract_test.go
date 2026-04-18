package schemas

import (
	"path/filepath"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/goccy/go-yaml"
)

func TestSidecarOperationContractDecode(t *testing.T) {
	raw := []byte(`
operations:
  - id: get_static_routes
    data: config
    datastore: running
    preferred: [gnmi_native, netconf_native]
    parameters:
      - name: network_instance
        path_key: name
        default: default
    limits:
      default_count: 100
      max_count: 1000
    native_paths:
      - /native/static
`)
	var ops SidecarOps
	if err := yaml.Unmarshal(raw, &ops); err != nil {
		t.Fatal(err)
	}
	mappings := ops.ToOperationMappings()
	if len(mappings) != 1 {
		t.Fatalf("mappings = %d; want 1", len(mappings))
	}
	got := mappings[0]
	if got.Data != OperationDataConfig {
		t.Fatalf("Data = %q; want config", got.Data)
	}
	if got.datastore() != "running" {
		t.Fatalf("datastore = %q; want running", got.datastore())
	}
	if got.Preferred[0] != "gnmi_native" {
		t.Fatalf("preferred[0] = %q; want gnmi_native", got.Preferred[0])
	}
	if got.Parameters[0].Name != "network_instance" || got.Parameters[0].PathKey != "name" {
		t.Fatalf("parameter = %+v; want network_instance/name", got.Parameters[0])
	}
	if got.Limits == nil || got.Limits.MaxCount != 1000 {
		t.Fatalf("limits = %+v; want max_count 1000", got.Limits)
	}
}

func TestBuildProfile_ContractDatastoreAndPreference(t *testing.T) {
	store := NewSchemaStore()
	key := SchemaKey{Vendor: "test", Platform: "device", Version: "1.0"}
	yangDir := filepath.Join("testdata", "minimal")
	if err := store.Load(key, []string{yangDir}); err != nil {
		t.Fatal(err)
	}
	b, _ := store.Lookup("test", "device", "1.0")

	profile, warnings := BuildProfile(b, []OperationMapping{{
		OperationID: "get_static_routes",
		Data:        OperationDataConfig,
		Datastore:   "candidate",
		Preferred:   []string{"netconf_native", "gnmi_native"},
		NativePaths: []string{"interfaces"},
	}})
	if len(warnings) > 0 {
		t.Logf("warnings: %v", warnings)
	}
	op := profile.Operations["get_static_routes"]
	if len(op.Paths) < 2 {
		t.Fatalf("paths = %d; want at least 2", len(op.Paths))
	}
	if op.Paths[0].Protocol != profiles.ProtocolNetconfNative {
		t.Fatalf("first protocol = %q; want netconf_native", op.Paths[0].Protocol)
	}
	if !op.Paths[0].UseGetConfig || op.Paths[0].Datastore != "candidate" {
		t.Fatalf("netconf path = %+v; want get-config candidate", op.Paths[0])
	}
}

func TestSchemaMapper_GenericModel(t *testing.T) {
	mapper, err := NewSchemaMapper(nil, "get_bgp_neighbors")
	if err != nil {
		t.Fatal(err)
	}
	payload, quality, err := mapper.MapJSON(map[string]any{
		"neighbor": []any{
			map[string]any{
				"peer-address":  "192.0.2.1",
				"peer-as":       float64(64512),
				"session-state": "established",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if quality.MappingQuality != "exact" {
		t.Fatalf("quality = %q; want exact", quality.MappingQuality)
	}
	neighbors, ok := payload.([]models.BGPNeighbor)
	if !ok {
		t.Fatalf("payload type = %T; want []models.BGPNeighbor", payload)
	}
	if len(neighbors) != 1 || neighbors[0].NeighborAddress != "192.0.2.1" || neighbors[0].SessionState != "ESTABLISHED" {
		t.Fatalf("neighbors = %+v", neighbors)
	}
}

func TestBuildCoverageReport(t *testing.T) {
	report := BuildCoverageReport(&profiles.Profile{
		Vendor:   "nokia",
		Platform: "srlinux",
		Operations: map[string]profiles.OperationDescriptor{
			"get_interfaces": {
				OperationID: "get_interfaces",
				Paths: []profiles.ProtocolPath{
					{Protocol: profiles.ProtocolGnmiNative, Paths: []string{"/interface"}},
				},
			},
		},
	}, nil)
	if len(report.Operations) != 1 {
		t.Fatalf("coverage operations = %d; want 1", len(report.Operations))
	}
	got := report.Operations[0]
	if !got.CanonicalMapPresent || !got.CanonicalModelPresent || !got.DedicatedToolPresent {
		t.Fatalf("coverage = %+v; want canonical map/model and dedicated tool", got)
	}
}
