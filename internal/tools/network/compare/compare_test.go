package compare

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
)

func TestBuildComparison_PartialErrorsAndDifferences(t *testing.T) {
	result := fanout.Result{Results: []fanout.DeviceResult{
		{
			Device: "spine-1",
			Status: "success",
			Data:   &models.Record{Payload: []models.InterfaceState{{Name: "ethernet-1/1", OperStatus: "UP"}}},
		},
		{
			Device: "spine-2",
			Status: "success",
			Data:   &models.Record{Payload: []models.InterfaceState{{Name: "ethernet-1/1", OperStatus: "DOWN"}}},
		},
		{
			Device: "spine-3",
			Status: "error",
			Error:  "device unavailable",
		},
	}}

	got := buildComparison("get_interfaces", result)
	if len(got.Devices) != 3 {
		t.Fatalf("devices = %d, want 3", len(got.Devices))
	}
	if got.Devices[2].Error == "" {
		t.Fatal("expected per-device error")
	}
	if len(got.Differences) != 1 {
		t.Fatalf("differences = %d, want 1", len(got.Differences))
	}
	if _, ok := got.Differences[0].Values["spine-1"]; !ok {
		t.Fatalf("difference values missing spine-1: %+v", got.Differences[0].Values)
	}
}
