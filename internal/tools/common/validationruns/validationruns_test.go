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

package validationruns_test

import (
	"context"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/server/resources"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	validate "github.com/adrien19/noc-foundry/internal/tools/common/validate"
	common "github.com/adrien19/noc-foundry/internal/tools/common/validationruns"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validationruns"
)

type mockSource struct {
	output string
	delay  time.Duration
}

func (m *mockSource) RunCommand(ctx context.Context, command string) (string, error) {
	if m.delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(m.delay):
		}
	}
	return m.output, nil
}

func (m *mockSource) SourceType() string             { return "ssh" }
func (m *mockSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockSource) DeviceVendor() string           { return "nokia" }
func (m *mockSource) DevicePlatform() string         { return "srlinux" }
func (m *mockSource) DeviceVersion() string          { return "" }
func (m *mockSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

func TestValidationRunLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &mockSource{output: `{"host-name":"leaf1","software-version":"v24.3.2"}`}
	targetCfg := validate.Config{
		Name:   "validate_upgrade",
		Type:   "validate",
		Source: "lab/leaf1/ssh",
		Phases: []validate.Phase{{
			Name: "pre",
			Steps: []validate.Step{
				{Name: "collect", Collect: &validate.CollectSpec{
					Into: "version", Command: "show version | as json",
				}},
				{Name: "assert", Assert: &validate.AssertSpec{
					From: []string{"version"}, Scope: validate.ScopePerRecord,
					Expr: `.payload."software-version" == "v24.3.2"`,
				}},
			},
		}},
	}
	targetTool, err := targetCfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize target tool: %v", err)
	}

	startCfg := common.Config{Name: "start_validation", Type: "validation-run-start"}
	startTool, err := startCfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize start tool: %v", err)
	}
	statusCfg := common.Config{Name: "validation_status", Type: "validation-run-status"}
	statusTool, err := statusCfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize status tool: %v", err)
	}
	resultCfg := common.Config{Name: "validation_result", Type: "validation-run-result"}
	resultTool, err := resultCfg.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize result tool: %v", err)
	}

	resourceMgr := resources.NewResourceManager(
		map[string]sources.Source{"lab/leaf1/ssh": src},
		nil, nil,
		map[string]tools.Tool{"validate_upgrade": targetTool},
		nil, nil, nil,
	)
	manager, err := validationruns.NewManager(ctx, validationruns.Config{ExecutionBackend: "local"}, resourceMgr, nil)
	if err != nil {
		t.Fatalf("initialize manager: %v", err)
	}
	defer func() { _ = manager.Shutdown(context.Background()) }()
	resourceMgr.SetValidationRunManager(manager)

	startResult, toolErr := startTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
		{Name: "validation", Value: "validate_upgrade"},
		{Name: "params", Value: map[string]any{}},
	}, "")
	if toolErr != nil {
		t.Fatalf("start failed: %v", toolErr)
	}
	handle := startResult.(validationruns.RunHandle)
	if handle.Status != validationruns.StatusAccepted {
		t.Fatalf("unexpected handle: %+v", handle)
	}

	var run map[string]any
	deadline := time.Now().Add(2 * time.Second)
	for {
		statusResult, toolErr := statusTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
			{Name: "run_id", Value: handle.RunID},
			{Name: "after_sequence", Value: 0},
			{Name: "limit", Value: 20},
		}, "")
		if toolErr != nil {
			t.Fatalf("status failed: %v", toolErr)
		}
		run = statusResult.(map[string]any)
		record := run["run"].(validationruns.RunRecord)
		if record.Status == validationruns.StatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("validation run did not complete: %+v", record)
		}
		time.Sleep(10 * time.Millisecond)
	}

	resultResult, toolErr := resultTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
		{Name: "run_id", Value: handle.RunID},
	}, "")
	if toolErr != nil {
		t.Fatalf("result failed: %v", toolErr)
	}
	resultPayload := resultResult.(map[string]any)
	record := resultPayload["run"].(validationruns.RunRecord)
	if record.Status != validationruns.StatusCompleted {
		t.Fatalf("unexpected run status: %+v", record)
	}
	events := run["events"].([]validationruns.RunEvent)
	var sawRunning, sawStep bool
	for _, event := range events {
		if event.Type == "running" {
			sawRunning = true
		}
		if event.Type == "step_started" {
			sawStep = true
		}
	}
	if !sawRunning || !sawStep {
		t.Fatalf("expected running and step events, got %+v", events)
	}
}

func TestValidationRunCancelAndIdempotency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &mockSource{output: `{"host-name":"leaf1","software-version":"v24.3.2"}`, delay: 100 * time.Millisecond}
	targetCfg := validate.Config{
		Name:   "validate_upgrade",
		Type:   "validate",
		Source: "lab/leaf1/ssh",
		Phases: []validate.Phase{{
			Name: "pre",
			Steps: []validate.Step{{
				Name: "collect",
				Collect: &validate.CollectSpec{
					Into: "version", Command: "show version | as json",
				},
			}},
		}},
	}
	targetTool, err := targetCfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize target tool: %v", err)
	}
	startTool, err := common.Config{Name: "start_validation", Type: "validation-run-start"}.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize start tool: %v", err)
	}
	cancelTool, err := common.Config{Name: "cancel_validation", Type: "validation-run-cancel"}.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize cancel tool: %v", err)
	}
	statusTool, err := common.Config{Name: "validation_status", Type: "validation-run-status"}.Initialize(nil)
	if err != nil {
		t.Fatalf("initialize status tool: %v", err)
	}

	resourceMgr := resources.NewResourceManager(
		map[string]sources.Source{"lab/leaf1/ssh": src},
		nil, nil,
		map[string]tools.Tool{"validate_upgrade": targetTool},
		nil, nil, nil,
	)
	manager, err := validationruns.NewManager(ctx, validationruns.Config{ExecutionBackend: "local"}, resourceMgr, nil)
	if err != nil {
		t.Fatalf("initialize manager: %v", err)
	}
	defer func() { _ = manager.Shutdown(context.Background()) }()
	resourceMgr.SetValidationRunManager(manager)

	first, toolErr := startTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
		{Name: "validation", Value: "validate_upgrade"},
		{Name: "params", Value: map[string]any{}},
		{Name: "idempotency_key", Value: "same-run"},
	}, "")
	if toolErr != nil {
		t.Fatalf("first start failed: %v", toolErr)
	}
	second, toolErr := startTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
		{Name: "validation", Value: "validate_upgrade"},
		{Name: "params", Value: map[string]any{}},
		{Name: "idempotency_key", Value: "same-run"},
	}, "")
	if toolErr != nil {
		t.Fatalf("second start failed: %v", toolErr)
	}
	firstHandle := first.(validationruns.RunHandle)
	secondHandle := second.(validationruns.RunHandle)
	if firstHandle.RunID != secondHandle.RunID {
		t.Fatalf("idempotency did not reuse active run: %+v %+v", firstHandle, secondHandle)
	}

	_, toolErr = cancelTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
		{Name: "run_id", Value: firstHandle.RunID},
		{Name: "reason", Value: "test cancellation"},
	}, "")
	if toolErr != nil {
		t.Fatalf("cancel failed: %v", toolErr)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		statusResult, toolErr := statusTool.Invoke(ctx, resourceMgr, parameters.ParamValues{
			{Name: "run_id", Value: firstHandle.RunID},
			{Name: "after_sequence", Value: 0},
			{Name: "limit", Value: 20},
		}, "")
		if toolErr != nil {
			t.Fatalf("status failed: %v", toolErr)
		}
		record := statusResult.(map[string]any)["run"].(validationruns.RunRecord)
		if record.Status == validationruns.StatusCancelled {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("validation run did not cancel: %+v", record)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
