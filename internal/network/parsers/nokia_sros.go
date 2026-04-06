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

package parsers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adrien19/noc-foundry/internal/network/models"
)

func init() {
	// Text parsers — wraps the existing hand-written CLI parsers.
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "sros", Operation: "get_interfaces", Format: "text"}, func(raw string) (any, models.QualityMeta, error) {
		return ParseSROSInterfaces(raw), models.QualityMeta{MappingQuality: models.MappingDerived}, nil
	})
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "sros", Operation: "get_system_version", Format: "text"}, func(raw string) (any, models.QualityMeta, error) {
		return ParseSROSSystemVersion(raw), models.QualityMeta{MappingQuality: models.MappingDerived}, nil
	})

	// JSON parsers — used when the device returns structured JSON output
	// (SROS 20.x+ with MD-CLI enabled, using "| json" output redirect).
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "sros", Operation: "get_interfaces", Format: "json"}, parseSROSInterfacesJSON)
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "sros", Operation: "get_system_version", Format: "json"}, parseSROSSystemVersionJSON)
}

// parseSROSInterfacesJSON parses JSON-formatted "show router interface | json"
// output (MD-CLI, SROS 20.x+) into canonical InterfaceState records.
//
// SROS wraps the list under a "router-interface" or "interface" key
// depending on the model version. A bare JSON array is also accepted.
func parseSROSInterfacesJSON(raw string) (any, models.QualityMeta, error) {
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"JSON parse error: " + err.Error()},
		}, fmt.Errorf("SROS interfaces JSON parse error: %w", err)
	}

	var items []any
	switch v := root.(type) {
	case []any:
		items = v
	case map[string]any:
		// Try common SROS wrapper keys in order of preference.
		for _, key := range []string{"router-interface", "interface", "Interface"} {
			if arr, ok := v[key].([]any); ok {
				items = arr
				break
			}
		}
		if items == nil {
			items = []any{v}
		}
	}

	var interfaces []models.InterfaceState
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		iface := models.InterfaceState{}
		for _, nameKey := range []string{"interface-name", "name", "intf-name"} {
			if v, ok := m[nameKey].(string); ok {
				iface.Name = v
				break
			}
		}
		for _, admKey := range []string{"admin-state", "admin-status"} {
			if v, ok := m[admKey].(string); ok {
				iface.AdminStatus = NormalizeStatus(v)
				break
			}
		}
		for _, operKey := range []string{"oper-state", "oper-status"} {
			if v, ok := m[operKey].(string); ok {
				iface.OperStatus = NormalizeStatus(v)
				break
			}
		}
		if v, ok := m["description"].(string); ok {
			iface.Description = v
		}
		if iface.Name != "" {
			interfaces = append(interfaces, iface)
		}
	}

	if len(interfaces) == 0 {
		return interfaces, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"no interfaces parsed from JSON output"},
		}, nil
	}
	return interfaces, models.QualityMeta{MappingQuality: models.MappingExact}, nil
}

// parseSROSSystemVersionJSON parses JSON-formatted "show system information | json"
// output (MD-CLI, SROS 20.x+) into a canonical SystemVersion record.
func parseSROSSystemVersionJSON(raw string) (any, models.QualityMeta, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"JSON parse error: " + err.Error()},
		}, fmt.Errorf("SROS system version JSON parse error: %w", err)
	}

	sv := models.SystemVersion{}
	for _, k := range []string{"system-name", "name", "hostname"} {
		if v, ok := root[k].(string); ok && v != "" {
			sv.Hostname = v
			break
		}
	}
	for _, k := range []string{"software-version", "system-version", "version"} {
		if v, ok := root[k].(string); ok && v != "" {
			sv.SoftwareVersion = v
			break
		}
	}
	if v, ok := root["system-type"].(string); ok {
		sv.SystemType = v
	}
	if v, ok := root["chassis-type"].(string); ok {
		sv.ChassisType = v
	}
	for _, k := range []string{"system-up-time", "up-time", "uptime"} {
		if v, ok := root[k].(string); ok && v != "" {
			sv.Uptime = v
			break
		}
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if sv.Hostname == "" && sv.SoftwareVersion == "" {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"minimal system data parsed from JSON output"},
		}
	}
	return sv, quality, nil
}

// ParseSROSInterfaces parses "show router interface" CLI output from
// an SROS device into canonical InterfaceState records.
//
// Expected table format:
// ===============================================================================
// Interface Table (Router: Base)
// ===============================================================================
// Interface-Name                  Adm       Opr(v4/v6)  Mode     Port/SapId
//
//	IP-Address                                            PfxState
//
// -------------------------------------------------------------------------------
// system                          Up        Up/Down     Network  -
//
//	10.0.0.1/32                                           n/a
//
// toR2                            Up        Up/Down     Network  1/1/1
//
//	10.1.1.1/30                                           n/a
//
// ===============================================================================
func ParseSROSInterfaces(raw string) []models.InterfaceState {
	lines := Lines(raw)
	var interfaces []models.InterfaceState
	inTable := false

	for _, line := range lines {
		if IsSeparatorLine(line, '=') {
			inTable = false
			continue
		}
		if IsSeparatorLine(line, '-') {
			inTable = true
			continue
		}
		if strings.Contains(line, "Interface-Name") {
			continue
		}
		if strings.Contains(line, "IP-Address") {
			continue
		}
		if !inTable {
			continue
		}

		// Skip indented lines (IP addresses under interface entries)
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		iface := models.InterfaceState{
			Name:        fields[0],
			AdminStatus: NormalizeStatus(fields[1]),
		}

		// Opr field is typically "Up/Down" format for v4/v6
		oprParts := strings.SplitN(fields[2], "/", 2)
		iface.OperStatus = NormalizeStatus(oprParts[0])

		if len(fields) >= 4 {
			iface.VendorExtensions = map[string]any{
				"mode": fields[3],
			}
		}

		interfaces = append(interfaces, iface)
	}
	return interfaces
}

// ParseSROSSystemVersion parses "show system information" CLI output from
// an SROS device into a canonical SystemVersion record.
func ParseSROSSystemVersion(raw string) models.SystemVersion {
	sv := models.SystemVersion{}
	lines := Lines(raw)
	for _, line := range lines {
		if IsSeparatorLine(line, '=') || IsSeparatorLine(line, '-') {
			continue
		}
		if k, v, ok := ExtractKeyValue(line, ":"); ok {
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "system name":
				sv.Hostname = v
			case "system version":
				sv.SoftwareVersion = v
			case "system type":
				sv.SystemType = v
			case "chassis type":
				sv.ChassisType = v
			case "system up time", "up time":
				sv.Uptime = v
			}
		}
	}
	return sv
}
