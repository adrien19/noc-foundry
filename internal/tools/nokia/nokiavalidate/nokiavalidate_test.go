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

package nokiavalidate_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/tools"
	nokia "github.com/adrien19/noc-foundry/internal/tools/nokia/nokiavalidate"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validation"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func TestParseFromYamlNokiaValidate(t *testing.T) {
	in := `
kind: tools
name: validate_upgrade
type: nokia-validate
source: my-nokia
phases:
  - name: pre
    steps:
      - name: collect_version
        collect:
          into: version
          operation: get_system_version
      - name: assert_version
        assert:
          from: ["version"]
          scope: per_record
          expr: '.payload.software_version == "v24.3.2"'
`

	want := server.ToolConfigs{
		"validate_upgrade": nokia.Config{
			Name:         "validate_upgrade",
			Type:         "nokia-validate",
			Source:       "my-nokia",
			AuthRequired: []string{},
			Phases: []nokia.Phase{
				{
					Name: "pre",
					Steps: []nokia.Step{
						{
							Name: "collect_version",
							Collect: &nokia.CollectSpec{
								Into:      "version",
								Operation: "get_system_version",
							},
						},
						{
							Name: "assert_version",
							Assert: &nokia.AssertSpec{
								From:  []string{"version"},
								Scope: nokia.ScopePerRecord,
								Expr:  `.payload.software_version == "v24.3.2"`,
							},
						},
					},
				},
			},
		},
	}

	_, _, _, got, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(in))
	if err != nil {
		t.Fatalf("unable to unmarshal: %s", err)
	}
	if diff := cmp.Diff(want, got, cmpopts.IgnoreUnexported(tools.ToolAnnotations{})); diff != "" {
		t.Fatalf("incorrect parse (-want +got):\n%s", diff)
	}
}

func TestParseFromYamlNokiaValidateUseProfile(t *testing.T) {
	in := `
kind: tools
name: validate_profile
type: nokia-validate
source: my-nokia
useProfile: base
profiles:
  base:
    phases:
      - name: pre
        steps:
          - name: collect_version
            collect:
              into: version
              operation: get_system_version
`

	_, _, _, got, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(in))
	if err != nil {
		t.Fatalf("unable to unmarshal profile-backed validator: %s", err)
	}
	if _, ok := got["validate_profile"]; !ok {
		t.Fatalf("expected validate_profile tool in parsed configs, got keys: %+v", got)
	}
}

func TestInitializeValidationErrors(t *testing.T) {
	cfg := nokia.Config{
		Name: "validate",
		Type: "nokia-validate",
		Phases: []nokia.Phase{{
			Name: "pre",
			Steps: []nokia.Step{{
				Name: "bad",
				Collect: &nokia.CollectSpec{
					Into: "version",
				},
			}},
		}},
	}
	if _, err := cfg.Initialize(nil); err == nil {
		t.Fatal("expected initialize error")
	}
}

type mockSource struct {
	outputs  map[string]string
	runCount map[string]int
	errs     map[string]error
}

func (m *mockSource) RunCommand(_ context.Context, command string) (string, error) {
	if m.runCount != nil {
		m.runCount[command]++
	}
	if err, ok := m.errs[command]; ok {
		return "", err
	}
	out, ok := m.outputs[command]
	if !ok {
		return "", fmt.Errorf("unknown command %q", command)
	}
	return out, nil
}
func (m *mockSource) SourceType() string             { return "ssh" }
func (m *mockSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockSource) DeviceVendor() string           { return "nokia" }
func (m *mockSource) DevicePlatform() string         { return "srlinux" }
func (m *mockSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

type scriptedSource struct {
	steps []scriptedStep
	index int
}

type scriptedStep struct {
	output string
	err    error
	wait   time.Duration
}

func (s *scriptedSource) RunCommand(ctx context.Context, command string) (string, error) {
	if s.index >= len(s.steps) {
		return "", fmt.Errorf("unexpected command %q", command)
	}
	step := s.steps[s.index]
	s.index++
	if step.wait > 0 {
		timer := time.NewTimer(step.wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-timer.C:
		}
	}
	if step.err != nil {
		return "", step.err
	}
	return step.output, nil
}
func (s *scriptedSource) SourceType() string             { return "ssh" }
func (s *scriptedSource) ToConfig() sources.SourceConfig { return nil }
func (s *scriptedSource) DeviceVendor() string           { return "nokia" }
func (s *scriptedSource) DevicePlatform() string         { return "srlinux" }
func (s *scriptedSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

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

func TestInvokeSingleSourceOperationPass(t *testing.T) {
	src := &mockSource{
		outputs: map[string]string{
			"show version | as json": `{"host-name":"leaf1","software-version":"v24.3.2","system-type":"7220 IXR-D2"}`,
		},
	}

	cfg := nokia.Config{
		Name:   "validate_upgrade",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{
			Name: "pre",
			Steps: []nokia.Step{
				{
					Name: "collect_version",
					Collect: &nokia.CollectSpec{
						Into:      "version",
						Operation: "get_system_version",
					},
				},
				{
					Name: "assert_version",
					Assert: &nokia.AssertSpec{
						Name:  "expected_version",
						From:  []string{"version"},
						Scope: nokia.ScopePerRecord,
						Expr:  `.payload.software_version == "v24.3.2"`,
					},
				},
			},
		}},
	}

	tool, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	result, toolErr := tool.Invoke(context.Background(), &mockSourceProvider{
		sources: map[string]sources.Source{"lab/leaf1/ssh": src},
	}, parameters.ParamValues{}, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	got := result.(nokia.Result)
	if got.Status != nokia.StatusPass {
		t.Fatalf("status = %q, want %q", got.Status, nokia.StatusPass)
	}
	if got.Phase != "pre" {
		t.Fatalf("phase = %q, want %q", got.Phase, "pre")
	}
	if len(got.Evidence) != 1 || got.Evidence[0].Summary.Succeeded != 1 {
		t.Fatalf("unexpected evidence summary: %+v", got.Evidence)
	}
}

func TestInvokeSelectorAggregatePartial(t *testing.T) {
	good := &mockSource{
		outputs: map[string]string{
			"show system alarms | as json": `{"active_alarms":[]}`,
		},
	}
	bad := &mockSource{
		errs: map[string]error{
			"show system alarms | as json": fmt.Errorf("temporary transport failure"),
		},
	}

	cfg := nokia.Config{
		Name: "validate_alarms",
		Type: "nokia-validate",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"service": "bng"},
		},
		Phases: []nokia.Phase{{
			Name: "pre",
			Steps: []nokia.Step{
				{
					Name: "collect_alarms",
					Collect: &nokia.CollectSpec{
						Into:    "alarms",
						Command: "show system alarms | as json",
						Transforms: map[string]query.TransformSpec{
							query.OpRunCommand: {Format: "json"},
						},
					},
				},
				{
					Name: "assert_no_alarms",
					Assert: &nokia.AssertSpec{
						From:  []string{"alarms"},
						Scope: nokia.ScopeAggregate,
						Expr:  `([.records[] | select(.error == null and ((.payload.active_alarms // []) | length > 0))] | length) == 0`,
					},
				},
			},
		}},
	}

	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	provider := &mockSourceProvider{
		sources: map[string]sources.Source{
			"lab/bng-1/ssh": good,
			"lab/bng-2/ssh": bad,
		},
		labelSources: map[string]sources.Source{
			"lab/bng-1/ssh": good,
			"lab/bng-2/ssh": bad,
		},
		poolLabels: map[string]map[string]string{
			"lab/bng-1/ssh": {"service": "bng"},
			"lab/bng-2/ssh": {"service": "bng"},
		},
	}

	result, toolErr := tool.Invoke(context.Background(), provider, parameters.ParamValues{}, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	got := result.(nokia.Result)
	if got.Status != nokia.StatusFail {
		t.Fatalf("status = %q, want %q", got.Status, nokia.StatusFail)
	}
	if got.Steps[0].Status != nokia.StatusPartial {
		t.Fatalf("collect step status = %q, want %q", got.Steps[0].Status, nokia.StatusPartial)
	}
	if got.Steps[1].Status != nokia.StatusPass {
		t.Fatalf("assert step status = %q, want %q", got.Steps[1].Status, nokia.StatusPass)
	}
}

func TestInvokeRequiresPhaseParam(t *testing.T) {
	cfg := nokia.Config{
		Name:   "validate",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{
			{Name: "pre", Steps: []nokia.Step{{Name: "collect", Collect: &nokia.CollectSpec{Into: "x", Command: "show version"}}}},
			{Name: "post", Steps: []nokia.Step{{Name: "collect", Collect: &nokia.CollectSpec{Into: "x", Command: "show version"}}}},
		},
	}
	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	params := tool.GetParameters()
	if len(params) != 1 || params[0].GetName() != "phase" {
		t.Fatalf("unexpected params: %+v", params.Manifest())
	}

	_, toolErr := tool.Invoke(context.Background(), &mockSourceProvider{}, parameters.ParamValues{}, "")
	if toolErr == nil {
		t.Fatal("expected missing phase error")
	}
}

func TestSelectorDeviceNarrowingAndDedup(t *testing.T) {
	netconf := &mockSource{
		outputs: map[string]string{
			"show version | as json": `{"host-name":"edge-1","software-version":"v24.3.2"}`,
		},
	}
	ssh := &mockSource{
		outputs: map[string]string{
			"show version | as json": `{"host-name":"edge-1","software-version":"v24.3.2"}`,
		},
	}

	cfg := nokia.Config{
		Name: "validate",
		Type: "nokia-validate",
		SourceSelector: &nokia.SourceSelector{
			MatchLabels: map[string]string{"role": "edge"},
		},
		Phases: []nokia.Phase{{
			Name: "pre",
			Steps: []nokia.Step{
				{
					Name: "collect_version",
					Collect: &nokia.CollectSpec{
						Into:      "version",
						Operation: "get_system_version",
					},
				},
			},
		}},
	}

	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}

	provider := &mockSourceProvider{
		sources: map[string]sources.Source{
			"lab/edge-1/netconf": netconf,
			"lab/edge-1/ssh":     ssh,
			"lab/edge-2/ssh":     ssh,
		},
		labelSources: map[string]sources.Source{
			"lab/edge-1/netconf": netconf,
			"lab/edge-1/ssh":     ssh,
			"lab/edge-2/ssh":     ssh,
		},
		poolLabels: map[string]map[string]string{
			"lab/edge-1/netconf": {"role": "edge"},
			"lab/edge-1/ssh":     {"role": "edge"},
			"lab/edge-2/ssh":     {"role": "edge"},
		},
	}

	result, toolErr := tool.Invoke(context.Background(), provider, parameters.ParamValues{
		{Name: "device", Value: "edge-1"},
	}, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}

	got := result.(nokia.Result)
	if len(got.Evidence) != 1 || len(got.Evidence[0].Records) != 1 {
		t.Fatalf("unexpected evidence: %+v", got.Evidence)
	}
	if got.Evidence[0].Records[0].SourceName != "lab/edge-1/netconf" {
		t.Fatalf("source name = %q, want %q", got.Evidence[0].Records[0].SourceName, "lab/edge-1/netconf")
	}
}

func TestPrimitiveAssertionCountGTE(t *testing.T) {
	src := &mockSource{
		outputs: map[string]string{
			"show interface | as json": `[{"name":"ethernet-1/1","oper-status":"UP"},{"name":"ethernet-1/2","oper-status":"UP"},{"name":"ethernet-1/3","oper-status":"UP"}]`,
		},
	}
	cfg := nokia.Config{
		Name:   "validate_interfaces",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{
			Name: "pre",
			Steps: []nokia.Step{
				{Name: "collect", Collect: &nokia.CollectSpec{
					Into: "interfaces", Command: "show interface | as json",
					Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
				}},
				{Name: "assert", Assert: &nokia.AssertSpec{
					From:  []string{"interfaces"},
					Scope: nokia.ScopePerRecord,
					Primitive: &nokia.AssertionCheck{
						Type:   nokia.AssertionCountGTE,
						Path:   "payload",
						Filter: `.["oper-status"] == "UP"`,
						Count:  ptrInt(3),
					},
				}},
			},
		}},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	result, toolErr := tool.Invoke(context.Background(), &mockSourceProvider{sources: map[string]sources.Source{"lab/leaf1/ssh": src}}, nil, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}
	got := result.(nokia.Result)
	if got.Status != nokia.StatusPass {
		t.Fatalf("status = %q, want pass", got.Status)
	}
}

func TestWarningSeverityProducesPartialOutcome(t *testing.T) {
	src := &mockSource{
		outputs: map[string]string{
			"show version | as json": `{"host-name":"leaf1","software-version":"v24.3.2"}`,
		},
	}
	cfg := nokia.Config{
		Name:   "validate_warning",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{
			Name: "pre",
			Steps: []nokia.Step{
				{Name: "collect", Collect: &nokia.CollectSpec{
					Into: "version", Command: "show version | as json",
					Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
				}},
				{Name: "assert", Assert: &nokia.AssertSpec{
					From:     []string{"version"},
					Scope:    nokia.ScopePerRecord,
					Severity: nokia.SeverityWarning,
					Expr:     `.payload."software-version" == "bad"`,
					Message:  "review software version drift",
				}},
			},
		}},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	result, toolErr := tool.Invoke(context.Background(), &mockSourceProvider{sources: map[string]sources.Source{"lab/leaf1/ssh": src}}, nil, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}
	got := result.(nokia.Result)
	if got.Status != nokia.StatusPartial || got.Outcome != nokia.OutcomeWarning || got.Blocking {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestUseProfileAndTemplateExpansion(t *testing.T) {
	cfg := nokia.Config{
		Name:       "validate_profile",
		Type:       "nokia-validate",
		Source:     "lab/leaf1/ssh",
		UseProfile: "base",
		Profiles: map[string]nokia.ValidationProfile{
			"base": {
				Phases: []nokia.PhaseTemplate{{Name: "pre", Steps: []nokia.StepTemplate{{Name: "collect_version", Collect: &nokia.CollectSpec{Into: "version", Command: "show version"}}}}},
			},
		},
	}
	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	if tool.ToConfig().(nokia.Config).Phases[0].Steps[0].Name != "collect_version" {
		t.Fatalf("template expansion did not materialize phase steps")
	}
}

func TestTransportRequireWithoutFallbackProducesPartialCollection(t *testing.T) {
	src := &mockSource{
		outputs: map[string]string{"show version | as json": `{"software-version":"v24.3.2"}`},
	}
	cfg := nokia.Config{
		Name:           "validate_transport",
		Type:           "nokia-validate",
		SourceSelector: &nokia.SourceSelector{MatchLabels: map[string]string{"role": "edge"}},
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{{
			Name: "collect",
			Collect: &nokia.CollectSpec{
				Into: "version", Command: "show version | as json",
				Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
				Transport:  &nokia.TransportPolicy{Prefer: []string{"gnmi"}, Require: []string{"gnmi"}, Fallback: false},
			},
		}}}},
	}
	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	provider := &mockSourceProvider{
		sources:      map[string]sources.Source{"lab/edge-1/ssh": src},
		labelSources: map[string]sources.Source{"lab/edge-1/ssh": src},
		poolLabels:   map[string]map[string]string{"lab/edge-1/ssh": {"role": "edge"}},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, nil, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}
	got := result.(nokia.Result)
	if got.Status != nokia.StatusFail || got.Evidence[0].Summary.Failed == 0 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestConvergeAssertionPassesAfterRetry(t *testing.T) {
	src := &mockSource{
		outputs:  map[string]string{"show version | as json": `{"software-version":"v24.3.2"}`},
		runCount: map[string]int{},
	}
	cfg := nokia.Config{
		Name:   "validate_converge",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{
			{Name: "collect", Collect: &nokia.CollectSpec{
				Into: "version", Command: "show version | as json",
				Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
			}},
			{Name: "assert", Assert: &nokia.AssertSpec{
				From: []string{"version"}, Scope: nokia.ScopePerRecord,
				Expr: `.payload."software-version" == "v24.3.2"`,
			}, Converge: &nokia.ConvergePolicy{Until: nokia.ConvergeAssertionPass, Interval: "1ms", MaxAttempts: 2}},
		}}},
	}
	tool, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	result, toolErr := tool.Invoke(context.Background(), &mockSourceProvider{sources: map[string]sources.Source{"lab/leaf1/ssh": src}}, nil, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}
	got := result.(nokia.Result)
	if got.Steps[1].Convergence == nil || !got.Steps[1].Convergence.Met {
		t.Fatalf("expected convergence metadata, got %+v", got.Steps[1])
	}
}

func TestExecuteCompiledStepReturnsSingleConvergenceObservation(t *testing.T) {
	src := &mockSource{
		outputs: map[string]string{"show version | as json": `{"software-version":"v24.3.2"}`},
	}
	cfg := nokia.Config{
		Name:   "validate_converge_step",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{
			{Name: "collect", Collect: &nokia.CollectSpec{
				Into: "version", Command: "show version | as json",
				Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
			}},
			{Name: "assert", Assert: &nokia.AssertSpec{
				From: []string{"version"}, Scope: nokia.ScopePerRecord,
				Expr: `.payload."software-version" == "v24.3.2"`,
			}, Converge: &nokia.ConvergePolicy{Until: nokia.ConvergeAssertionPass, Interval: "1ms", MaxAttempts: 2, MinPasses: 2}},
		}}},
	}
	toolAny, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	tool := toolAny.(nokia.Tool)
	provider := &mockSourceProvider{sources: map[string]sources.Source{"lab/leaf1/ssh": src}}
	compiled, err := tool.CompileValidationRun(context.Background(), provider, nil)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	collectOutput, toolErr := tool.ExecuteCompiledStep(context.Background(), provider, compiled, validation.StepRef{
		Phase: "pre",
		Index: 0,
		Name:  "collect",
		Kind:  string(nokia.StepKindCollect),
	}, validation.StepExecutionInput{Attempt: 1}, "")
	if toolErr != nil {
		t.Fatalf("collect step failed: %v", toolErr)
	}
	assertOutput, toolErr := tool.ExecuteCompiledStep(context.Background(), provider, compiled, validation.StepRef{
		Phase: "pre",
		Index: 1,
		Name:  "assert",
		Kind:  string(nokia.StepKindAssert),
	}, validation.StepExecutionInput{
		Attempt:  1,
		Evidence: collectOutput.EvidenceDelta,
	}, "")
	if toolErr != nil {
		t.Fatalf("assert step failed: %v", toolErr)
	}
	if assertOutput.StepCompleted {
		t.Fatalf("expected single assert observation to be non-terminal, got %+v", assertOutput)
	}
	if assertOutput.RetryAfter == "" || assertOutput.ConvergenceState == nil || assertOutput.ConvergenceState.PassStreak != 1 {
		t.Fatalf("unexpected convergence output: %+v", assertOutput)
	}
}

func TestExecuteCompiledCollectStepRetriesTransientFailure(t *testing.T) {
	src := &scriptedSource{
		steps: []scriptedStep{
			{err: fmt.Errorf("dial tcp 192.0.2.1:22: i/o timeout")},
			{output: `{"software-version":"v24.3.2"}`},
		},
	}
	cfg := nokia.Config{
		Name:   "validate_collect_retry",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{{
			Name: "collect",
			Collect: &nokia.CollectSpec{
				Into: "version", Command: "show version | as json",
				Retry: &nokia.RetryPolicy{
					Interval:    "1ms",
					MaxAttempts: 2,
				},
				Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
			},
		}}}},
	}
	toolAny, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	tool := toolAny.(nokia.Tool)
	provider := &mockSourceProvider{sources: map[string]sources.Source{"lab/leaf1/ssh": src}}
	compiled, err := tool.CompileValidationRun(context.Background(), provider, nil)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}

	first, toolErr := tool.ExecuteCompiledStep(context.Background(), provider, compiled, validation.StepRef{
		Phase: "pre", Index: 0, Name: "collect", Kind: string(nokia.StepKindCollect),
	}, validation.StepExecutionInput{Attempt: 1}, "")
	if toolErr != nil {
		t.Fatalf("first attempt failed unexpectedly: %v", toolErr)
	}
	if first.StepCompleted || first.RetryAfter == "" {
		t.Fatalf("expected retry scheduling output, got %+v", first)
	}
	if len(first.EvidenceDelta) != 0 {
		t.Fatalf("expected no evidence delta on failed attempt, got %+v", first.EvidenceDelta)
	}

	second, toolErr := tool.ExecuteCompiledStep(context.Background(), provider, compiled, validation.StepRef{
		Phase: "pre", Index: 0, Name: "collect", Kind: string(nokia.StepKindCollect),
	}, validation.StepExecutionInput{Attempt: 2}, "")
	if toolErr != nil {
		t.Fatalf("second attempt failed unexpectedly: %v", toolErr)
	}
	if !second.StepCompleted || second.Terminal {
		t.Fatalf("expected completed successful retry, got %+v", second)
	}
	if len(second.EvidenceDelta) != 1 {
		t.Fatalf("expected evidence delta on successful retry, got %+v", second.EvidenceDelta)
	}
}

func TestCollectRetryValidation(t *testing.T) {
	t.Run("requires interval for retries", func(t *testing.T) {
		cfg := nokia.Config{
			Name:   "validate_collect_retry",
			Type:   "nokia-validate",
			Source: "lab/leaf1/ssh",
			Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{{
				Name: "collect",
				Collect: &nokia.CollectSpec{
					Into:    "version",
					Command: "show version | as json",
					Retry:   &nokia.RetryPolicy{MaxAttempts: 2},
				},
			}}}},
		}
		if _, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": &mockSource{outputs: map[string]string{"show version | as json": `{}`}}}); err == nil {
			t.Fatal("expected retry interval validation error")
		}
	})

	t.Run("rejects invalid collect retry timeout", func(t *testing.T) {
		cfg := nokia.Config{
			Name:   "validate_collect_retry",
			Type:   "nokia-validate",
			Source: "lab/leaf1/ssh",
			Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{{
				Name: "collect",
				Collect: &nokia.CollectSpec{
					Into:    "version",
					Command: "show version | as json",
					Retry:   &nokia.RetryPolicy{Timeout: "bad"},
				},
			}}}},
		}
		if _, err := cfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": &mockSource{outputs: map[string]string{"show version | as json": `{}`}}}); err == nil {
			t.Fatal("expected collect retry timeout validation error")
		}
	})
}

func TestCommandCollectionDefaultsToSSH(t *testing.T) {
	ssh := &mockSource{
		outputs: map[string]string{"show version | as json": `{"software-version":"v24.3.2"}`},
	}
	cfg := nokia.Config{
		Name:           "validate_command_default",
		Type:           "nokia-validate",
		SourceSelector: &nokia.SourceSelector{MatchLabels: map[string]string{"role": "edge"}},
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{{
			Name: "collect",
			Collect: &nokia.CollectSpec{
				Into: "version", Command: "show version | as json",
				Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
			},
		}}}},
	}
	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	provider := &mockSourceProvider{
		sources: map[string]sources.Source{
			"lab/edge-1/netconf": &mockSource{outputs: map[string]string{}},
			"lab/edge-1/ssh":     ssh,
		},
		labelSources: map[string]sources.Source{
			"lab/edge-1/netconf": &mockSource{outputs: map[string]string{}},
			"lab/edge-1/ssh":     ssh,
		},
		poolLabels: map[string]map[string]string{
			"lab/edge-1/netconf": {"role": "edge"},
			"lab/edge-1/ssh":     {"role": "edge"},
		},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, nil, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}
	got := result.(nokia.Result)
	if got.Evidence[0].Records[0].SourceName != "lab/edge-1/ssh" {
		t.Fatalf("source name = %q, want ssh", got.Evidence[0].Records[0].SourceName)
	}
}

func TestEvidenceRefFiltersByStoredGroups(t *testing.T) {
	src := &mockSource{
		outputs: map[string]string{"show version | as json": `{"software-version":"v24.3.2"}`},
	}
	cfg := nokia.Config{
		Name:           "validate_groups",
		Type:           "nokia-validate",
		SourceSelector: &nokia.SourceSelector{MatchLabels: map[string]string{"role": "edge"}},
		Groups: map[string]nokia.TargetGroup{
			"primary": {MatchLabels: map[string]string{"lane": "a"}},
			"backup":  {MatchLabels: map[string]string{"lane": "b"}},
		},
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{
			{Name: "collect", Collect: &nokia.CollectSpec{
				Into: "version", Command: "show version | as json", Targets: []string{"primary"},
				Transforms: map[string]query.TransformSpec{query.OpRunCommand: {Format: "json"}},
			}},
			{Name: "assert", Assert: &nokia.AssertSpec{
				Inputs: map[string]nokia.EvidenceRef{
					"p": {Evidence: "version", Group: "primary", Alias: "primary"},
				},
				Scope: nokia.ScopeAggregate,
				Expr:  `(.inputs.primary | length) == 1`,
			}},
		}}},
	}
	tool, err := cfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize failed: %v", err)
	}
	provider := &mockSourceProvider{
		sources: map[string]sources.Source{
			"lab/edge-1/ssh": src,
			"lab/edge-2/ssh": src,
		},
		labelSources: map[string]sources.Source{
			"lab/edge-1/ssh": src,
			"lab/edge-2/ssh": src,
		},
		poolLabels: map[string]map[string]string{
			"lab/edge-1/ssh": {"role": "edge", "lane": "a"},
			"lab/edge-2/ssh": {"role": "edge", "lane": "b"},
		},
	}
	result, toolErr := tool.Invoke(context.Background(), provider, nil, "")
	if toolErr != nil {
		t.Fatalf("invoke failed: %v", toolErr)
	}
	got := result.(nokia.Result)
	if got.Status != nokia.StatusPass {
		t.Fatalf("unexpected result: %+v", got)
	}
}

func TestDeltaWithinRequiresReduce(t *testing.T) {
	cfg := nokia.Config{
		Name:   "validate_delta",
		Type:   "nokia-validate",
		Source: "lab/leaf1/ssh",
		Phases: []nokia.Phase{{Name: "pre", Steps: []nokia.Step{{
			Name: "assert",
			Assert: &nokia.AssertSpec{
				Compare: []nokia.EvidenceComparisonRef{{
					Left:  nokia.EvidenceRef{Evidence: "a"},
					Right: nokia.EvidenceRef{Evidence: "b"},
				}},
				Scope: nokia.ScopeAggregate,
				Primitive: &nokia.AssertionCheck{
					Type:      nokia.AssertionDeltaWithin,
					Tolerance: ptrFloat(0),
				},
			},
		}}}},
	}
	if _, err := cfg.Initialize(nil); err == nil {
		t.Fatal("expected initialize error")
	}
}

func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }
