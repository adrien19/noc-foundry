package validationruns_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/server/resources"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	validate "github.com/adrien19/noc-foundry/internal/tools/common/validate"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validation"
	"github.com/adrien19/noc-foundry/internal/validationruns"
)

type testSource struct {
	output string
	err    error
	wait   <-chan struct{}
}

func (m *testSource) RunCommand(ctx context.Context, command string) (string, error) {
	if m.wait != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-m.wait:
		}
	}
	if m.err != nil {
		return "", m.err
	}
	return m.output, nil
}

func (m *testSource) SourceType() string             { return "ssh" }
func (m *testSource) ToConfig() sources.SourceConfig { return nil }
func (m *testSource) DeviceVendor() string           { return "nokia" }
func (m *testSource) DevicePlatform() string         { return "srlinux" }
func (m *testSource) DeviceVersion() string          { return "" }
func (m *testSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

type retryingTestSource struct {
	steps []retryingTestStep
	index int
}

type retryingTestStep struct {
	output string
	err    error
}

func (m *retryingTestSource) RunCommand(ctx context.Context, command string) (string, error) {
	_ = ctx
	_ = command
	if m.index >= len(m.steps) {
		return "", fmt.Errorf("unexpected command invocation")
	}
	step := m.steps[m.index]
	m.index++
	if step.err != nil {
		return "", step.err
	}
	return step.output, nil
}

func (m *retryingTestSource) SourceType() string             { return "ssh" }
func (m *retryingTestSource) ToConfig() sources.SourceConfig { return nil }
func (m *retryingTestSource) DeviceVendor() string           { return "nokia" }
func (m *retryingTestSource) DevicePlatform() string         { return "srlinux" }
func (m *retryingTestSource) DeviceVersion() string          { return "" }
func (m *retryingTestSource) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{CLI: true}
}

func TestSQLiteStoreLifecycle(t *testing.T) {
	store, err := validationruns.NewSQLiteStore(filepath.Join(t.TempDir(), "runs.sqlite"))
	if err != nil {
		t.Fatalf("new sqlite store: %v", err)
	}
	now := time.Now().UTC()
	run := validationruns.RunRecord{
		ID:                "run-1",
		RunType:           "validation",
		ToolName:          "validate_upgrade",
		ToolType:          "validate",
		Status:            validationruns.StatusAccepted,
		ResourceVersion:   1,
		ConfigFingerprint: "cfg",
		PlanFingerprint:   "plan",
		IdempotencyKey:    "dup-1",
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := store.CreateRun(context.Background(), run); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if _, err := store.FindActiveByIdempotencyKey(context.Background(), "dup-1"); err != nil {
		t.Fatalf("find active run: %v", err)
	}
	if err := store.AppendEvent(context.Background(), validationruns.RunEvent{
		RunID:     run.ID,
		Timestamp: now,
		Type:      "accepted",
		Message:   "accepted",
	}); err != nil {
		t.Fatalf("append event: %v", err)
	}
	events, err := store.ListEvents(context.Background(), run.ID, 0, 10)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 || events[0].Sequence != 1 {
		t.Fatalf("unexpected events: %+v", events)
	}
	clearKey := ""
	completed := validationruns.StatusCompleted
	if err := store.UpdateRun(context.Background(), validationruns.RunPatch{
		ID:             run.ID,
		Status:         &completed,
		IdempotencyKey: &clearKey,
		CompletedAt:    &now,
		UpdatedAt:      &now,
	}); err != nil {
		t.Fatalf("update run: %v", err)
	}
	if _, err := store.FindActiveByIdempotencyKey(context.Background(), "dup-1"); err != validationruns.ErrRunNotFound {
		t.Fatalf("expected idempotency key to clear, got %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"status": "ok"})
	if err := store.PutResult(context.Background(), validationruns.RunResult{
		RunID:             run.ID,
		Status:            validationruns.StatusCompleted,
		ConfigFingerprint: "cfg",
		PlanFingerprint:   "plan",
		Result:            payload,
		StoredAt:          now,
	}); err != nil {
		t.Fatalf("put result: %v", err)
	}
	if _, err := store.GetResult(context.Background(), run.ID); err != nil {
		t.Fatalf("get result: %v", err)
	}
	old := now.Add(-2 * time.Hour)
	if err := store.AppendEvent(context.Background(), validationruns.RunEvent{
		RunID:     run.ID,
		Timestamp: old,
		Type:      "old",
		Message:   "old event",
	}); err != nil {
		t.Fatalf("append old event: %v", err)
	}
	if err := store.DeleteEventsBefore(context.Background(), now.Add(-time.Hour)); err != nil {
		t.Fatalf("delete old events: %v", err)
	}
	events, err = store.ListEvents(context.Background(), run.ID, 0, 10)
	if err != nil {
		t.Fatalf("list events after pruning: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("unexpected events after pruning: %+v", events)
	}
}

func TestDurableTaskManagerLifecycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &testSource{output: `{"host-name":"leaf1","software-version":"v24.3.2"}`}
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

	resourceMgr := resources.NewResourceManager(
		map[string]sources.Source{"lab/leaf1/ssh": src},
		nil, nil,
		map[string]tools.Tool{"validate_upgrade": targetTool},
		nil, nil, nil,
	)
	manager, err := validationruns.NewManager(ctx, validationruns.Config{
		ExecutionBackend:      "durabletask",
		StoreBackend:          "sqlite",
		SQLitePath:            filepath.Join(t.TempDir(), "runs.sqlite"),
		DurableTaskSQLitePath: filepath.Join(t.TempDir(), "taskhub.sqlite"),
	}, resourceMgr, nil)
	if err != nil {
		t.Fatalf("initialize durabletask manager: %v", err)
	}
	defer func() { _ = manager.Shutdown(context.Background()) }()
	resourceMgr.SetValidationRunManager(manager)

	asyncTool := targetTool.(validation.AsyncRunnable)
	compiled, err := asyncTool.CompileValidationRun(ctx, resourceMgr, parameters.ParamValues{{Name: "phase", Value: "pre"}})
	if err != nil {
		t.Fatalf("compile run: %v", err)
	}
	handle, err := manager.Submit(ctx, validationruns.SubmitRequest{
		Compiled: compiled,
		Executor: asyncTool,
	})
	if err != nil {
		t.Fatalf("submit run: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		record, err := manager.Get(ctx, handle.RunID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if record.Status == validationruns.StatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("durable validation run did not complete: %+v", record)
		}
		time.Sleep(25 * time.Millisecond)
	}
	if _, err := manager.GetResult(ctx, handle.RunID); err != nil {
		t.Fatalf("get durable result: %v", err)
	}
	events, err := manager.ListEvents(ctx, handle.RunID, 0, 100)
	if err != nil {
		t.Fatalf("list durable events: %v", err)
	}
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

func TestFreezeOnSubmitSurvivesToolRemoval(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &testSource{output: `{"host-name":"leaf1","software-version":"v24.3.2"}`}
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

	for _, backend := range []validationruns.Config{
		{ExecutionBackend: "local", StoreBackend: "memory"},
		{
			ExecutionBackend:      "durabletask",
			StoreBackend:          "sqlite",
			SQLitePath:            filepath.Join(t.TempDir(), "runs.sqlite"),
			DurableTaskSQLitePath: filepath.Join(t.TempDir(), "taskhub.sqlite"),
		},
	} {
		resourceMgr := resources.NewResourceManager(
			map[string]sources.Source{"lab/leaf1/ssh": src},
			nil, nil,
			map[string]tools.Tool{"validate_upgrade": targetTool},
			nil, nil, nil,
		)
		manager, err := validationruns.NewManager(ctx, backend, resourceMgr, nil)
		if err != nil {
			t.Fatalf("initialize manager for backend %+v: %v", backend, err)
		}
		resourceMgr.SetValidationRunManager(manager)

		asyncTool := targetTool.(validation.AsyncRunnable)
		compiled, err := asyncTool.CompileValidationRun(ctx, resourceMgr, parameters.ParamValues{{Name: "phase", Value: "pre"}})
		if err != nil {
			t.Fatalf("compile run: %v", err)
		}
		handle, err := manager.Submit(ctx, validationruns.SubmitRequest{
			Compiled: compiled,
			Executor: asyncTool,
		})
		if err != nil {
			t.Fatalf("submit run: %v", err)
		}

		resourceMgr.SetResources(
			map[string]sources.Source{"lab/leaf1/ssh": src},
			nil, nil,
			map[string]tools.Tool{},
			nil, nil, nil,
		)

		deadline := time.Now().Add(5 * time.Second)
		for {
			record, err := manager.Get(ctx, handle.RunID)
			if err != nil {
				t.Fatalf("get run: %v", err)
			}
			if record.Status == validationruns.StatusCompleted {
				break
			}
			if record.Status == validationruns.StatusFailed {
				t.Fatalf("run failed after tool removal on backend %+v: %+v", backend, record)
			}
			if time.Now().After(deadline) {
				t.Fatalf("run did not complete on backend %+v: %+v", backend, record)
			}
			time.Sleep(25 * time.Millisecond)
		}
		_ = manager.Shutdown(context.Background())
	}
}

func TestExecutionBackendMemoryAliasRejected(t *testing.T) {
	_, err := validationruns.NewManager(context.Background(), validationruns.Config{
		ExecutionBackend: "memory",
		StoreBackend:     "memory",
	}, resources.NewResourceManager(nil, nil, nil, nil, nil, nil, nil), validationruns.NewMemoryStore())
	if err == nil {
		t.Fatalf("expected execution backend alias to be rejected")
	}
}

func TestFailedCollectionProducesFailedRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &testSource{err: fmt.Errorf("device unreachable")}
	targetCfg := validate.Config{
		Name:   "validate_upgrade",
		Type:   "validate",
		Source: "lab/leaf1/ssh",
		Phases: []validate.Phase{{
			Name: "pre",
			Steps: []validate.Step{
				{
					Name: "collect",
					Collect: &validate.CollectSpec{
						Into: "version", Command: "show version | as json",
					},
				},
				{
					Name: "collect_next",
					Collect: &validate.CollectSpec{
						Into: "version_next", Command: "show system information | as json",
					},
				},
			},
		}},
	}
	targetTool, err := targetCfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize target tool: %v", err)
	}

	resourceMgr := resources.NewResourceManager(
		map[string]sources.Source{"lab/leaf1/ssh": src},
		nil, nil,
		map[string]tools.Tool{"validate_upgrade": targetTool},
		nil, nil, nil,
	)
	asyncTool := targetTool.(validation.AsyncRunnable)
	compiled, err := asyncTool.CompileValidationRun(ctx, resourceMgr, parameters.ParamValues{{Name: "phase", Value: "pre"}})
	if err != nil {
		t.Fatalf("compile run: %v", err)
	}

	cases := []struct {
		name string
		cfg  validationruns.Config
	}{
		{
			name: "local",
			cfg:  validationruns.Config{ExecutionBackend: "local"},
		},
		{
			name: "durabletask",
			cfg: validationruns.Config{
				ExecutionBackend:      "durabletask",
				StoreBackend:          "sqlite",
				SQLitePath:            filepath.Join(t.TempDir(), "runs.sqlite"),
				DurableTaskSQLitePath: filepath.Join(t.TempDir(), "taskhub.sqlite"),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			manager, err := validationruns.NewManager(ctx, tc.cfg, resourceMgr, nil)
			if err != nil {
				t.Fatalf("initialize manager: %v", err)
			}
			defer func() { _ = manager.Shutdown(context.Background()) }()

			handle, err := manager.Submit(ctx, validationruns.SubmitRequest{Compiled: compiled, Executor: asyncTool})
			if err != nil {
				t.Fatalf("submit run: %v", err)
			}

			deadline := time.Now().Add(5 * time.Second)
			for {
				record, err := manager.Get(ctx, handle.RunID)
				if err != nil {
					t.Fatalf("get run: %v", err)
				}
				if record.Status == validationruns.StatusFailed {
					result, err := manager.GetResult(ctx, handle.RunID)
					if err != nil {
						t.Fatalf("get result: %v", err)
					}
					var payload struct {
						Status string `json:"status"`
					}
					if err := json.Unmarshal(result.Result, &payload); err != nil {
						t.Fatalf("decode result: %v", err)
					}
					if payload.Status != "fail" {
						t.Fatalf("unexpected validation result status: %+v", payload)
					}
					events, err := manager.ListEvents(ctx, handle.RunID, 0, 20)
					if err != nil {
						t.Fatalf("list events: %v", err)
					}
					started := 0
					for _, event := range events {
						if event.Type == "step_started" {
							started++
						}
					}
					if started != 1 {
						t.Fatalf("expected exactly one step to start before failure, got %+v", events)
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("run did not fail: %+v", record)
				}
				time.Sleep(25 * time.Millisecond)
			}
		})
	}
}

func TestDurableShutdownCancelsInFlightActivity(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	wait := make(chan struct{})
	src := &testSource{wait: wait}
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
	resourceMgr := resources.NewResourceManager(
		map[string]sources.Source{"lab/leaf1/ssh": src},
		nil, nil,
		map[string]tools.Tool{"validate_upgrade": targetTool},
		nil, nil, nil,
	)
	manager, err := validationruns.NewManager(ctx, validationruns.Config{
		ExecutionBackend:      "durabletask",
		StoreBackend:          "sqlite",
		SQLitePath:            filepath.Join(t.TempDir(), "runs.sqlite"),
		DurableTaskSQLitePath: filepath.Join(t.TempDir(), "taskhub.sqlite"),
	}, resourceMgr, nil)
	if err != nil {
		t.Fatalf("initialize durable manager: %v", err)
	}

	asyncTool := targetTool.(validation.AsyncRunnable)
	compiled, err := asyncTool.CompileValidationRun(ctx, resourceMgr, parameters.ParamValues{{Name: "phase", Value: "pre"}})
	if err != nil {
		t.Fatalf("compile run: %v", err)
	}
	handle, err := manager.Submit(ctx, validationruns.SubmitRequest{Compiled: compiled, Executor: asyncTool})
	if err != nil {
		t.Fatalf("submit run: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		events, err := manager.ListEvents(ctx, handle.RunID, 0, 20)
		if err != nil {
			t.Fatalf("list events: %v", err)
		}
		sawStep := false
		for _, event := range events {
			if event.Type == "step_started" {
				sawStep = true
				break
			}
		}
		if sawStep {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("step did not start in time")
		}
		time.Sleep(25 * time.Millisecond)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutdownCancel()
	if err := manager.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

func TestCollectRetryEmitsStepRetryScheduled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	src := &retryingTestSource{
		steps: []retryingTestStep{
			{err: fmt.Errorf("dial tcp 192.0.2.1:22: i/o timeout")},
			{output: `{"software-version":"v24.3.2"}`},
		},
	}
	targetCfg := validate.Config{
		Name:   "validate_upgrade",
		Type:   "validate",
		Source: "lab/leaf1/ssh",
		Phases: []validate.Phase{{
			Name: "pre",
			Steps: []validate.Step{
				{Name: "collect", Collect: &validate.CollectSpec{
					Into:    "version",
					Command: "show version | as json",
					Retry: &validate.RetryPolicy{
						Interval:    "1ms",
						MaxAttempts: 2,
					},
				}},
			},
		}},
	}
	targetTool, err := targetCfg.Initialize(map[string]sources.Source{"lab/leaf1/ssh": src})
	if err != nil {
		t.Fatalf("initialize target tool: %v", err)
	}
	resourceMgr := resources.NewResourceManager(
		map[string]sources.Source{"lab/leaf1/ssh": src},
		nil, nil,
		map[string]tools.Tool{"validate_upgrade": targetTool},
		nil, nil, nil,
	)
	manager, err := validationruns.NewManager(ctx, validationruns.Config{ExecutionBackend: "durabletask", StoreBackend: "sqlite", SQLitePath: filepath.Join(t.TempDir(), "runs.sqlite"), DurableTaskSQLitePath: filepath.Join(t.TempDir(), "taskhub.sqlite")}, resourceMgr, nil)
	if err != nil {
		t.Fatalf("initialize manager: %v", err)
	}
	defer func() { _ = manager.Shutdown(context.Background()) }()

	asyncTool := targetTool.(validation.AsyncRunnable)
	compiled, err := asyncTool.CompileValidationRun(ctx, resourceMgr, parameters.ParamValues{{Name: "phase", Value: "pre"}})
	if err != nil {
		t.Fatalf("compile run: %v", err)
	}
	handle, err := manager.Submit(ctx, validationruns.SubmitRequest{Compiled: compiled, Executor: asyncTool})
	if err != nil {
		t.Fatalf("submit run: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		record, err := manager.Get(ctx, handle.RunID)
		if err != nil {
			t.Fatalf("get run: %v", err)
		}
		if record.Status == validationruns.StatusCompleted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("run did not complete: %+v", record)
		}
		time.Sleep(25 * time.Millisecond)
	}
	events, err := manager.ListEvents(ctx, handle.RunID, 0, 20)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	sawRetry := false
	for _, event := range events {
		if event.Type == "step_retry_scheduled" {
			sawRetry = true
			break
		}
	}
	if !sawRetry {
		t.Fatalf("expected retry scheduling event, got %+v", events)
	}
}
