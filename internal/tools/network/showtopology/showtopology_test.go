package showtopology

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

func TestBuildTopologyResult_BidirectionalAndUnmatched(t *testing.T) {
	result := fanout.Result{Results: []fanout.DeviceResult{
		{
			Device: "spine-1",
			Status: "success",
			Data: &models.Record{RecordType: profiles.OpGetLLDPNeighbors, Payload: []models.LLDPNeighbor{
				{LocalInterface: "ethernet-1/1", RemoteSystemName: "leaf-1", RemotePortID: "ethernet-1/1"},
				{LocalInterface: "ethernet-1/2", RemoteSystemName: "unknown-node", RemotePortID: "eth0"},
			}},
		},
		{
			Device: "leaf-1",
			Status: "success",
			Data: &models.Record{RecordType: profiles.OpGetLLDPNeighbors, Payload: []models.LLDPNeighbor{
				{LocalInterface: "ethernet-1/1", RemoteSystemName: "spine-1", RemotePortID: "ethernet-1/1"},
			}},
		},
	}}
	labels := map[string]map[string]string{
		"dc1/spine-1/default": {"role": "spine"},
		"dc1/leaf-1/default":  {"role": "leaf"},
	}

	got := buildTopologyResult(result, labels)
	if len(got.Topology.Links) != 3 {
		t.Fatalf("links = %d, want 3", len(got.Topology.Links))
	}
	spineLeaf := findLink(t, got.Topology.Links, "spine-1", "ethernet-1/1", "leaf-1")
	if spineLeaf.Confidence != "high" || !hasEvidence(spineLeaf.Evidence, "lldp_bidirectional") {
		t.Fatalf("bidirectional link not marked high confidence: %+v", spineLeaf)
	}
	unmatched := findLink(t, got.Topology.Links, "spine-1", "ethernet-1/2", "unknown-node")
	if unmatched.Confidence != "low" || !hasEvidence(unmatched.Evidence, "unmatched_inventory_node") {
		t.Fatalf("unmatched link evidence missing: %+v", unmatched)
	}
}

func TestBuildTopologyResult_SingleSidedManagedAndMissingRemoteInterface(t *testing.T) {
	result := fanout.Result{Results: []fanout.DeviceResult{
		{
			Device: "spine-1",
			Status: "success",
			Data: &models.Record{RecordType: profiles.OpGetLLDPNeighbors, Payload: []models.LLDPNeighbor{
				{LocalInterface: "ethernet-1/10", RemoteSystemName: "leaf-1", RemotePortID: ""},
			}},
		},
		{
			Device: "leaf-1",
			Status: "error",
			Error:  "timeout",
		},
	}}
	labels := map[string]map[string]string{
		"dc1/spine-1/default": {"role": "spine"},
		"dc1/leaf-1/default":  {"role": "leaf"},
	}

	got := buildTopologyResult(result, labels)
	link := findLink(t, got.Topology.Links, "spine-1", "ethernet-1/10", "leaf-1")
	if link.Confidence != "medium" {
		t.Fatalf("confidence = %q, want medium", link.Confidence)
	}
	if !hasEvidence(link.Evidence, "lldp_single_sided") || !hasEvidence(link.Evidence, "inventory_node_matched") || !hasEvidence(link.Evidence, "remote_interface_missing") {
		t.Fatalf("evidence = %+v, want single-sided inventory-matched remote-interface-missing", link.Evidence)
	}
	if len(got.Errors) != 1 || got.Errors[0].Device != "leaf-1" {
		t.Fatalf("errors = %+v, want leaf-1 partial failure", got.Errors)
	}
}

func findLink(t *testing.T, links []models.TopologyLink, localDevice, localInterface, remoteDevice string) models.TopologyLink {
	t.Helper()
	for _, link := range links {
		if link.LocalDevice == localDevice && link.LocalInterface == localInterface && link.RemoteDevice == remoteDevice {
			return link
		}
	}
	t.Fatalf("link %s %s -> %s not found", localDevice, localInterface, remoteDevice)
	return models.TopologyLink{}
}

func hasEvidence(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
