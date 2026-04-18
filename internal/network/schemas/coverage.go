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
	"strings"

	"github.com/adrien19/noc-foundry/internal/network/parsers"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
)

// OperationCoverage describes startup-time readiness for one schema-derived
// operation. It is intentionally compact so it can be logged and tested.
type OperationCoverage struct {
	OperationID           string   `json:"operation_id"`
	Protocols             []string `json:"protocols"`
	Warnings              []string `json:"warnings,omitempty"`
	CanonicalMapPresent   bool     `json:"canonical_map_present"`
	CanonicalModelPresent bool     `json:"canonical_model_present"`
	CLIParsers            []string `json:"cli_parsers,omitempty"`
	DedicatedToolPresent  bool     `json:"dedicated_tool_present"`
	Readiness             string   `json:"readiness"`
	SidecarOrigin         string   `json:"sidecar_origin,omitempty"`
}

// CoverageReport summarizes operation readiness for a vendor/platform/version.
type CoverageReport struct {
	Vendor     string              `json:"vendor"`
	Platform   string              `json:"platform"`
	Version    string              `json:"version,omitempty"`
	Operations []OperationCoverage `json:"operations"`
}

var dedicatedToolOperations = map[string]bool{
	"get_interfaces":          true,
	"get_system_version":      true,
	"get_bgp_neighbors":       true,
	"get_route_table":         true,
	"get_system_alarms":       true,
	"get_lldp_neighbors":      true,
	"get_bgp_rib":             true,
	"get_ospf_neighbors":      true,
	"get_isis_adjacencies":    true,
	"get_platform_components": true,
	"get_transceiver_state":   true,
	"get_acl":                 true,
	"get_qos_interfaces":      true,
	"get_routing_policy":      true,
	"get_log_entries":         true,
	"get_config_section":      true,
}

// BuildCoverageReport creates a testable operation coverage view.
func BuildCoverageReport(profile *profiles.Profile, warnings []string) CoverageReport {
	report := CoverageReport{}
	if profile == nil {
		return report
	}
	report.Vendor = profile.Vendor
	report.Platform = profile.Platform
	report.Version = profile.Version

	warningsByOp := warningsByOperation(warnings)
	for opID, op := range profile.Operations {
		coverage := OperationCoverage{
			OperationID:           opID,
			Warnings:              warningsByOp[opID],
			DedicatedToolPresent:  dedicatedToolOperations[opID],
			CanonicalMapPresent:   hasCanonicalMap(opID),
			CanonicalModelPresent: hasCanonicalModel(opID),
			// TODO(schema-coverage): Record exact sidecar origin (opsFile,
			// repo-local, prebuilt, or fallback) in the sidecar registry and
			// surface it here. Operators need this to debug why a path came from
			// a particular mapping source.
			SidecarOrigin: "unknown",
		}
		protocolSeen := map[string]bool{}
		for _, pp := range op.Paths {
			protocol := string(pp.Protocol)
			if !protocolSeen[protocol] {
				coverage.Protocols = append(coverage.Protocols, protocol)
				protocolSeen[protocol] = true
			}
		}
		for _, format := range []string{"json", "text"} {
			if parsers.HasParser(parsers.ParserKey{Vendor: profile.Vendor, Platform: profile.Platform, Operation: opID, Format: format}) {
				coverage.CLIParsers = append(coverage.CLIParsers, format)
			}
		}
		coverage.Readiness = readinessForCoverage(coverage)
		report.Operations = append(report.Operations, coverage)
	}
	// TODO(schema-coverage): Expose CoverageReport through an operator-facing
	// debug/readiness tool so NOC users can inspect missing mappings without
	// reading startup logs.
	return report
}

func readinessForCoverage(c OperationCoverage) string {
	if len(c.Protocols) == 0 {
		return "registered"
	}
	if !c.CanonicalMapPresent || !c.CanonicalModelPresent {
		return "schema-ready"
	}
	if c.DedicatedToolPresent {
		return "tool-ready"
	}
	return "canonical-ready"
}

func warningsByOperation(warnings []string) map[string][]string {
	out := map[string][]string{}
	for _, warning := range warnings {
		opID := ""
		if strings.HasPrefix(warning, "operation ") {
			rest := strings.TrimPrefix(warning, "operation ")
			if idx := strings.Index(rest, ":"); idx >= 0 {
				opID = rest[:idx]
			}
		}
		if opID == "" {
			opID = "_global"
		}
		out[opID] = append(out[opID], warning)
	}
	return out
}

func hasCanonicalMap(operationID string) bool {
	_, ok := LookupCanonicalMap(operationID)
	return ok
}

func hasCanonicalModel(operationID string) bool {
	_, ok := LookupCanonicalModel(operationID)
	return ok
}
