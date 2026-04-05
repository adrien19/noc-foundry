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
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "srlinux", Operation: "get_interfaces", Format: "text"}, func(raw string) (any, models.QualityMeta, error) {
		return ParseSRLinuxInterfaces(raw), models.QualityMeta{MappingQuality: models.MappingDerived}, nil
	})
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "srlinux", Operation: "get_system_version", Format: "text"}, func(raw string) (any, models.QualityMeta, error) {
		return ParseSRLinuxSystemVersion(raw), models.QualityMeta{MappingQuality: models.MappingDerived}, nil
	})

	// JSON parsers — used when the device returns structured JSON output.
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "srlinux", Operation: "get_interfaces", Format: "json"}, parseSRLinuxInterfacesJSON)
	RegisterParser(ParserKey{Vendor: "nokia", Platform: "srlinux", Operation: "get_system_version", Format: "json"}, parseSRLinuxSystemVersionJSON)
}

// ParseSRLinuxInterfaces parses "show interface" CLI output from an
// SRLinux device into canonical InterfaceState records.
func ParseSRLinuxInterfaces(raw string) []models.InterfaceState {
	lines := Lines(raw)
	var interfaces []models.InterfaceState
	var current *models.InterfaceState

	for _, line := range lines {
		if IsSeparatorLine(line, '=') || IsSeparatorLine(line, '-') {
			continue
		}

		// SRLinux "show interface" format:
		// <interface-name> is <admin-status>, speed <speed>, type <type>
		//   <description>
		//   oper-status is <oper-status>
		if !strings.HasPrefix(line, " ") && strings.Contains(line, " is ") {
			if current != nil {
				interfaces = append(interfaces, *current)
			}
			current = &models.InterfaceState{}
			parts := strings.SplitN(line, " is ", 2)
			current.Name = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				fields := strings.Split(parts[1], ",")
				for _, f := range fields {
					f = strings.TrimSpace(f)
					if strings.HasPrefix(f, "speed ") {
						current.Speed = strings.TrimPrefix(f, "speed ")
					} else if strings.HasPrefix(f, "type ") {
						current.Type = strings.TrimPrefix(f, "type ")
					} else {
						current.AdminStatus = NormalizeStatus(f)
					}
				}
			}
			continue
		}

		if current != nil {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "oper-status is ") {
				current.OperStatus = NormalizeStatus(strings.TrimPrefix(trimmed, "oper-status is "))
			} else if k, v, ok := ExtractKeyValue(trimmed, ":"); ok {
				switch strings.ToLower(k) {
				case "description":
					current.Description = v
				}
			}
		}
	}
	if current != nil {
		interfaces = append(interfaces, *current)
	}
	return interfaces
}

// parseSRLinuxInterfacesJSON parses JSON-formatted "show interface | json"
// output from an SRLinux device into canonical InterfaceState records.
//
// SRLinux typically wraps the list under an "interface" key:
//
//	{"interface": [{"name": "ethernet-1/1", "admin-state": "enable", ...}]}
//
// A bare JSON array is also accepted.
func parseSRLinuxInterfacesJSON(raw string) (any, models.QualityMeta, error) {
	var root any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"JSON parse error: " + err.Error()},
		}, fmt.Errorf("SRLinux interfaces JSON parse error: %w", err)
	}

	// Normalize root to a list of interface objects.
	var items []any
	switch v := root.(type) {
	case []any:
		items = v
	case map[string]any:
		if arr, ok := v["interface"].([]any); ok {
			items = arr
		} else if arr, ok := v["interfaces"].([]any); ok {
			items = arr
		} else if section, ok := v["interfaces"].(map[string]any); ok {
			if arr, ok := section["interface"].([]any); ok {
				items = arr
			} else {
				items = []any{section}
			}
		} else {
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
		if name, ok := m["name"].(string); ok {
			iface.Name = name
		}
		if adm, ok := m["admin-state"].(string); ok {
			iface.AdminStatus = NormalizeStatus(adm)
		}
		if oper, ok := m["oper-state"].(string); ok {
			iface.OperStatus = NormalizeStatus(oper)
		} else if subs, ok := m["subinterfaces"].([]any); ok {
			for _, sub := range subs {
				sm, ok := sub.(map[string]any)
				if !ok {
					continue
				}
				if oper, ok := sm["oper-state"].(string); ok {
					iface.OperStatus = NormalizeStatus(oper)
					break
				}
			}
		}
		if desc, ok := m["description"].(string); ok {
			iface.Description = desc
		}
		if speed, ok := m["speed"].(string); ok {
			iface.Speed = speed
		}
		if typ, ok := m["type"].(string); ok {
			iface.Type = typ
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

// parseSRLinuxSystemVersionJSON parses JSON-formatted "show version | json"
// output from an SRLinux device into a canonical SystemVersion record.
//
// SR Linux `show version | as json` may nest all fields under a single top-level
// section key (e.g. "basic system info") and uses Title Case / space-separated
// key names that mirror the text output. This parser normalises all keys by
// lowercasing and replacing spaces with hyphens before matching, making it
// robust to both YANG-style keys ("software-version") and display-style keys
// ("Software Version").
func parseSRLinuxSystemVersionJSON(raw string) (any, models.QualityMeta, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"JSON parse error: " + err.Error()},
		}, fmt.Errorf("SRLinux system version JSON parse error: %w", err)
	}

	// If the JSON has exactly one top-level key whose value is an object, treat
	// that nested object as the real payload (e.g. {"basic system info": {...}}).
	flat := root
	if len(root) == 1 {
		for _, v := range root {
			if nested, ok := v.(map[string]any); ok {
				flat = nested
			}
		}
	}

	sv := models.SystemVersion{}
	for k, v := range flat {
		norm := strings.ToLower(strings.ReplaceAll(k, " ", "-"))
		s, ok := v.(string)
		if !ok {
			continue
		}
		switch norm {
		case "hostname":
			sv.Hostname = s
		case "software-version", "version", "sw-version":
			if sv.SoftwareVersion == "" {
				sv.SoftwareVersion = s
			}
		case "system-type", "platform-type":
			sv.SystemType = s
		case "chassis-type":
			sv.ChassisType = s
		case "last-booted", "uptime":
			sv.Uptime = s
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

// ParseSRLinuxSystemVersion parses "show version" CLI output from an
// SRLinux device into a canonical SystemVersion record.
func ParseSRLinuxSystemVersion(raw string) models.SystemVersion {
	sv := models.SystemVersion{}
	lines := Lines(raw)
	for _, line := range lines {
		if IsSeparatorLine(line, '=') || IsSeparatorLine(line, '-') {
			continue
		}
		if k, v, ok := ExtractKeyValue(line, ":"); ok {
			switch strings.ToLower(k) {
			case "hostname":
				sv.Hostname = v
			case "software version", "version":
				sv.SoftwareVersion = v
			case "system type", "platform type":
				sv.SystemType = v
			case "chassis type":
				sv.ChassisType = v
			case "last booted", "uptime":
				sv.Uptime = v
			}
		}
	}
	return sv
}
