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

package nokiashowversion_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/tools"
	nokia "github.com/adrien19/noc-foundry/internal/tools/nokia/nokiashowversion"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestParseFromYamlNokiaShowVersion(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want server.ToolConfigs
	}{
		{
			desc: "basic example",
			in: `
			kind: tools
			name: show_version
			type: nokia-show-version
			source: my-nokia
			`,
			want: map[string]tools.ToolConfig{
				"show_version": nokia.Config{
					Name:         "show_version",
					Type:         "nokia-show-version",
					Source:       "my-nokia",
					AuthRequired: []string{},
				},
			},
		},
		{
			desc: "full example with description and auth",
			in: `
			kind: tools
			name: show_version
			type: nokia-show-version
			source: my-nokia
			description: Show version info on Nokia
			authRequired:
				- network-admin
			`,
			want: map[string]tools.ToolConfig{
				"show_version": nokia.Config{
					Name:         "show_version",
					Type:         "nokia-show-version",
					Source:       "my-nokia",
					Description:  "Show version info on Nokia",
					AuthRequired: []string{"network-admin"},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, got, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreUnexported(tools.ToolAnnotations{})); diff != "" {
				t.Fatalf("incorrect parse (-want +got):\n%s", diff)
			}
		})
	}
}

func TestFailParseFromYamlNokiaShowVersion(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
	}{
		{
			desc: "extra field",
			in: `
			kind: tools
			name: show_version
			type: nokia-show-version
			source: my-nokia
			unknownField: value
			`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err == nil {
				t.Fatal("expected error but got nil")
			}
		})
	}
}

// mockSource implements SourceIdentity + CapabilityProvider + CommandRunner for testing.
type mockSource struct {
	output string
	err    error
}

func (m *mockSource) RunCommand(_ context.Context, _ string) (string, error) {
	return m.output, m.err
}
func (m *mockSource) SourceType() string             { return "ssh" }
func (m *mockSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockSource) DeviceVendor() string           { return "nokia" }
func (m *mockSource) DevicePlatform() string         { return "srlinux" }
func (m *mockSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

// mockSourceProvider implements tools.SourceProvider for testing.
type mockSourceProvider struct {
	sources      map[string]sources.Source
	labelSources map[string]sources.Source
	labelErr     error
	poolLabels   map[string]map[string]string
}

func (m *mockSourceProvider) GetSource(name string) (sources.Source, bool) {
	s, ok := m.sources[name]
	return s, ok
}

func (m *mockSourceProvider) GetSourcesByLabels(_ context.Context, matchLabels map[string]string) (map[string]sources.Source, error) {
	if m.labelErr != nil {
		return nil, m.labelErr
	}
	return m.labelSources, nil
}

func (m *mockSourceProvider) GetDevicePoolLabels() map[string]map[string]string {
	return m.poolLabels
}

func TestInvokeShowVersion(t *testing.T) {
	sampleOutput := `Hostname          : srl1
Software Version  : v24.3.2
System Type       : 7220 IXR-D2`

	ms := &mockSource{output: sampleOutput}
	provider := &mockSourceProvider{
		sources: map[string]sources.Source{"my-nokia": ms},
	}

	cfg := nokia.Config{
		Name:   "show_version",
		Type:   "nokia-show-version",
		Source: "my-nokia",
	}

	srcs := map[string]sources.Source{"my-nokia": ms}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}

	params := parameters.ParamValues{}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	record, ok := result.(*models.Record)
	if !ok {
		t.Fatalf("expected result to be *models.Record, got %T", result)
	}

	if record.RecordType != "get_system_version" {
		t.Errorf("RecordType = %q, want %q", record.RecordType, "get_system_version")
	}
	if record.Source.Vendor != "nokia" {
		t.Errorf("Vendor = %q, want %q", record.Source.Vendor, "nokia")
	}
	if record.Collection.Protocol != models.ProtocolCLI {
		t.Errorf("Protocol = %q, want %q", record.Collection.Protocol, models.ProtocolCLI)
	}

	sv, ok := record.Payload.(models.SystemVersion)
	if !ok {
		t.Fatalf("Payload type = %T, want models.SystemVersion", record.Payload)
	}
	if sv.Hostname != "srl1" {
		t.Errorf("Hostname = %q, want %q", sv.Hostname, "srl1")
	}
	if sv.SoftwareVersion != "v24.3.2" {
		t.Errorf("SoftwareVersion = %q, want %q", sv.SoftwareVersion, "v24.3.2")
	}
}

func TestInitializeWithMissingSource(t *testing.T) {
	// A source not in the eager map is accepted — it may come from
	// the device pool and will be validated at invocation time.
	cfg := nokia.Config{
		Name:   "show_version",
		Type:   "nokia-show-version",
		Source: "nonexistent",
	}
	srcs := map[string]sources.Source{}
	_, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitializeWithIncompatibleSource(t *testing.T) {
	incompatible := &incompatibleSource{}
	cfg := nokia.Config{
		Name:   "show_version",
		Type:   "nokia-show-version",
		Source: "bad-source",
	}
	srcs := map[string]sources.Source{"bad-source": incompatible}
	_, err := cfg.Initialize(srcs)
	if err == nil {
		t.Fatal("expected error for incompatible source")
	}
}

type incompatibleSource struct{}

func (s *incompatibleSource) SourceType() string             { return "incompatible" }
func (s *incompatibleSource) ToConfig() sources.SourceConfig { return nil }

func TestInvokeWithSourceError(t *testing.T) {
	ms := &mockSource{err: fmt.Errorf("connection refused")}
	provider := &mockSourceProvider{
		sources: map[string]sources.Source{"my-nokia": ms},
	}

	cfg := nokia.Config{
		Name:   "show_version",
		Type:   "nokia-show-version",
		Source: "my-nokia",
	}
	srcs := map[string]sources.Source{"my-nokia": ms}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}

	params := parameters.ParamValues{}
	_, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr == nil {
		t.Fatal("expected error from invoke")
	}
}

func TestMcpManifestIsReadOnly(t *testing.T) {
	ms := &mockSource{output: ""}
	cfg := nokia.Config{
		Name:   "show_version",
		Type:   "nokia-show-version",
		Source: "my-nokia",
	}
	srcs := map[string]sources.Source{"my-nokia": ms}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize tool: %v", err)
	}

	manifest := tool.McpManifest()
	if manifest.Annotations.ReadOnlyHint == nil || !*manifest.Annotations.ReadOnlyHint {
		t.Fatal("expected ReadOnlyHint to be true")
	}
}

// --- SourceSelector tests ---

func TestInitializeWithSourceSelector(t *testing.T) {
	cfg := nokia.Config{
		Name: "show_version",
		Type: "nokia-show-version",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"vendor": "nokia"},
		},
	}
	srcs := map[string]sources.Source{}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize with selector: %v", err)
	}

	params := tool.GetParameters()
	foundDevice := false
	for _, p := range params {
		m := p.Manifest()
		if m.Name == "device" {
			foundDevice = true
		}
	}
	if !foundDevice {
		t.Error("expected 'device' parameter in selector mode")
	}
}

func TestInitializeMutualExclusivity(t *testing.T) {
	ms := &mockSource{output: ""}

	cfg := nokia.Config{
		Name:   "show_version",
		Type:   "nokia-show-version",
		Source: "my-nokia",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"vendor": "nokia"},
		},
	}
	srcs := map[string]sources.Source{"my-nokia": ms}
	_, err := cfg.Initialize(srcs)
	if err == nil {
		t.Fatal("expected error when both source and sourceSelector are set")
	}

	cfg2 := nokia.Config{
		Name: "show_version",
		Type: "nokia-show-version",
	}
	_, err = cfg2.Initialize(srcs)
	if err == nil {
		t.Fatal("expected error when neither source nor sourceSelector is set")
	}
}

func TestInvokeWithSelectorFanOut(t *testing.T) {
	sampleOutput := `Hostname          : srl1
Software Version  : v24.3.2
System Type       : 7220 IXR-D2`

	ms1 := &mockSource{output: sampleOutput}
	ms2 := &mockSource{output: sampleOutput}

	provider := &mockSourceProvider{
		sources: map[string]sources.Source{
			"dc1/spine-1/default": ms1,
			"dc1/spine-2/default": ms2,
		},
		labelSources: map[string]sources.Source{
			"dc1/spine-1/default": ms1,
			"dc1/spine-2/default": ms2,
		},
	}

	cfg := nokia.Config{
		Name: "show_version",
		Type: "nokia-show-version",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"vendor": "nokia"},
		},
	}
	srcs := map[string]sources.Source{}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	fanResult, ok := result.(fanout.Result)
	if !ok {
		t.Fatalf("expected fanout.Result, got %T", result)
	}
	if len(fanResult.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(fanResult.Results))
	}
	for _, dr := range fanResult.Results {
		if dr.Status != "success" {
			t.Errorf("device %q: expected success, got %q (error: %s)", dr.Device, dr.Status, dr.Error)
		}
	}
}

func TestInvokeWithSelectorTargetedDevice(t *testing.T) {
	sampleOutput := `Hostname          : srl1
Software Version  : v24.3.2
System Type       : 7220 IXR-D2`

	ms1 := &mockSource{output: sampleOutput}
	ms2 := &mockSource{output: "should not be called"}

	provider := &mockSourceProvider{
		sources: map[string]sources.Source{
			"dc1/spine-1/default": ms1,
			"dc1/spine-2/default": ms2,
		},
		labelSources: map[string]sources.Source{
			"dc1/spine-1/default": ms1,
			"dc1/spine-2/default": ms2,
		},
	}

	cfg := nokia.Config{
		Name: "show_version",
		Type: "nokia-show-version",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"vendor": "nokia"},
		},
	}
	srcs := map[string]sources.Source{}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{
		{Name: "device", Value: "spine-1"},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	record, ok := result.(*models.Record)
	if !ok {
		t.Fatalf("expected *models.Record for targeted query, got %T", result)
	}
	if record.RecordType != "get_system_version" {
		t.Errorf("RecordType = %q, want %q", record.RecordType, "get_system_version")
	}
}

func TestInvokeWithSelectorNoMatches(t *testing.T) {
	provider := &mockSourceProvider{
		sources:      map[string]sources.Source{},
		labelSources: map[string]sources.Source{},
	}

	cfg := nokia.Config{
		Name: "show_version",
		Type: "nokia-show-version",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"vendor": "nokia"},
		},
	}
	srcs := map[string]sources.Source{}
	tool, err := cfg.Initialize(srcs)
	if err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	params := parameters.ParamValues{}
	_, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr == nil {
		t.Fatal("expected error when no sources match selector")
	}
}

func TestParseSourceSelectorFromYaml(t *testing.T) {
	in := `
	kind: tools
	name: show_version
	type: nokia-show-version
	sourceSelector:
		matchLabels:
			vendor: nokia
		maxConcurrency: 3
	`
	_, _, _, got, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(in))
	if err != nil {
		t.Fatalf("unable to unmarshal: %s", err)
	}

	cfg, ok := got["show_version"].(nokia.Config)
	if !ok {
		t.Fatalf("expected nokia.Config, got %T", got["show_version"])
	}
	if cfg.SourceSelector == nil {
		t.Fatal("expected sourceSelector to be parsed")
	}
	if cfg.SourceSelector.MatchLabels["vendor"] != "nokia" {
		t.Errorf("expected vendor=nokia, got %q", cfg.SourceSelector.MatchLabels["vendor"])
	}
	if cfg.SourceSelector.MaxConcurrency != 3 {
		t.Errorf("expected maxConcurrency=3, got %d", cfg.SourceSelector.MaxConcurrency)
	}
}
