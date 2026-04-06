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

package query

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/sources"
)

// ---------------------------------------------------------------------------
// XML types for OpenConfig interfaces (NETCONF)
// ---------------------------------------------------------------------------

type xmlNCOCInterfaces struct {
	XMLName    xml.Name       `xml:"interfaces"`
	Interfaces []xmlNCOCIface `xml:"interface"`
}

type xmlNCOCIface struct {
	Name   string          `xml:"name"`
	State  xmlNCOCIfState  `xml:"state"`
	Config xmlNCOCIfConfig `xml:"config"`
}

type xmlNCOCIfState struct {
	Type        string `xml:"type"`
	MTU         int    `xml:"mtu"`
	Description string `xml:"description"`
	AdminStatus string `xml:"admin-status"`
	OperStatus  string `xml:"oper-status"`
}

type xmlNCOCIfConfig struct {
	MTU         int    `xml:"mtu"`
	Description string `xml:"description"`
}

// ---------------------------------------------------------------------------
// XML types for Nokia SR Linux native interfaces (NETCONF)
// ---------------------------------------------------------------------------

// xmlNCSRLInterface represents a single interface in the Nokia SR Linux native YANG model.
// SR Linux NETCONF <get> returns multiple sibling <interface> elements.
type xmlNCSRLInterface struct {
	XMLName     xml.Name `xml:"interface"`
	Name        string   `xml:"name"`
	Description string   `xml:"description"`
	AdminState  string   `xml:"admin-state"`
	OperState   string   `xml:"oper-state"`
	MTU         int      `xml:"mtu"`
}

// ---------------------------------------------------------------------------
// XML types for Nokia SR-OS native interfaces (NETCONF)
// State model: /state/router[router-name=Base]/interface
// ---------------------------------------------------------------------------

type xmlNCSROSState struct {
	XMLName xml.Name          `xml:"state"`
	Routers []xmlNCSROSRouter `xml:"router"`
}

type xmlNCSROSRouter struct {
	RouterName string           `xml:"router-name"`
	Interfaces []xmlNCSROSIface `xml:"interface"`
}

type xmlNCSROSIface struct {
	InterfaceName string `xml:"interface-name"`
	Description   string `xml:"description"`
	AdminState    string `xml:"admin-state"`
	OperState     string `xml:"oper-state"`
	MTU           int    `xml:"mtu"`
}

// ---------------------------------------------------------------------------
// XML types for OpenConfig system version (NETCONF)
// Response may contain two top-level elements: <system> and <components>.
// ---------------------------------------------------------------------------

type xmlNCOCSystem struct {
	XMLName xml.Name `xml:"system"`
	State   struct {
		Hostname   string `xml:"hostname"`
		DomainName string `xml:"domain-name"`
	} `xml:"state"`
}

type xmlNCOCComponents struct {
	XMLName    xml.Name           `xml:"components"`
	Components []xmlNCOCComponent `xml:"component"`
}

type xmlNCOCComponent struct {
	Name  string `xml:"name"`
	State struct {
		SoftwareVersion string `xml:"software-version"`
		Description     string `xml:"description"`
	} `xml:"state"`
}

// ---------------------------------------------------------------------------
// XML types for Nokia SR Linux native system (NETCONF)
// ---------------------------------------------------------------------------

// xmlNCSRLSystem parses the SR Linux system response.
// SR Linux distributes system data across two child namespaces:
//   - <information xmlns="urn:nokia.com:srlinux:linux:system-info"> for version/description
//   - <name xmlns="urn:nokia.com:srlinux:chassis:system-name"> for the hostname
//
// Go's encoding/xml matches child elements by local name regardless of
// namespace, so the struct tags work without explicit namespace qualifiers.
type xmlNCSRLSystem struct {
	XMLName     xml.Name `xml:"system"`
	Information struct {
		Version     string `xml:"version"`
		Description string `xml:"description"`
	} `xml:"information"`
	Name struct {
		HostName string `xml:"host-name"`
	} `xml:"name"`
}

// ---------------------------------------------------------------------------
// XML types for Nokia SR-OS native system (NETCONF)
// State model: /state/system/information
// ---------------------------------------------------------------------------

type xmlNCSROSSystemState struct {
	XMLName xml.Name `xml:"state"`
	System  struct {
		Information struct {
			Version     string `xml:"version"`
			Description string `xml:"description"`
			Hostname    string `xml:"hostname"`
		} `xml:"information"`
	} `xml:"system"`
}

// ---------------------------------------------------------------------------
// NETCONF execution
// ---------------------------------------------------------------------------

// executeNetconf runs a NETCONF <get> or <get-config> RPC against the source
// and returns a canonical Record. UseGetConfig selects <get-config> (config
// data only); the default <get> retrieves both configuration and state data,
// which is required for operational metrics such as admin/oper status.
func executeNetconf(ctx context.Context, source sources.Source, pp profiles.ProtocolPath, operationID, sourceID, vendor, platform string, collectedAt time.Time) (*models.Record, error) {
	querier, ok := source.(capabilities.NetconfQuerier)
	if !ok {
		return nil, fmt.Errorf("source %q does not implement NetconfQuerier", sourceID)
	}

	var (
		rawXML []byte
		err    error
	)
	if pp.UseGetConfig {
		ds := pp.Datastore
		if ds == "" {
			ds = "running"
		}
		rawXML, err = querier.NetconfGetConfig(ctx, ds, pp.Filter)
	} else {
		rawXML, err = querier.NetconfGet(ctx, pp.Filter)
	}
	if err != nil {
		return nil, fmt.Errorf("NETCONF RPC failed on source %q: %w", sourceID, err)
	}

	protocol := models.ProtocolNetconfOpenConfig
	if pp.Protocol == profiles.ProtocolNetconfNative {
		protocol = models.ProtocolNetconfNative
	}

	payload, quality, err := normalizeNetconfResponse(rawXML, pp.Protocol, operationID, vendor, platform)
	if err != nil {
		return nil, err
	}

	return &models.Record{
		RecordType:    operationID,
		SchemaVersion: models.SchemaVersion,
		Source: models.SourceMeta{
			DeviceID:  sourceID,
			Vendor:    vendor,
			Platform:  platform,
			Transport: "netconf",
		},
		Collection: models.CollectionMeta{
			Mode:        models.CollectionSnapshot,
			Protocol:    protocol,
			CollectedAt: collectedAt,
		},
		Payload: payload,
		Quality: quality,
		Native: &models.NativeMeta{
			NativePath: pp.Filter,
		},
	}, nil
}

// ---------------------------------------------------------------------------
// NETCONF response normalization
// ---------------------------------------------------------------------------

// normalizeNetconfResponse dispatches XML normalization by operation ID.
func normalizeNetconfResponse(rawXML []byte, protocol profiles.Protocol, operationID, vendor, platform string) (any, models.QualityMeta, error) {
	switch operationID {
	case profiles.OpGetInterfaces:
		return parseNetconfInterfaces(rawXML, protocol, vendor, platform)
	case profiles.OpGetSystemVersion:
		return parseNetconfSystemVersion(rawXML, protocol, vendor, platform)
	default:
		return string(rawXML), models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"no canonical NETCONF parser for operation " + operationID},
		}, nil
	}
}

// parseNetconfInterfaces dispatches to the appropriate interface parser.
func parseNetconfInterfaces(rawXML []byte, protocol profiles.Protocol, vendor, platform string) (any, models.QualityMeta, error) {
	if protocol == profiles.ProtocolNetconfOpenConfig {
		return parseNetconfOCInterfaces(rawXML)
	}
	return parseNetconfNativeInterfaces(rawXML, vendor, platform)
}

// parseNetconfSystemVersion dispatches to the appropriate system version parser.
func parseNetconfSystemVersion(rawXML []byte, protocol profiles.Protocol, vendor, platform string) (any, models.QualityMeta, error) {
	if protocol == profiles.ProtocolNetconfOpenConfig {
		return parseNetconfOCSystemVersion(rawXML)
	}
	return parseNetconfNativeSystemVersion(rawXML, vendor, platform)
}

// ---------------------------------------------------------------------------
// OpenConfig parsers (vendor-agnostic)
// ---------------------------------------------------------------------------

// parseNetconfOCInterfaces parses OpenConfig interfaces XML from a NETCONF <get>
// response. The response contains a single top-level <interfaces> element.
func parseNetconfOCInterfaces(rawXML []byte) (any, models.QualityMeta, error) {
	var root xmlNCOCInterfaces
	if err := xml.Unmarshal(rawXML, &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"XML parse error: " + err.Error()},
		}, fmt.Errorf("failed to parse OpenConfig interfaces XML: %w", err)
	}

	interfaces := make([]models.InterfaceState, 0, len(root.Interfaces))
	for _, iface := range root.Interfaces {
		if iface.Name == "" {
			continue
		}
		state := models.InterfaceState{
			Name:        iface.Name,
			Type:        iface.State.Type,
			AdminStatus: normalizeOCStatus(iface.State.AdminStatus),
			OperStatus:  normalizeOCStatus(iface.State.OperStatus),
		}
		// State container takes precedence over config for description and MTU.
		if iface.State.Description != "" {
			state.Description = iface.State.Description
		} else {
			state.Description = iface.Config.Description
		}
		if iface.State.MTU > 0 {
			state.MTU = iface.State.MTU
		} else {
			state.MTU = iface.Config.MTU
		}
		interfaces = append(interfaces, state)
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if len(interfaces) == 0 {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"no interfaces found in NETCONF OpenConfig response"},
		}
	}
	return interfaces, quality, nil
}

// parseNetconfOCSystemVersion parses OpenConfig system XML from a NETCONF <get>
// response. The response may contain two top-level elements: <system> and
// <components>. A streaming decoder handles both in a single pass.
func parseNetconfOCSystemVersion(rawXML []byte) (any, models.QualityMeta, error) {
	sv := models.SystemVersion{}
	dec := xml.NewDecoder(bytes.NewReader(rawXML))
	for {
		token, err := dec.Token()
		if err != nil {
			break // io.EOF or parse error — stop gracefully
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "system":
			var sys xmlNCOCSystem
			if decErr := dec.DecodeElement(&sys, &start); decErr == nil {
				sv.Hostname = sys.State.Hostname
			}
		case "components":
			var comps xmlNCOCComponents
			if decErr := dec.DecodeElement(&comps, &start); decErr == nil {
				for _, comp := range comps.Components {
					if strings.EqualFold(comp.Name, "chassis") {
						sv.SoftwareVersion = comp.State.SoftwareVersion
						sv.ChassisType = comp.State.Description
						break
					}
				}
			}
		}
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if sv.Hostname == "" && sv.SoftwareVersion == "" {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"minimal system data in NETCONF OpenConfig response"},
		}
	}
	return sv, quality, nil
}

// ---------------------------------------------------------------------------
// Native YANG parsers (vendor-specific dispatch)
// ---------------------------------------------------------------------------

// parseNetconfNativeInterfaces dispatches to the vendor-specific native parser.
func parseNetconfNativeInterfaces(rawXML []byte, vendor, platform string) (any, models.QualityMeta, error) {
	switch strings.ToLower(platform) {
	case "sros":
		return parseNetconfSROSInterfaces(rawXML)
	default:
		// Default handles Nokia SR Linux and similar per-interface XML structures.
		return parseNetconfSRLInterfaces(rawXML)
	}
}

// parseNetconfNativeSystemVersion dispatches to the vendor-specific native system parser.
func parseNetconfNativeSystemVersion(rawXML []byte, vendor, platform string) (any, models.QualityMeta, error) {
	switch strings.ToLower(platform) {
	case "sros":
		return parseNetconfSROSSystemVersion(rawXML)
	default:
		return parseNetconfSRLSystemVersion(rawXML)
	}
}

// ---------------------------------------------------------------------------
// Nokia SR Linux native parsers
// ---------------------------------------------------------------------------

// parseNetconfSRLInterfaces parses Nokia SR Linux native interface XML.
// SR Linux NETCONF <get> returns multiple sibling <interface> elements at the
// top level of the <data> response, so a synthetic wrapper is used for Unmarshal.
func parseNetconfSRLInterfaces(rawXML []byte) (any, models.QualityMeta, error) {
	wrapped := make([]byte, 0, len(rawXML)+13)
	wrapped = append(wrapped, "<root>"...)
	wrapped = append(wrapped, rawXML...)
	wrapped = append(wrapped, "</root>"...)

	var container struct {
		XMLName    xml.Name            `xml:"root"`
		Interfaces []xmlNCSRLInterface `xml:"interface"`
	}
	if err := xml.Unmarshal(wrapped, &container); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"XML parse error: " + err.Error()},
		}, fmt.Errorf("failed to parse SR Linux native interfaces XML: %w", err)
	}

	interfaces := make([]models.InterfaceState, 0, len(container.Interfaces))
	for _, iface := range container.Interfaces {
		if iface.Name == "" {
			continue
		}
		interfaces = append(interfaces, models.InterfaceState{
			Name:        iface.Name,
			Description: iface.Description,
			AdminStatus: normalizeSRLAdminState(iface.AdminState),
			OperStatus:  normalizeSRLOperState(iface.OperState),
			MTU:         iface.MTU,
		})
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if len(interfaces) == 0 {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"no interfaces found in SR Linux native NETCONF response"},
		}
	}
	return interfaces, quality, nil
}

// parseNetconfSRLSystemVersion parses Nokia SR Linux native system version XML.
// The response contains a <system> root with two child containers in separate
// namespaces: <information> (version/description) and <name> (host-name).
func parseNetconfSRLSystemVersion(rawXML []byte) (any, models.QualityMeta, error) {
	var root xmlNCSRLSystem
	if err := xml.Unmarshal(rawXML, &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"XML parse error: " + err.Error()},
		}, fmt.Errorf("failed to parse SR Linux native system XML: %w", err)
	}

	sv := models.SystemVersion{
		Hostname:        root.Name.HostName,
		SoftwareVersion: root.Information.Version,
		ChassisType:     root.Information.Description,
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if sv.SoftwareVersion == "" {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"minimal system data in SR Linux native NETCONF response"},
		}
	}
	return sv, quality, nil
}

// ---------------------------------------------------------------------------
// Nokia SR-OS native parsers
// ---------------------------------------------------------------------------

// parseNetconfSROSInterfaces parses Nokia SR-OS native interface XML.
// The NETCONF <get> response contains a <state> root with nested
// router/interface structure matching /state/router[router-name=Base]/interface.
func parseNetconfSROSInterfaces(rawXML []byte) (any, models.QualityMeta, error) {
	var root xmlNCSROSState
	if err := xml.Unmarshal(rawXML, &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"XML parse error: " + err.Error()},
		}, fmt.Errorf("failed to parse SR-OS native interfaces XML: %w", err)
	}

	var interfaces []models.InterfaceState
	for _, router := range root.Routers {
		for _, iface := range router.Interfaces {
			if iface.InterfaceName == "" {
				continue
			}
			interfaces = append(interfaces, models.InterfaceState{
				Name:        iface.InterfaceName,
				Description: iface.Description,
				AdminStatus: normalizeSROSState(iface.AdminState),
				OperStatus:  normalizeSROSState(iface.OperState),
				MTU:         iface.MTU,
			})
		}
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if len(interfaces) == 0 {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"no interfaces found in SR-OS native NETCONF response"},
		}
	}
	return interfaces, quality, nil
}

// parseNetconfSROSSystemVersion parses Nokia SR-OS native system version XML.
// The response contains a <state> root with nested system/information structure
// matching /state/system/information.
func parseNetconfSROSSystemVersion(rawXML []byte) (any, models.QualityMeta, error) {
	var root xmlNCSROSSystemState
	if err := xml.Unmarshal(rawXML, &root); err != nil {
		return nil, models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"XML parse error: " + err.Error()},
		}, fmt.Errorf("failed to parse SR-OS native system XML: %w", err)
	}

	sv := models.SystemVersion{
		Hostname:        root.System.Information.Hostname,
		SoftwareVersion: root.System.Information.Version,
		ChassisType:     root.System.Information.Description,
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if sv.SoftwareVersion == "" && sv.Hostname == "" {
		quality = models.QualityMeta{
			MappingQuality: models.MappingPartial,
			Warnings:       []string{"minimal system data in SR-OS native NETCONF response"},
		}
	}
	return sv, quality, nil
}

// ---------------------------------------------------------------------------
// Status normalization helpers
// ---------------------------------------------------------------------------

// normalizeSRLAdminState maps Nokia SR Linux admin-state to canonical UP/DOWN.
// SR Linux uses "enable"/"disable".
func normalizeSRLAdminState(s string) string {
	switch strings.ToLower(s) {
	case "enable":
		return "UP"
	case "disable":
		return "DOWN"
	default:
		return s
	}
}

// normalizeSRLOperState maps Nokia SR Linux oper-state to canonical UP/DOWN.
// SR Linux uses "up"/"down".
func normalizeSRLOperState(s string) string {
	switch strings.ToLower(s) {
	case "up":
		return "UP"
	case "down":
		return "DOWN"
	default:
		return s
	}
}

// normalizeSROSState maps Nokia SR-OS operational state values to canonical UP/DOWN.
// SR-OS uses "inService"/"outOfService"/"shutdown".
func normalizeSROSState(s string) string {
	switch s {
	case "inService":
		return "UP"
	case "outOfService", "shutdown":
		return "DOWN"
	default:
		return s
	}
}
