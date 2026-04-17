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

package networkshow_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/tools"
	netshow "github.com/adrien19/noc-foundry/internal/tools/network/show"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

// ---------------------------------------------------------------------------
// Mock helpers
// ---------------------------------------------------------------------------

// mockSource implements CommandRunner + SourceIdentity + CapabilityProvider.
type mockSource struct {
	output map[string]string // command → output
	err    error
}

func (m *mockSource) RunCommand(_ context.Context, cmd string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	if out, ok := m.output[cmd]; ok {
		return out, nil
	}
	return "", fmt.Errorf("command not found in mock: %q", cmd)
}

func (m *mockSource) SourceType() string             { return "ssh" }
func (m *mockSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockSource) DeviceVendor() string           { return "nokia" }
func (m *mockSource) DevicePlatform() string         { return "srlinux" }
func (m *mockSource) DeviceVersion() string          { return "" }
func (m *mockSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

// incompatibleSource has no CommandRunner.
type incompatibleSource struct{}

func (s *incompatibleSource) SourceType() string             { return "incompatible" }
func (s *incompatibleSource) ToConfig() sources.SourceConfig { return nil }

// mockSourceProvider implements tools.SourceProvider.
type mockSourceProvider struct {
	sources      map[string]sources.Source
	labelSources map[string]sources.Source
	labelErr     error
}

func (m *mockSourceProvider) GetSource(name string) (sources.Source, bool) {
	s, ok := m.sources[name]
	return s, ok
}

func (m *mockSourceProvider) GetSourcesByLabels(_ context.Context, _ map[string]string) (map[string]sources.Source, error) {
	if m.labelErr != nil {
		return nil, m.labelErr
	}
	return m.labelSources, nil
}

func (m *mockSourceProvider) GetDevicePoolLabels() map[string]map[string]string {
	return nil
}

// ---------------------------------------------------------------------------
// YAML parse tests
// ---------------------------------------------------------------------------

func TestParseFromYamlNetworkShow(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want server.ToolConfigs
	}{
		{
			desc: "basic example",
			in: `
			kind: tools
			name: run_show
			type: network-show
			source: my-device
			`,
			want: map[string]tools.ToolConfig{
				"run_show": netshow.Config{
					Name:         "run_show",
					Type:         "network-show",
					Source:       "my-device",
					AuthRequired: []string{},
				},
			},
		},
		{
			desc: "with description and auth",
			in: `
			kind: tools
			name: run_show
			type: network-show
			source: my-device
			description: Run any read-only show command
			authRequired:
				- network-admin
			`,
			want: map[string]tools.ToolConfig{
				"run_show": netshow.Config{
					Name:         "run_show",
					Type:         "network-show",
					Source:       "my-device",
					Description:  "Run any read-only show command",
					AuthRequired: []string{"network-admin"},
				},
			},
		},
		{
			desc: "with transform",
			in: `
			kind: tools
			name: run_show
			type: network-show
			source: my-device
			transforms:
				run_command:
					format: json
					jq: '.some | .path'
			`,
			want: map[string]tools.ToolConfig{
				"run_show": netshow.Config{
					Name:   "run_show",
					Type:   "network-show",
					Source: "my-device",
					Transforms: map[string]query.TransformSpec{
						"run_command": {Format: "json", JQ: ".some | .path"},
					},
					AuthRequired: []string{},
				},
			},
		},
		{
			desc: "predefined command with parameters",
			in: `
			kind: tools
			name: show_iface
			type: network-show
			source: my-device
			description: Show interface details
			command: "show interface {interface} detail"
			parameters:
				- name: interface
				  type: string
				  description: Interface name (e.g. "ethernet-1/1")
			`,
			want: map[string]tools.ToolConfig{
				"show_iface": netshow.Config{
					Name:        "show_iface",
					Type:        "network-show",
					Source:      "my-device",
					Description: "Show interface details",
					Command:     `show interface {interface} detail`,
					ExtraParams: []netshow.CommandParam{
						{Name: "interface", Type: "string", Description: `Interface name (e.g. "ethernet-1/1")`},
					},
					AuthRequired: []string{},
				},
			},
		},
		{
			desc: "predefined command with transform and format hint",
			in: `
			kind: tools
			name: show_iface
			type: network-show
			source: my-device
			command: "show interface {interface} detail | as json"
			parameters:
				- name: interface
				  type: string
				  description: Interface name
			transforms:
				run_command:
					format: json
					jq: '.name'
			`,
			want: map[string]tools.ToolConfig{
				"show_iface": netshow.Config{
					Name:    "show_iface",
					Type:    "network-show",
					Source:  "my-device",
					Command: `show interface {interface} detail | as json`,
					ExtraParams: []netshow.CommandParam{
						{Name: "interface", Type: "string", Description: "Interface name"},
					},
					Transforms: map[string]query.TransformSpec{
						"run_command": {Format: "json", JQ: ".name"},
					},
					AuthRequired: []string{},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, got, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(tools.ToolAnnotations{})); diff != "" {
				t.Fatalf("incorrect parse (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFailParseFromYamlNetworkShow(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
	}{
		{
			desc: "unknown field",
			in: `
			kind: tools
			name: run_show
			type: network-show
			source: my-device
			unknownField: value
			`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err == nil {
				t.Fatal("expected error but got nil")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Initialize tests
// ---------------------------------------------------------------------------

func TestInitializeMissingSourceAndSelector(t *testing.T) {
	cfg := netshow.Config{Name: "run_show", Type: "network-show"}
	_, err := cfg.Initialize(map[string]sources.Source{})
	if err == nil {
		t.Fatal("expected error when neither source nor sourceSelector is set")
	}
}

func TestInitializeBothSourceAndSelector(t *testing.T) {
	cfg := netshow.Config{
		Name:           "run_show",
		Type:           "network-show",
		Source:         "my-device",
		SourceSelector: &netshow.SourceSelector{MatchLabels: map[string]string{"role": "spine"}},
	}
	_, err := cfg.Initialize(map[string]sources.Source{})
	if err == nil {
		t.Fatal("expected error when both source and sourceSelector are set")
	}
}

func TestInitializeIncompatibleSource(t *testing.T) {
	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "bad"}
	_, err := cfg.Initialize(map[string]sources.Source{"bad": &incompatibleSource{}})
	if err == nil {
		t.Fatal("expected error for source without CommandRunner")
	}
}

func TestInitializeMissingSourceAccepted(t *testing.T) {
	// Sources resolved lazily at invocation time (device pool) are not in the
	// eager source map, so Initialize must accept them without error.
	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "pool-source"}
	_, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Invoke tests (single source)
// ---------------------------------------------------------------------------

func TestInvokeShowCommand_RawOutput(t *testing.T) {
	rawOutput := "spine-1  SRLinux 24.3.1  uptime 3d 4h"

	ms := &mockSource{output: map[string]string{
		"show version": rawOutput,
	}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "command", Value: "show version"}}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record, ok := result.(*models.Record)
	if !ok {
		t.Fatalf("result type = %T, want *models.Record", result)
	}
	if record.RecordType != query.OpRunCommand {
		t.Errorf("RecordType = %q, want %q", record.RecordType, query.OpRunCommand)
	}
	if record.Source.Transport != "cli" {
		t.Errorf("Transport = %q, want %q", record.Source.Transport, "cli")
	}
	if record.Payload != rawOutput {
		t.Errorf("Payload = %v, want %q", record.Payload, rawOutput)
	}
	if record.Quality.MappingQuality != models.MappingPartial {
		t.Errorf("MappingQuality = %q, want %q", record.Quality.MappingQuality, models.MappingPartial)
	}
	if record.Native == nil || record.Native.NativePath != "show version" {
		t.Errorf("Native.NativePath = %v, want %q", record.Native, "show version")
	}
}

func TestInvokeShowCommand_WithJQTransform(t *testing.T) {
	jsonOutput := `{"hostname":"spine-1","software-version":"SRLinux 24.3.1"}`

	ms := &mockSource{output: map[string]string{
		"show version | json": jsonOutput,
	}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{
		Name:   "run_show",
		Type:   "network-show",
		Source: "my-device",
		Transforms: map[string]query.TransformSpec{
			query.OpRunCommand: {Format: "json", JQ: `.hostname`},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "command", Value: "show version | json"}}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Payload != "spine-1" {
		t.Errorf("Payload = %v, want %q", record.Payload, "spine-1")
	}
	if record.Quality.MappingQuality != models.MappingDerived {
		t.Errorf("MappingQuality = %q, want %q", record.Quality.MappingQuality, models.MappingDerived)
	}
}

func TestInvokeMissingCommandParam(t *testing.T) {
	ms := &mockSource{output: map[string]string{}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// No "command" param provided.
	_, toolErr := tool.Invoke(context.Background(), provider, parameters.ParamValues{}, "")
	if toolErr == nil {
		t.Fatal("expected error when command param is missing")
	}
}

func TestInvokeDangerousCommandRejected(t *testing.T) {
	ms := &mockSource{output: map[string]string{}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "command", Value: "configure router interface"}}
	_, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr == nil {
		t.Fatal("expected error for dangerous command")
	}
}

func TestInvokeSourceError(t *testing.T) {
	ms := &mockSource{err: fmt.Errorf("SSH connection lost")}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "command", Value: "show version"}}
	_, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr == nil {
		t.Fatal("expected error when source returns error")
	}
}

func TestInvokeSourceNotFound(t *testing.T) {
	provider := &mockSourceProvider{sources: map[string]sources.Source{}}

	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "missing"}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "command", Value: "show version"}}
	_, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr == nil {
		t.Fatal("expected error when source is not found at invocation")
	}
}

// ---------------------------------------------------------------------------
// Manifest / MCP tests
// ---------------------------------------------------------------------------

func TestMcpManifestHasCommandParam(t *testing.T) {
	ms := &mockSource{output: map[string]string{}}
	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	mcp := tool.McpManifest()
	if mcp.Name != "run_show" {
		t.Errorf("McpManifest.Name = %q, want %q", mcp.Name, "run_show")
	}

	if _, ok := mcp.InputSchema.Properties["command"]; !ok {
		t.Error("expected 'command' parameter in MCP manifest")
	}
	// jq is always available
	if _, ok := mcp.InputSchema.Properties["jq"]; !ok {
		t.Error("expected 'jq' parameter in MCP manifest")
	}
}

func TestSelectorManifestHasDeviceAndCommandParams(t *testing.T) {
	cfg := netshow.Config{
		Name:           "run_show",
		Type:           "network-show",
		SourceSelector: &netshow.SourceSelector{MatchLabels: map[string]string{"role": "spine"}},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := tool.GetParameters()
	names := make(map[string]bool)
	for _, p := range params {
		names[p.GetName()] = true
	}
	if !names["command"] {
		t.Error("expected 'command' parameter for selector tool")
	}
	if !names["device"] {
		t.Error("expected 'device' parameter for selector tool")
	}
}

// ---------------------------------------------------------------------------
// Runtime jq parameter tests
// ---------------------------------------------------------------------------

func TestInvokeShowCommand_RuntimeJQ(t *testing.T) {
	// Ad-hoc mode: both command and jq supplied at runtime.
	// Uses explicit format: json hint (strict mode).
	jsonOutput := `{"hostname":"spine-1","version":"24.3.1"}`
	ms := &mockSource{output: map[string]string{
		"show version | json": jsonOutput,
	}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	// Format hint: format: json, no static jq expression.
	cfg := netshow.Config{
		Name:   "run_show",
		Type:   "network-show",
		Source: "my-device",
		Transforms: map[string]query.TransformSpec{
			query.OpRunCommand: {Format: "json"},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{
		{Name: "command", Value: "show version | json"},
		{Name: "jq", Value: `.hostname`},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Payload != "spine-1" {
		t.Errorf("Payload = %v, want %q", record.Payload, "spine-1")
	}
	if record.Quality.MappingQuality != models.MappingDerived {
		t.Errorf("MappingQuality = %q, want %q", record.Quality.MappingQuality, models.MappingDerived)
	}
}

// TestInvokeShowCommand_RuntimeJQ_AutoDetect verifies that no format hint is
// required when the command produces JSON output. The auto-detect logic in
// applyJQTransform parses the JSON and the jq expression receives the object
// directly — no {"text": raw} wrapper and no `format: json` annotation needed.
func TestInvokeShowCommand_RuntimeJQ_AutoDetect(t *testing.T) {
	jsonOutput := `{"hostname":"spine-1","version":"24.3.1"}`
	ms := &mockSource{output: map[string]string{
		"show version | as json": jsonOutput,
	}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	// No transforms block at all — auto-detect kicks in.
	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{
		{Name: "command", Value: "show version | as json"},
		{Name: "jq", Value: `.hostname`},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Payload != "spine-1" {
		t.Errorf("Payload = %v, want %q (auto-detected JSON, no format hint required)", record.Payload, "spine-1")
	}
}

func TestInvokeShowCommand_RuntimeJQ_OverridesStaticTransform(t *testing.T) {
	// Runtime jq should override the static transform.
	jsonOutput := `{"hostname":"spine-1","version":"24.3.1"}`
	ms := &mockSource{output: map[string]string{
		"show version | json": jsonOutput,
	}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{
		Name:   "run_show",
		Type:   "network-show",
		Source: "my-device",
		Transforms: map[string]query.TransformSpec{
			// Static transform returns "version" field.
			query.OpRunCommand: {Format: "json", JQ: ".version"},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// Runtime jq overrides: returns "hostname" instead.
	params := parameters.ParamValues{
		{Name: "command", Value: "show version | json"},
		{Name: "jq", Value: `.hostname`},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Payload != "spine-1" {
		t.Errorf("Payload = %v (runtime jq should return hostname, not version)", record.Payload)
	}
}

func TestInvokeShowCommand_NoJQ_UsesRawOutput(t *testing.T) {
	// Without jq and without a static transform, payload is the raw string.
	rawOutput := "spine-1  SRLinux 24.3.1"
	ms := &mockSource{output: map[string]string{"show version": rawOutput}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{Name: "run_show", Type: "network-show", Source: "my-device"}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "command", Value: "show version"}}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Payload != rawOutput {
		t.Errorf("Payload = %v, want raw string %q", record.Payload, rawOutput)
	}
}

// ---------------------------------------------------------------------------
// Predefined-command mode tests
// ---------------------------------------------------------------------------

func TestInitialize_PredefinedCommand_ValidTemplate(t *testing.T) {
	cfg := netshow.Config{
		Name:    "show_iface",
		Type:    "network-show",
		Source:  "my-device",
		Command: "show interface {interface} detail",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
		},
	}
	_, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitialize_PredefinedCommand_UndeclaredPlaceholder(t *testing.T) {
	// Template references {vrf} but no param named "vrf" is declared.
	cfg := netshow.Config{
		Name:    "show_iface",
		Type:    "network-show",
		Source:  "my-device",
		Command: "show interface {interface} vrf {vrf} detail",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
			// {vrf} is NOT declared
		},
	}
	_, err := cfg.Initialize(map[string]sources.Source{})
	if err == nil {
		t.Fatal("expected error for undeclared placeholder {vrf}")
	}
}

func TestInitialize_ExtraParamsWithoutCommand(t *testing.T) {
	// ExtraParams only makes sense with Command.
	cfg := netshow.Config{
		Name:   "bad",
		Type:   "network-show",
		Source: "my-device",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
		},
	}
	_, err := cfg.Initialize(map[string]sources.Source{})
	if err == nil {
		t.Fatal("expected error when ExtraParams set without Command")
	}
}

func TestInvoke_PredefinedCommand_ExpandsTemplate(t *testing.T) {
	// Template: "show interface {interface} detail"
	// Invoked with interface=ethernet-1/1 → expanded command sent to device.
	expandedCmd := "show interface ethernet-1/1 detail"
	ms := &mockSource{output: map[string]string{
		expandedCmd: "ethernet-1/1 is up",
	}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{
		Name:    "show_iface",
		Type:    "network-show",
		Source:  "my-device",
		Command: "show interface {interface} detail",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{{Name: "interface", Value: "ethernet-1/1"}}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Native == nil || record.Native.NativePath != expandedCmd {
		t.Errorf("NativePath = %v, want %q", record.Native, expandedCmd)
	}
}

func TestInvoke_PredefinedCommand_NoCommandParamExposed(t *testing.T) {
	// In predefined mode the "command" runtime parameter must NOT be present;
	// only the declared ExtraParams + "jq" should appear.
	cfg := netshow.Config{
		Name:    "show_iface",
		Type:    "network-show",
		Source:  "my-device",
		Command: "show interface {interface} detail",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	names := make(map[string]bool)
	for _, p := range tool.GetParameters() {
		names[p.GetName()] = true
	}
	if names["command"] {
		t.Error("'command' parameter must NOT be exposed in predefined-command mode")
	}
	if !names["interface"] {
		t.Error("expected 'interface' parameter in predefined-command mode")
	}
	if !names["jq"] {
		t.Error("expected 'jq' parameter in predefined-command mode")
	}
}

func TestInvoke_PredefinedCommand_MissingRequiredParam(t *testing.T) {
	ms := &mockSource{output: map[string]string{}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{
		Name:    "show_iface",
		Type:    "network-show",
		Source:  "my-device",
		Command: "show interface {interface} detail",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	// No "interface" param provided.
	_, toolErr := tool.Invoke(context.Background(), provider, parameters.ParamValues{}, "")
	if toolErr == nil {
		t.Fatal("expected error when required ExtraParam is missing")
	}
}

func TestInvoke_PredefinedCommand_WithRuntimeJQ(t *testing.T) {
	expandedCmd := "show interface ethernet-1/1 detail | as json"
	jsonOutput := `{"name":"ethernet-1/1","oper-state":"up"}`
	ms := &mockSource{output: map[string]string{expandedCmd: jsonOutput}}
	provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

	cfg := netshow.Config{
		Name:    "show_iface",
		Type:    "network-show",
		Source:  "my-device",
		Command: "show interface {interface} detail | as json",
		ExtraParams: []netshow.CommandParam{
			{Name: "interface", Description: "Interface name"},
		},
		Transforms: map[string]query.TransformSpec{
			// Format hint: JSON — static jq is empty.
			query.OpRunCommand: {Format: "json"},
		},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
	if err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	params := parameters.ParamValues{
		{Name: "interface", Value: "ethernet-1/1"},
		{Name: "jq", Value: `."oper-state"`},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("Invoke: %v", toolErr)
	}

	record := result.(*models.Record)
	if record.Payload != "up" {
		t.Errorf("Payload = %v, want %q", record.Payload, "up")
	}
}

// ---------------------------------------------------------------------------
// Security tests: parameter value sanitization
// ---------------------------------------------------------------------------

func TestInvoke_PredefinedCommand_SafeCharactersAllowed(t *testing.T) {
	// All of these values should be accepted.
	safeValues := []string{
		"ethernet-1/1",
		"lo0",
		"192.168.1.0/24",
		"2001:db8::/32",
		"Base",
		"my-vrf-1",
		"router@edge",
	}

	for _, val := range safeValues {
		t.Run(val, func(t *testing.T) {
			expandedCmd := "show interface " + val + " detail"
			ms := &mockSource{output: map[string]string{expandedCmd: "output"}}
			provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

			cfg := netshow.Config{
				Name:    "show_iface",
				Type:    "network-show",
				Source:  "my-device",
				Command: "show interface {interface} detail",
				ExtraParams: []netshow.CommandParam{
					{Name: "interface", Description: "Interface name"},
				},
			}
			tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
			if err != nil {
				t.Fatalf("Initialize: %v", err)
			}

			params := parameters.ParamValues{{Name: "interface", Value: val}}
			_, toolErr := tool.Invoke(context.Background(), provider, params, "")
			if toolErr != nil {
				t.Errorf("expected success for safe value %q, got: %v", val, toolErr)
			}
		})
	}
}

func TestInvoke_PredefinedCommand_UnsafeCharactersRejected(t *testing.T) {
	// Each of these values contains characters that could be used to inject
	// additional CLI commands on the target device.
	unsafeValues := []string{
		"eth0; configure",
		"eth0 | show",
		"eth0 && delete",
		"eth0\nshow version",
		"eth0`whoami`",
		"eth0$(reboot)",
		"eth0 > /dev/null",
		"eth0!",
		"eth0#comment",
	}

	for _, val := range unsafeValues {
		t.Run(val, func(t *testing.T) {
			ms := &mockSource{output: map[string]string{}}
			provider := &mockSourceProvider{sources: map[string]sources.Source{"my-device": ms}}

			cfg := netshow.Config{
				Name:    "show_iface",
				Type:    "network-show",
				Source:  "my-device",
				Command: "show interface {interface} detail",
				ExtraParams: []netshow.CommandParam{
					{Name: "interface", Description: "Interface name"},
				},
			}
			tool, err := cfg.Initialize(map[string]sources.Source{"my-device": ms})
			if err != nil {
				t.Fatalf("Initialize: %v", err)
			}

			params := parameters.ParamValues{{Name: "interface", Value: val}}
			_, toolErr := tool.Invoke(context.Background(), provider, params, "")
			if toolErr == nil {
				t.Errorf("expected sanitization error for unsafe value %q", val)
			}
		})
	}
}
