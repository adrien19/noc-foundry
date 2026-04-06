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

package validationruns

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/validation"
	dtapi "github.com/microsoft/durabletask-go/api"
	dtbackend "github.com/microsoft/durabletask-go/backend"
	dtsqlite "github.com/microsoft/durabletask-go/backend/sqlite"
	dttask "github.com/microsoft/durabletask-go/task"
)

const (
	durableOrchestratorName     = "validationRunOrchestrator"
	durableStepActivityName     = "executeValidationStep"
	durableStartActivityName    = "startValidationRun"
	durableFinalizeActivityName = "finalizeValidationRun"
	durableFailActivityName     = "failValidationRun"
	durableCancelActivityName   = "cancelValidationRun"
	durableCancelEventName      = "validation_cancel_requested"
)

type durableTaskManager struct {
	cfg      Config
	provider validation.RuntimeProvider
	store    Store
	client   dtbackend.TaskHubClient
	worker   dtbackend.TaskHubWorker
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

type durableRunInput struct {
	RunID       string                 `json:"run_id"`
	Compiled    validation.CompiledRun `json:"compiled"`
	AccessToken tools.AccessToken      `json:"access_token"`
}

type durableStepActivityInput struct {
	RunID            string                       `json:"run_id"`
	Compiled         validation.CompiledRun       `json:"compiled"`
	Step             validation.StepRef           `json:"step"`
	Evidence         map[string]json.RawMessage   `json:"evidence,omitempty"`
	Attempt          int                          `json:"attempt"`
	ConvergenceState *validation.ConvergenceState `json:"convergence_state,omitempty"`
	AccessToken      tools.AccessToken            `json:"access_token"`
}

type durableStartInput struct {
	RunID    string                 `json:"run_id"`
	Compiled validation.CompiledRun `json:"compiled"`
}

type durableFinalizeInput struct {
	RunID       string                     `json:"run_id"`
	Compiled    validation.CompiledRun     `json:"compiled"`
	Evidence    map[string]json.RawMessage `json:"evidence,omitempty"`
	StepResults []json.RawMessage          `json:"step_results,omitempty"`
}

type durableFailInput struct {
	RunID     string `json:"run_id"`
	Message   string `json:"message"`
	Cancelled bool   `json:"cancelled,omitempty"`
}

type durableCancelInput struct {
	RunID   string `json:"run_id"`
	Message string `json:"message"`
}

func newDurableTaskManager(ctx context.Context, cfg Config, provider validation.RuntimeProvider, store Store) (Manager, error) {
	logger := dtbackend.DefaultLogger()
	be := dtsqlite.NewSqliteBackend(dtsqlite.NewSqliteOptions(cfg.DurableTaskSQLitePath), logger)
	registry := dttask.NewTaskRegistry()

	managerCtx, cancel := context.WithCancel(ctx)
	m := &durableTaskManager{
		cfg:      cfg,
		provider: provider,
		store:    store,
		ctx:      managerCtx,
		cancel:   cancel,
	}
	if err := registry.AddOrchestratorN(durableOrchestratorName, m.runOrchestration); err != nil {
		cancel()
		return nil, err
	}
	if err := registry.AddActivityN(durableStartActivityName, m.startRunActivity); err != nil {
		cancel()
		return nil, err
	}
	if err := registry.AddActivityN(durableStepActivityName, m.executeStepActivity); err != nil {
		cancel()
		return nil, err
	}
	if err := registry.AddActivityN(durableFinalizeActivityName, m.finalizeRunActivity); err != nil {
		cancel()
		return nil, err
	}
	if err := registry.AddActivityN(durableFailActivityName, m.failRunActivity); err != nil {
		cancel()
		return nil, err
	}
	if err := registry.AddActivityN(durableCancelActivityName, m.cancelRunActivity); err != nil {
		cancel()
		return nil, err
	}

	executor := dttask.NewTaskExecutor(registry)
	orchWorker := dtbackend.NewOrchestrationWorker(be, executor, logger)
	actWorker := dtbackend.NewActivityTaskWorker(be, executor, logger)
	worker := dtbackend.NewTaskHubWorker(be, orchWorker, actWorker, logger)
	if err := worker.Start(managerCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("start durable task worker: %w", err)
	}
	m.worker = worker
	m.client = dtbackend.NewTaskHubClient(be)
	if err := m.reconcileRunStore(managerCtx); err != nil {
		_ = worker.Shutdown(managerCtx)
		cancel()
		return nil, err
	}
	if cfg.ResultRetention > 0 || cfg.EventRetention > 0 {
		m.wg.Add(1)
		go m.cleanupLoop()
	}
	return m, nil
}

func (m *durableTaskManager) Submit(ctx context.Context, req SubmitRequest) (RunHandle, error) {
	if req.Executor == nil {
		return RunHandle{}, fmt.Errorf("validation run executor is required")
	}
	if req.IdempotencyKey != "" {
		record, err := m.store.FindActiveByIdempotencyKey(ctx, req.IdempotencyKey)
		if err == nil {
			return RunHandle{
				RunID:             record.ID,
				Status:            record.Status,
				ToolName:          record.ToolName,
				ConfigFingerprint: record.ConfigFingerprint,
				PlanFingerprint:   record.PlanFingerprint,
				ResourceVersion:   record.ResourceVersion,
				SubmittedAt:       record.CreatedAt,
			}, nil
		}
		if err != nil && err != ErrRunNotFound {
			return RunHandle{}, err
		}
	}

	runID := fmt.Sprintf("%d-%s", time.Now().UTC().UnixNano(), req.Compiled.ToolName)
	now := time.Now().UTC()
	record := RunRecord{
		ID:                runID,
		RunType:           req.Compiled.RunType,
		ToolName:          req.Compiled.ToolName,
		ToolType:          req.Compiled.ToolType,
		Status:            StatusAccepted,
		ResourceVersion:   req.Compiled.ResourceVersion,
		ConfigFingerprint: req.Compiled.ConfigFingerprint,
		PlanFingerprint:   req.Compiled.PlanFingerprint,
		IdempotencyKey:    req.IdempotencyKey,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := m.store.CreateRun(ctx, record); err != nil {
		return RunHandle{}, err
	}
	_ = m.store.AppendEvent(ctx, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "accepted",
		Message:   "validation run accepted",
	})

	input := durableRunInput{RunID: runID, Compiled: req.Compiled, AccessToken: req.AccessToken}
	if _, err := m.client.ScheduleNewOrchestration(ctx, durableOrchestratorName, dtapi.WithInstanceID(dtapi.InstanceID(runID)), dtapi.WithInput(input)); err != nil {
		return RunHandle{}, fmt.Errorf("schedule durable validation run: %w", err)
	}
	return RunHandle{
		RunID:             runID,
		Status:            StatusAccepted,
		ToolName:          req.Compiled.ToolName,
		ConfigFingerprint: req.Compiled.ConfigFingerprint,
		PlanFingerprint:   req.Compiled.PlanFingerprint,
		ResourceVersion:   req.Compiled.ResourceVersion,
		SubmittedAt:       now,
	}, nil
}

func (m *durableTaskManager) Get(ctx context.Context, runID string) (RunRecord, error) {
	return m.store.GetRun(ctx, runID)
}

func (m *durableTaskManager) GetResult(ctx context.Context, runID string) (RunResult, error) {
	return m.store.GetResult(ctx, runID)
}

func (m *durableTaskManager) ListEvents(ctx context.Context, runID string, after int64, limit int) ([]RunEvent, error) {
	return m.store.ListEvents(ctx, runID, after, limit)
}

func (m *durableTaskManager) Cancel(ctx context.Context, runID, reason string) error {
	requested := true
	summary := "cancellation requested"
	if reason != "" {
		summary = reason
	}
	now := time.Now().UTC()
	if err := m.store.UpdateRun(ctx, RunPatch{
		ID:                    runID,
		CancellationRequested: &requested,
		Summary:               &summary,
		UpdatedAt:             &now,
	}); err != nil {
		return err
	}
	_ = m.store.AppendEvent(ctx, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "cancel_requested",
		Message:   summary,
	})
	if err := m.client.RaiseEvent(ctx, dtapi.InstanceID(runID), durableCancelEventName, dtapi.WithEventPayload(map[string]string{"reason": summary})); err != nil {
		if termErr := m.client.TerminateOrchestration(ctx, dtapi.InstanceID(runID), dtapi.WithOutput(map[string]string{"reason": summary})); termErr != nil {
			return fmt.Errorf("signal durable validation run cancellation: %w (terminate fallback failed: %v)", err, termErr)
		}
	}
	return nil
}

func (m *durableTaskManager) Shutdown(ctx context.Context) error {
	m.cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.wg.Wait()
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
	}
	if m.worker == nil {
		return nil
	}
	return m.worker.Shutdown(ctx)
}

func (m *durableTaskManager) runOrchestration(ctx *dttask.OrchestrationContext) (any, error) {
	var input durableRunInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	if err := ctx.CallActivity(durableStartActivityName, dttask.WithActivityInput(durableStartInput{
		RunID:    input.RunID,
		Compiled: input.Compiled,
	})).Await(nil); err != nil {
		return nil, err
	}

	evidence := make(map[string]json.RawMessage)
	stepResults := make([]json.RawMessage, 0, len(input.Compiled.Steps))
	terminalStop := false
	for _, stepRef := range input.Compiled.Steps {
		cancelled, reason, err := waitForCancelEvent(ctx, 0)
		if err != nil {
			_ = ctx.CallActivity(durableFailActivityName, dttask.WithActivityInput(durableFailInput{
				RunID:   input.RunID,
				Message: err.Error(),
			})).Await(nil)
			return nil, err
		}
		if cancelled {
			_ = ctx.CallActivity(durableCancelActivityName, dttask.WithActivityInput(durableCancelInput{
				RunID:   input.RunID,
				Message: reason,
			})).Await(nil)
			return map[string]any{"run_id": input.RunID}, nil
		}
		activityInput := durableStepActivityInput{
			RunID:       input.RunID,
			Compiled:    input.Compiled,
			Step:        stepRef,
			Evidence:    evidence,
			Attempt:     1,
			AccessToken: input.AccessToken,
		}
		for {
			var output validation.StepExecutionOutput
			if err := ctx.CallActivity(durableStepActivityName, dttask.WithActivityInput(activityInput)).Await(&output); err != nil {
				_ = ctx.CallActivity(durableFailActivityName, dttask.WithActivityInput(durableFailInput{
					RunID:   input.RunID,
					Message: err.Error(),
				})).Await(nil)
				return nil, err
			}
			for name, raw := range output.EvidenceDelta {
				evidence[name] = raw
			}
			if output.StepCompleted {
				stepResults = append(stepResults, output.StepResult)
				if output.FailFastStop || output.Terminal {
					terminalStop = true
					goto finalize
				}
				goto nextStep
			}
			retryAfter, err := time.ParseDuration(output.RetryAfter)
			if err != nil {
				_ = ctx.CallActivity(durableFailActivityName, dttask.WithActivityInput(durableFailInput{
					RunID:   input.RunID,
					Message: err.Error(),
				})).Await(nil)
				return nil, err
			}
			cancelled, reason, err = waitForCancelEvent(ctx, retryAfter)
			if err != nil {
				_ = ctx.CallActivity(durableFailActivityName, dttask.WithActivityInput(durableFailInput{
					RunID:   input.RunID,
					Message: err.Error(),
				})).Await(nil)
				return nil, err
			}
			if cancelled {
				_ = ctx.CallActivity(durableCancelActivityName, dttask.WithActivityInput(durableCancelInput{
					RunID:   input.RunID,
					Message: reason,
				})).Await(nil)
				return map[string]any{"run_id": input.RunID}, nil
			}
			activityInput.Attempt++
			activityInput.Evidence = evidence
			activityInput.ConvergenceState = output.ConvergenceState
		}
	nextStep:
	}

finalize:
	if terminalStop || len(stepResults) > 0 || len(input.Compiled.Steps) == 0 {
		if err := ctx.CallActivity(durableFinalizeActivityName, dttask.WithActivityInput(durableFinalizeInput{
			RunID:       input.RunID,
			Compiled:    input.Compiled,
			Evidence:    evidence,
			StepResults: stepResults,
		})).Await(nil); err != nil {
			_ = ctx.CallActivity(durableFailActivityName, dttask.WithActivityInput(durableFailInput{
				RunID:   input.RunID,
				Message: err.Error(),
			})).Await(nil)
			return nil, err
		}
	}
	return map[string]any{"run_id": input.RunID}, nil
}

func (m *durableTaskManager) startRunActivity(ctx dttask.ActivityContext) (any, error) {
	var input durableStartInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "validation run started"
	markRunRunning(ctx.Context(), m.store, input.RunID, now, summary)
	return nil, nil
}

func (m *durableTaskManager) executeStepActivity(ctx dttask.ActivityContext) (any, error) {
	var input durableStepActivityInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	runtime, err := validation.ResolveCompiledRuntime(input.Compiled)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	updateStepExecutionState(ctx.Context(), m.store, input.RunID, input.Step, input.Attempt, now)

	stepCtx, release := joinContexts(ctx.Context(), m.ctx)
	defer release()
	output, tbErr := runtime.ExecuteCompiledStep(stepCtx, m.provider, input.Compiled, input.Step, validation.StepExecutionInput{
		Attempt:          input.Attempt,
		Evidence:         input.Evidence,
		ConvergenceState: input.ConvergenceState,
	}, input.AccessToken)
	if tbErr != nil {
		appendStepFailed(ctx.Context(), m.store, input.RunID, input.Step, input.Attempt, time.Now().UTC(), tbErr.Error())
		return nil, fmt.Errorf("%s", tbErr.Error())
	}
	completed := time.Now().UTC()
	appendStepCompleted(ctx.Context(), m.store, input.RunID, input.Step, input.Attempt, completed, output.StepResult)
	if !output.StepCompleted {
		appendStepRetryScheduled(ctx.Context(), m.store, input.RunID, input.Step, input.Attempt, completed, output.RetryAfter)
	}
	return output, nil
}

func (m *durableTaskManager) finalizeRunActivity(ctx dttask.ActivityContext) (any, error) {
	var input durableFinalizeInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	runtime, err := validation.ResolveCompiledRuntime(input.Compiled)
	if err != nil {
		return nil, err
	}
	result, tbErr := runtime.AssembleCompiledValidationResult(ctx.Context(), m.provider, input.Compiled, input.Evidence, input.StepResults)
	if tbErr != nil {
		return nil, fmt.Errorf("%s", tbErr.Error())
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status := extractValidationStatus(raw)
	summary := extractValidationSummary(raw)
	if summary == "" {
		if status == StatusFailed {
			summary = "validation run failed"
		} else {
			summary = "validation run completed"
		}
	}
	outcome := extractOutcome(raw)
	if err := m.store.PutResult(ctx.Context(), RunResult{
		RunID:             input.RunID,
		Status:            status,
		Outcome:           outcome,
		ConfigFingerprint: input.Compiled.ConfigFingerprint,
		PlanFingerprint:   input.Compiled.PlanFingerprint,
		Result:            raw,
		StoredAt:          now,
	}); err != nil {
		return nil, err
	}
	if err := markRunTerminal(ctx.Context(), m.store, m.cfg, input.RunID, status, outcome, summary, now); err != nil {
		return nil, err
	}
	appendRunEvent(ctx.Context(), m.store, RunEvent{
		RunID:     input.RunID,
		Timestamp: now,
		Type:      string(status),
		Message:   summary,
	})
	return nil, nil
}

func (m *durableTaskManager) failRunActivity(ctx dttask.ActivityContext) (any, error) {
	var input durableFailInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	status := StatusFailed
	if input.Cancelled {
		status = StatusCancelled
	}
	if err := markRunTerminal(ctx.Context(), m.store, m.cfg, input.RunID, status, "", input.Message, now); err != nil {
		return nil, err
	}
	appendRunEvent(ctx.Context(), m.store, RunEvent{
		RunID:     input.RunID,
		Timestamp: now,
		Type:      string(status),
		Message:   input.Message,
	})
	return nil, nil
}

func (m *durableTaskManager) cancelRunActivity(ctx dttask.ActivityContext) (any, error) {
	var input durableCancelInput
	if err := ctx.GetInput(&input); err != nil {
		return nil, err
	}
	if input.Message == "" {
		input.Message = "validation run cancelled"
	}
	now := time.Now().UTC()
	status := StatusCancelled
	if err := markRunTerminal(ctx.Context(), m.store, m.cfg, input.RunID, status, "", input.Message, now); err != nil {
		return nil, err
	}
	appendRunEvent(ctx.Context(), m.store, RunEvent{
		RunID:     input.RunID,
		Timestamp: now,
		Type:      "cancelled",
		Message:   input.Message,
	})
	return nil, nil
}

func waitForCancelEvent(ctx *dttask.OrchestrationContext, timeout time.Duration) (bool, string, error) {
	var payload map[string]string
	err := ctx.WaitForSingleEvent(durableCancelEventName, timeout).Await(&payload)
	if errors.Is(err, dttask.ErrTaskCanceled) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	reason := "validation run cancelled"
	if payload != nil && payload["reason"] != "" {
		reason = payload["reason"]
	}
	return true, reason, nil
}

func (m *durableTaskManager) cleanupLoop() {
	defer m.wg.Done()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			if m.cfg.EventRetention > 0 {
				_ = m.store.DeleteEventsBefore(m.ctx, time.Now().UTC().Add(-m.cfg.EventRetention))
			}
			_ = m.store.DeleteExpired(m.ctx, time.Now().UTC())
		}
	}
}

func (m *durableTaskManager) reconcileRunStore(ctx context.Context) error {
	lister, ok := m.store.(runStatusLister)
	if !ok {
		return nil
	}
	runs, err := lister.ListRunsByStatus(ctx, StatusAccepted, StatusRunning)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, run := range runs {
		metadata, err := m.client.FetchOrchestrationMetadata(ctx, dtapi.InstanceID(run.ID))
		if err != nil || metadata == nil {
			if run.Status == StatusRunning {
				status := StatusInterrupted
				summary := "validation run interrupted by nocfoundry restart"
				clearKey := ""
				_ = m.store.UpdateRun(ctx, RunPatch{
					ID:             run.ID,
					Status:         &status,
					IdempotencyKey: &clearKey,
					Summary:        &summary,
					UpdatedAt:      &now,
					CompletedAt:    &now,
				})
				appendRunEvent(ctx, m.store, RunEvent{
					RunID:     run.ID,
					Timestamp: now,
					Type:      "interrupted",
					Message:   summary,
				})
			}
			continue
		}
		if metadata.IsComplete() {
			continue
		}
		if run.Status != StatusRunning {
			markRunRunning(ctx, m.store, run.ID, now, "validation run started")
		}
	}
	return nil
}

func joinContexts(primary context.Context, secondary context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(primary)
	go func() {
		select {
		case <-primary.Done():
			cancel()
		case <-secondary.Done():
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}
