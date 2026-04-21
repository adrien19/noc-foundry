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

	op := findCoverageOp(t, report, profiles.OpGetInterfaces)
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

	op := findCoverageOp(t, report, "custom_unmapped")
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

func TestBuildCoverageReport_Diagnostics(t *testing.T) {
	report := BuildCoverageReport(&profiles.Profile{
		Vendor:   "nokia",
		Platform: "srlinux",
		DiagnosticCommands: map[string]profiles.DiagnosticCommandTemplate{
			profiles.OpRunPing: {
				OperationID: profiles.OpRunPing,
				Transport:   profiles.DiagnosticTransportCLI,
				Command:     "ping {target} -c {count}",
			},
		},
	}, nil)

	ping := findCoverageOp(t, report, profiles.OpRunPing)
	if ping.DiagnosticTransport != "cli" {
		t.Fatalf("DiagnosticTransport = %q, want cli", ping.DiagnosticTransport)
	}
	if !ping.DiagnosticTemplate {
		t.Fatal("expected diagnostic template to be present")
	}
	if !ping.DiagnosticTypedResult {
		t.Fatal("expected diagnostic typed result to be present")
	}
	if ping.DiagnosticReady != "ops-ready" || ping.Readiness != "ops-ready" {
		t.Fatalf("diagnostic readiness = %q / %q, want ops-ready", ping.DiagnosticReady, ping.Readiness)
	}

	traceroute := findCoverageOp(t, report, profiles.OpRunTraceroute)
	if traceroute.DiagnosticReady != "registered" {
		t.Fatalf("missing diagnostic template readiness = %q, want registered", traceroute.DiagnosticReady)
	}
}

func findCoverageOp(t *testing.T, report CoverageReport, opID string) OperationCoverage {
	t.Helper()
	for _, op := range report.Operations {
		if op.OperationID == opID {
			return op
		}
	}
	t.Fatalf("operation %q not found in coverage report", opID)
	return OperationCoverage{}
}
