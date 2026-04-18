package schemas

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

func TestBuildCoverageReport_OriginAndReadiness(t *testing.T) {
	key := SchemaKey{Vendor: "coverage", Platform: "router", Version: "v1"}
	RegisterSidecarMappingsWithOrigin(key, []OperationMapping{{OperationID: profiles.OpGetInterfaces}}, "prebuilt:coverage/router")
	t.Cleanup(func() { ResetSidecarMappings() })

	report := BuildCoverageReport(&profiles.Profile{
		Vendor:   key.Vendor,
		Platform: key.Platform,
		Version:  key.Version,
		Operations: map[string]profiles.OperationDescriptor{
			profiles.OpGetInterfaces: {
				OperationID: profiles.OpGetInterfaces,
				Paths: []profiles.ProtocolPath{
					{Protocol: profiles.ProtocolGnmiNative, Paths: []string{"/interface"}},
				},
			},
		},
	}, nil)

	if len(report.Operations) != 1 {
		t.Fatalf("operations = %d, want 1", len(report.Operations))
	}
	op := report.Operations[0]
	if op.SidecarOrigin != "prebuilt" {
		t.Fatalf("SidecarOrigin = %q, want prebuilt", op.SidecarOrigin)
	}
	if op.Readiness != "ops-ready" {
		t.Fatalf("Readiness = %q, want ops-ready", op.Readiness)
	}
}

func TestBuildCoverageReport_FallbackOrigin(t *testing.T) {
	report := BuildCoverageReport(&profiles.Profile{
		Vendor:   "fallback",
		Platform: "router",
		Operations: map[string]profiles.OperationDescriptor{
			"custom_unmapped": {
				OperationID: "custom_unmapped",
				Paths:       []profiles.ProtocolPath{{Protocol: profiles.ProtocolCLI, Command: "show custom"}},
			},
		},
	}, []string{"operation custom_unmapped: no canonical map"})

	if len(report.Operations) != 1 {
		t.Fatalf("operations = %d, want 1", len(report.Operations))
	}
	op := report.Operations[0]
	if op.SidecarOrigin != "fallback" {
		t.Fatalf("SidecarOrigin = %q, want fallback", op.SidecarOrigin)
	}
	if op.Readiness != "schema-ready" {
		t.Fatalf("Readiness = %q, want schema-ready", op.Readiness)
	}
	if len(op.Warnings) == 0 {
		t.Fatal("expected warning to be attached to operation")
	}
}
