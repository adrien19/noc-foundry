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

// Package query implements a read-only query executor that routes
// operations through the correct protocol based on source capabilities
// and model profiles. The executor tries protocols in preference order
// (gNMI OpenConfig -> gNMI native -> CLI) and normalizes all output
// into the canonical NOCFoundry schema.
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/parsers"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/safety"
	"github.com/adrien19/noc-foundry/internal/network/schemas"
	"github.com/adrien19/noc-foundry/internal/sources"
)

// Executor runs read-only query operations against network sources.
// Use NewExecutor to create an instance; optionally chain WithTransforms
// to attach user-defined jq transforms for CLI operations.
type Executor struct {
	// Transforms holds optional per-operation jq transforms. When a transform
	// is registered for an operation, it replaces the built-in CLI parser for
	// that operation on this Executor instance.
	Transforms TransformSet
	// SchemaStore provides optional YANG schema metadata. When set,
	// NativeMeta on query results is populated with model name, revision,
	// and native path information from the compiled schema.
	SchemaStore *schemas.SchemaStore
}

// NewExecutor returns a new query executor. If a default SchemaStore has
// been set (via schemas.SetDefault at startup), it is automatically used
// for YANG metadata enrichment.
func NewExecutor() *Executor {
	return &Executor{SchemaStore: schemas.Default()}
}

// WithSchemaStore returns a copy of the Executor with the given schema store.
func (e *Executor) WithSchemaStore(store *schemas.SchemaStore) *Executor {
	return &Executor{Transforms: e.Transforms, SchemaStore: store}
}

// WithTransforms returns a new Executor that applies the given transform set
// to CLI operations. The original Executor is not modified.
func (e *Executor) WithTransforms(ts TransformSet) *Executor {
	return &Executor{Transforms: ts, SchemaStore: e.SchemaStore}
}

// OpRunCommand is the TransformSet key used by ExecuteCommand.
// When configuring a network-show tool with a jq transform, use this key:
//
//	transforms:
//	  run_command:
//	    format: json
//	    jq: '.some | .expression'
const OpRunCommand = "run_command"

// ExecuteCommand runs an arbitrary CLI command against the given source,
// applying safety validation and an optional jq transform.
//
// Unlike Execute, ExecuteCommand bypasses profile-based operation routing.
// It is intended for generic tools where the caller explicitly supplies the
// command string. The command must pass safety.ValidateReadOnlyCommand;
// state-changing commands (configure, delete, commit, etc.) are rejected.
//
// If the Executor has a transform registered for OpRunCommand, the jq
// expression is applied to the raw output before returning.  The transform's
// Format field controls how the raw output is presented to jq:
//   - "json": raw output is JSON-unmarshalled before the query runs.
//   - "text" or "": raw output is wrapped as {"text": raw}.
//
// If no transform is configured the raw CLI output is returned as the
// Record payload with MappingPartial quality.
func (e *Executor) ExecuteCommand(ctx context.Context, source sources.Source, command string, sourceID string) (*models.Record, error) {
	if err := safety.ValidateReadOnlyCommand(command); err != nil {
		return nil, fmt.Errorf("safety check failed: %w", err)
	}

	runner, ok := source.(capabilities.CommandRunner)
	if !ok {
		return nil, fmt.Errorf("source %q does not implement CommandRunner (CLI not supported)", sourceID)
	}

	rawOutput, err := runner.RunCommand(ctx, command)
	if err != nil {
		return nil, fmt.Errorf("command %q failed on source %q: %w", command, sourceID, err)
	}

	collectedAt := time.Now()
	var payload any
	var quality models.QualityMeta

	if e != nil && len(e.Transforms) > 0 {
		// spec.JQ == "" is a valid "format-only" annotation. Skip jq but still
		// record the format annotation for the fallback path below.
		if spec, ok := e.Transforms[OpRunCommand]; ok && spec.JQ != "" {
			transformed, terr := applyJQTransform(ctx, spec, rawOutput, spec.Format)
			if terr != nil {
				return nil, fmt.Errorf("jq transform failed: %w", terr)
			}
			payload = transformed
			quality = models.QualityMeta{MappingQuality: models.MappingDerived}
		}
	}

	if payload == nil {
		// No jq transform was applied. Parse the output according to the
		// format annotation (strict JSON or auto-detect), so callers receive
		// a structured payload without needing to provide a jq expression.
		format := ""
		if e != nil {
			if spec, ok := e.Transforms[OpRunCommand]; ok {
				format = spec.Format
			}
		}

		parsed, wasJSON, perr := parseOutput(rawOutput, format)
		if perr != nil {
			return nil, fmt.Errorf("failed to parse command output: %w", perr)
		}
		payload = parsed
		if wasJSON {
			quality = models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"raw JSON output; no jq transform configured"}}
		} else {
			quality = models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"raw CLI output; no parser or transform configured"}}
		}
	}

	// Include vendor/platform in metadata when the source exposes identity,
	// but do not require it — CommandRunner alone is sufficient.
	vendor, platform := "", ""
	if identity, ok := source.(capabilities.SourceIdentity); ok {
		vendor = identity.DeviceVendor()
		platform = identity.DevicePlatform()
	}

	return &models.Record{
		RecordType:    OpRunCommand,
		SchemaVersion: models.SchemaVersion,
		Source: models.SourceMeta{
			DeviceID:  sourceID,
			Vendor:    vendor,
			Platform:  platform,
			Transport: "cli",
		},
		Collection: models.CollectionMeta{
			Mode:        models.CollectionSnapshot,
			Protocol:    models.ProtocolCLI,
			CollectedAt: collectedAt,
		},
		Payload: payload,
		Quality: quality,
		Native: &models.NativeMeta{
			NativePath: command,
		},
	}, nil
}

// Execute runs the named operation against the given source, routing
// through protocol paths in preference order and returning a canonical Record.
func (e *Executor) Execute(ctx context.Context, source sources.Source, operationID string, sourceID string) (*models.Record, error) {
	return e.ExecuteWithOptions(ctx, source, operationID, sourceID, ExecuteOptions{})
}

// ExecuteWithOptions runs the named operation with runtime parameters and
// safety controls used for source-side filtering and limit enforcement.
func (e *Executor) ExecuteWithOptions(ctx context.Context, source sources.Source, operationID string, sourceID string, opts ExecuteOptions) (*models.Record, error) {
	// Resolve source identity for profile lookup.
	identity, ok := source.(capabilities.SourceIdentity)
	if !ok {
		return nil, fmt.Errorf("source %q does not expose vendor/platform identity", sourceID)
	}

	vendor := identity.DeviceVendor()
	platform := identity.DevicePlatform()
	version := identity.DeviceVersion()

	// Look up the profile.
	profile, ok := profiles.LookupForDevice(vendor, platform, version)
	if !ok {
		return nil, fmt.Errorf("no profile registered for %s.%s; source %q cannot execute operation %q", vendor, platform, sourceID, operationID)
	}

	// Look up the operation descriptor.
	op, ok := profile.Operations[operationID]
	if !ok {
		return nil, fmt.Errorf("profile %s.%s does not define operation %q", vendor, platform, operationID)
	}

	// Determine source capabilities.
	caps := getCapabilities(source)

	// Try protocol paths in preference order.
	var lastErr error
	for _, pp := range op.Paths {
		if !pp.CanExecute(caps) {
			continue
		}

		rendered, renderWarnings, err := renderProtocolPath(pp, opts)
		if err != nil {
			lastErr = err
			continue
		}
		record, err := executePath(ctx, e, source, rendered, operationID, sourceID, vendor, platform, version, opts)
		if err != nil {
			lastErr = err
			continue // try next protocol
		}
		for _, warning := range renderWarnings {
			appendQualityWarning(&record.Quality, warning)
		}
		return record, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all protocol paths exhausted for operation %q on source %q: %w", operationID, sourceID, lastErr)
	}
	return nil, fmt.Errorf("no compatible protocol path found for operation %q on source %q (caps: %+v)", operationID, sourceID, caps)
}

// getCapabilities extracts SourceCapabilities from a source, defaulting
// to no capabilities if the source doesn't implement CapabilityProvider.
func getCapabilities(source sources.Source) capabilities.SourceCapabilities {
	if cp, ok := source.(capabilities.CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return capabilities.SourceCapabilities{}
}

// executePath runs a single protocol path and returns a canonical Record.
func executePath(ctx context.Context, e *Executor, source sources.Source, pp profiles.ProtocolPath, operationID, sourceID, vendor, platform, version string, opts ExecuteOptions) (*models.Record, error) {
	collectedAt := time.Now()

	switch pp.Protocol {
	case profiles.ProtocolGnmiOpenConfig, profiles.ProtocolGnmiNative:
		return executeGnmi(ctx, e, source, pp, operationID, sourceID, vendor, platform, version, collectedAt, opts)
	case profiles.ProtocolCLI:
		return executeCLI(ctx, e, source, pp, operationID, sourceID, vendor, platform, collectedAt, opts)
	case profiles.ProtocolNetconfOpenConfig, profiles.ProtocolNetconfNative:
		return executeNetconf(ctx, e, source, pp, operationID, sourceID, vendor, platform, version, collectedAt, opts)
	default:
		return nil, fmt.Errorf("unsupported protocol %q", pp.Protocol)
	}
}

// executeGnmi runs a gNMI Get RPC and normalizes the response.
func executeGnmi(ctx context.Context, e *Executor, source sources.Source, pp profiles.ProtocolPath, operationID, sourceID, vendor, platform, version string, collectedAt time.Time, opts ExecuteOptions) (*models.Record, error) {
	querier, ok := source.(capabilities.GnmiQuerier)
	if !ok {
		return nil, fmt.Errorf("source %q does not implement GnmiQuerier", sourceID)
	}

	encoding := "JSON_IETF"
	result, err := querier.GnmiGet(ctx, pp.Paths, encoding)
	if err != nil {
		return nil, fmt.Errorf("gNMI Get failed on source %q: %w", sourceID, err)
	}

	protocol := models.ProtocolGnmiOpenConfig
	if pp.Protocol == profiles.ProtocolGnmiNative {
		protocol = models.ProtocolGnmiNative
	}

	payload, quality, err := normalizeGnmiResponse(result, operationID, vendor, platform, version, e.SchemaStore)
	if err != nil {
		return nil, err
	}

	// Apply user-defined jq transform to the normalized payload when configured.
	if e != nil && len(e.Transforms) > 0 {
		if spec, ok := e.Transforms[operationID]; ok && spec.JQ != "" {
			transformed, terr := applyPayloadTransform(ctx, spec, payload)
			if terr != nil {
				return nil, fmt.Errorf("jq transform failed for gNMI operation %q: %w", operationID, terr)
			}
			payload = transformed
			quality = models.QualityMeta{MappingQuality: models.MappingDerived}
		}
	}
	payload, quality = enforceLimits(payload, quality, pp.Limits, opts.LimitOverride)

	return &models.Record{
		RecordType:    operationID,
		SchemaVersion: models.SchemaVersion,
		Source: models.SourceMeta{
			DeviceID:  sourceID,
			Vendor:    vendor,
			Platform:  platform,
			Transport: "gnmi",
		},
		Collection: models.CollectionMeta{
			Mode:        models.CollectionSnapshot,
			Protocol:    protocol,
			CollectedAt: collectedAt,
		},
		Payload: payload,
		Quality: quality,
		Native:  enrichNativeMeta(e, vendor, platform, version, pp.Paths, ""),
	}, nil
}

// enrichNativeMeta populates NativeMeta with schema model information
// when a SchemaStore is available. Falls back to basic path info.
func enrichNativeMeta(e *Executor, vendor, platform, version string, paths []string, filter string) *models.NativeMeta {
	nativePath := filter
	if nativePath == "" && len(paths) > 0 {
		nativePath = fmt.Sprintf("%v", paths)
	}

	meta := &models.NativeMeta{
		NativePath: nativePath,
	}

	if e == nil || e.SchemaStore == nil {
		return meta
	}

	bundle, ok := e.SchemaStore.LookupBestMatch(vendor, platform, version)
	if !ok {
		return meta
	}

	// Try to resolve the first path to get model metadata.
	lookupPath := filter
	if lookupPath == "" && len(paths) > 0 {
		lookupPath = paths[0]
	}
	if lookupPath == "" {
		return meta
	}

	resolved, err := bundle.ResolvePath(lookupPath)
	if err != nil {
		return meta
	}

	meta.ModelName = resolved.ModuleName
	meta.ModelRevision = resolved.ModuleRevision
	return meta
}

// executeCLI runs a CLI command and normalizes the response.
func executeCLI(ctx context.Context, e *Executor, source sources.Source, pp profiles.ProtocolPath, operationID, sourceID, vendor, platform string, collectedAt time.Time, opts ExecuteOptions) (*models.Record, error) {
	runner, ok := source.(capabilities.CommandRunner)
	if !ok {
		return nil, fmt.Errorf("source %q does not implement CommandRunner", sourceID)
	}

	// Build the actual command, appending the format argument when declared.
	actualCmd := pp.Command
	if pp.FormatArg != "" {
		actualCmd = pp.Command + " " + pp.FormatArg
	}

	if err := safety.ValidateReadOnlyCommand(actualCmd); err != nil {
		return nil, fmt.Errorf("safety check failed: %w", err)
	}

	rawOutput, err := runner.RunCommand(ctx, actualCmd)
	if err != nil {
		return nil, fmt.Errorf("CLI command %q failed on source %q: %w", actualCmd, sourceID, err)
	}

	format := pp.OutputFormat()

	var payload any
	var quality models.QualityMeta

	// If a user-defined jq transform is registered for this operation, apply
	// it instead of the built-in parser registry — but only when the transform's
	// declared Format matches the current path's format. An empty Format on the
	// spec means "any format". This prevents a json-targeted transform from
	// accidentally firing on the text fallback path (which would always fail
	// because the JSON structure won't be present in the text-wrapped input).
	if e != nil && len(e.Transforms) > 0 {
		// spec.JQ == "" skipped: it is a valid format-only hint used by callers
		// that want to set the input shape for runtime jq without a static expr.
		if spec, ok := e.Transforms[operationID]; ok && spec.JQ != "" {
			if spec.Format == "" || spec.Format == format {
				transformed, terr := applyJQTransform(ctx, spec, rawOutput, format)
				if terr != nil {
					return nil, fmt.Errorf("jq transform failed for operation %q: %w", operationID, terr)
				}
				payload = transformed
				quality = models.QualityMeta{MappingQuality: models.MappingDerived}
			}
		}
	}

	if payload == nil {
		// No transform — dispatch to the built-in parser registry.
		var perr error
		payload, quality, perr = parsers.Dispatch(parsers.ParserKey{
			Vendor:    vendor,
			Platform:  platform,
			Operation: operationID,
			Format:    format,
		}, rawOutput)
		if perr != nil {
			return nil, fmt.Errorf("CLI output parse failed for operation %q: %w", operationID, perr)
		}
	}
	payload, quality = enforceLimits(payload, quality, pp.Limits, opts.LimitOverride)

	return &models.Record{
		RecordType:    operationID,
		SchemaVersion: models.SchemaVersion,
		Source: models.SourceMeta{
			DeviceID:  sourceID,
			Vendor:    vendor,
			Platform:  platform,
			Transport: "cli",
		},
		Collection: models.CollectionMeta{
			Mode:        models.CollectionSnapshot,
			Protocol:    models.ProtocolCLI,
			CollectedAt: collectedAt,
		},
		Payload: payload,
		Quality: quality,
	}, nil
}

// normalizeGnmiResponse converts gNMI Get results to canonical payload.
// When a SchemaStore is available, it tries schema-driven canonical mapping
// first and falls back to the hardcoded per-vendor parsers on failure.
func normalizeGnmiResponse(result *capabilities.GnmiGetResult, operationID, vendor, platform, version string, schemaStore *schemas.SchemaStore) (any, models.QualityMeta, error) {
	if result == nil || len(result.Notifications) == 0 {
		return nil, models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"empty gNMI response"}}, fmt.Errorf("empty gNMI response")
	}

	// Merge all notification values into a single JSON object for parsing.
	merged := make(map[string]json.RawMessage)
	for _, n := range result.Notifications {
		merged[n.Path] = n.Value
	}

	// Try schema-driven canonical mapping first.
	if payload, quality, ok := trySchemaMapGnmi(merged, operationID, schemaStore, vendor, platform, version); ok {
		return payload, quality, nil
	}

	// Fall back to hardcoded per-vendor parsers.
	switch operationID {
	case profiles.OpGetInterfaces:
		return parseGnmiInterfaces(merged, vendor, platform)
	case profiles.OpGetSystemVersion:
		return parseGnmiSystemVersion(merged, vendor, platform)
	default:
		// Return raw merged data for unknown operations.
		return merged, models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"no canonical parser for operation"}}, nil
	}
}

// trySchemaMapGnmi attempts schema-driven mapping of merged gNMI notification
// values. Returns (payload, quality, true) on success. The third return is
// false when the mapper is unavailable or produces empty results, signalling
// the caller to fall back to hardcoded parsers.
func trySchemaMapGnmi(merged map[string]json.RawMessage, operationID string, schemaStore *schemas.SchemaStore, vendor, platform, version string) (any, models.QualityMeta, bool) {
	var bundle *schemas.SchemaBundle
	if schemaStore != nil {
		b, ok := schemaStore.LookupBestMatch(vendor, platform, version)
		if ok {
			bundle = b
		}
	}

	mapper, err := schemas.NewSchemaMapper(bundle, operationID)
	if err != nil {
		return nil, models.QualityMeta{}, false
	}

	// Unmarshal all notification values into generic structures.
	var allData []any
	for _, raw := range merged {
		var data any
		if uerr := json.Unmarshal(raw, &data); uerr == nil {
			allData = append(allData, data)
		}
	}
	if len(allData) == 0 {
		return nil, models.QualityMeta{}, false
	}

	// For a single notification, pass directly. For multiple, merge into
	// one map so the mapper sees all fields (e.g. system/information +
	// system/name from separate gNMI paths).
	var input any
	if len(allData) == 1 {
		input = allData[0]
	} else {
		combined := make(map[string]any)
		for _, d := range allData {
			if m, ok := d.(map[string]any); ok {
				for k, v := range m {
					if _, exists := combined[k]; !exists {
						combined[k] = v
					}
				}
			}
		}
		input = combined
	}

	payload, quality, merr := mapper.MapJSON(input)
	if merr != nil {
		return nil, models.QualityMeta{}, false
	}

	// Reject empty results so the hardcoded parsers get a chance.
	if isEmptyMappingResult(payload) {
		return nil, models.QualityMeta{}, false
	}

	return payload, quality, true
}

// isEmptyMappingResult returns true when the mapper produced no meaningful data.
func isEmptyMappingResult(payload any) bool {
	switch v := payload.(type) {
	case []models.InterfaceState:
		return len(v) == 0
	case models.SystemVersion:
		return v.Hostname == "" && v.SoftwareVersion == ""
	default:
		return false
	}
}

// parseGnmiInterfaces parses gNMI interface data into canonical InterfaceState.
func parseGnmiInterfaces(data map[string]json.RawMessage, _, _ string) (any, models.QualityMeta, error) {
	var interfaces []models.InterfaceState

	for _, v := range data {
		var raw any
		if err := json.Unmarshal(v, &raw); err != nil {
			continue
		}
		// gNMI responses may return a top-level wrapper object keyed by the
		// YANG module prefix (e.g. "srl_nokia-interfaces:interface",
		// "openconfig-interfaces:interface") or the bare "interface" key.
		// Unwrap those to reach the interface array/object.
		if m, ok := raw.(map[string]any); ok {
			raw = unwrapInterfaceList(m)
		}
		switch typed := raw.(type) {
		case []any:
			for _, item := range typed {
				if iface, ok := parseGnmiInterface(item); ok {
					interfaces = append(interfaces, iface)
				}
			}
		case map[string]any:
			if iface, ok := parseGnmiInterface(typed); ok {
				interfaces = append(interfaces, iface)
			}
		}
	}

	quality := models.QualityMeta{MappingQuality: models.MappingExact}
	if len(interfaces) == 0 {
		quality = models.QualityMeta{MappingQuality: models.MappingPartial, Warnings: []string{"no interfaces parsed from gNMI response"}}
	}
	return interfaces, quality, nil
}

// unwrapInterfaceList extracts the interface list from a gNMI wrapper object.
// gNMI may return {"srl_nokia-interfaces:interface": [...]} or
// {"openconfig-interfaces:interface": [...]} or {"interface": [...]}.
// If no known wrapper key is found, the original map is returned as-is.
func unwrapInterfaceList(m map[string]any) any {
	for k, v := range m {
		if k == "interface" || strings.HasSuffix(k, ":interface") {
			return v
		}
	}
	return m
}

// parseGnmiInterface extracts canonical InterfaceState from a gNMI JSON object.
// Handles both OpenConfig (admin-status/oper-status) and SR Linux native
// YANG (admin-state/oper-state) field names.
func parseGnmiInterface(data any) (models.InterfaceState, bool) {
	m, ok := data.(map[string]any)
	if !ok {
		return models.InterfaceState{}, false
	}

	iface := models.InterfaceState{}

	if name, ok := m["name"].(string); ok {
		iface.Name = name
	}

	// Navigate to state container if present (OpenConfig pattern).
	state := m
	if s, ok := m["state"].(map[string]any); ok {
		state = s
	}

	if v, ok := state["type"].(string); ok {
		iface.Type = v
	}

	// OpenConfig: admin-status / oper-status
	// SR Linux native: admin-state / oper-state
	if v, ok := state["admin-status"].(string); ok {
		iface.AdminStatus = normalizeOCStatus(v)
	} else if v, ok := state["admin-state"].(string); ok {
		iface.AdminStatus = normalizeSRLAdminState(v)
	}
	if v, ok := state["oper-status"].(string); ok {
		iface.OperStatus = normalizeOCStatus(v)
	} else if v, ok := state["oper-state"].(string); ok {
		iface.OperStatus = normalizeOCStatus(v)
	}
	if v, ok := state["description"].(string); ok {
		iface.Description = v
	}
	if v, ok := state["mtu"].(float64); ok {
		iface.MTU = int(v)
	} else if v, ok := m["mtu"].(float64); ok {
		iface.MTU = int(v)
	}

	return iface, iface.Name != ""
}

// parseGnmiSystemVersion parses gNMI system data into canonical SystemVersion.
func parseGnmiSystemVersion(data map[string]json.RawMessage, _, _ string) (any, models.QualityMeta, error) {
	sv := models.SystemVersion{}
	quality := models.QualityMeta{MappingQuality: models.MappingExact}

	for _, v := range data {
		var raw map[string]any
		if err := json.Unmarshal(v, &raw); err != nil {
			continue
		}
		// Unwrap a vendor-namespaced system wrapper if present, e.g.
		// {"srl_nokia-system:system": {"information": {...}}}
		for k, val := range raw {
			if k == "system" || strings.HasSuffix(k, ":system") {
				if nested, ok := val.(map[string]any); ok {
					raw = nested
				}
				break
			}
		}
		// OpenConfig system/state fields.
		if sv.Hostname == "" {
			if hostname, ok := raw["hostname"].(string); ok {
				sv.Hostname = hostname
			}
		}
		if sv.SoftwareVersion == "" {
			if ver, ok := raw["software-version"].(string); ok {
				sv.SoftwareVersion = ver
			}
		}
		if sv.ChassisType == "" {
			if desc, ok := raw["hardware-description"].(string); ok {
				sv.ChassisType = desc
			}
		}
		// SR Linux native: path /srl_nokia-system:system/information returns
		// the information container directly as a flat object.
		if sv.SoftwareVersion == "" {
			if ver, ok := raw["version"].(string); ok {
				sv.SoftwareVersion = ver
			}
		}
		if sv.ChassisType == "" {
			if desc, ok := raw["description"].(string); ok {
				sv.ChassisType = desc
			}
		}
		// SR Linux native: path /srl_nokia-system:system/name returns the name
		// container with host-name (with or without a module namespace prefix).
		if sv.Hostname == "" {
			for k, val := range raw {
				if k == "host-name" || strings.HasSuffix(k, ":host-name") {
					if s, ok := val.(string); ok {
						sv.Hostname = s
					}
					break
				}
			}
		}
		// SR Linux native: information as a nested sub-container (when the
		// full system object is returned instead of the information sub-path).
		if sv.SoftwareVersion == "" {
			if info, ok := raw["information"].(map[string]any); ok {
				if ver, ok := info["version"].(string); ok {
					sv.SoftwareVersion = ver
				}
				if sv.ChassisType == "" {
					if desc, ok := info["description"].(string); ok {
						sv.ChassisType = desc
					}
				}
			}
		}
	}

	if sv.Hostname == "" && sv.SoftwareVersion == "" {
		quality.Warnings = append(quality.Warnings, "minimal system data parsed from gNMI response")
		quality.MappingQuality = models.MappingPartial
	}
	return sv, quality, nil
}

// normalizeOCStatus converts OpenConfig enum-style status to canonical.
func normalizeOCStatus(s string) string {
	switch s {
	case "UP", "up":
		return "UP"
	case "DOWN", "down":
		return "DOWN"
	default:
		return s
	}
}
