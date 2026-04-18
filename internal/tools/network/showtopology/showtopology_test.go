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
	if got.Topology.Links[0].Confidence != "high" || got.Topology.Links[0].Evidence[0] != "lldp_bidirectional" {
		t.Fatalf("bidirectional link not marked high confidence: %+v", got.Topology.Links[0])
	}
	unmatched := got.Topology.Links[1]
	if unmatched.Confidence != "low" || len(unmatched.Evidence) < 2 || unmatched.Evidence[1] != "unmatched_inventory_node" {
		t.Fatalf("unmatched link evidence missing: %+v", unmatched)
	}
}
