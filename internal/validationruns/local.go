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
	"github.com/google/uuid"
)

type localManager struct {
	cfg           Config
	provider      validation.RuntimeProvider
	store         Store
	maxConcurrent int
	ctx           context.Context
	cancel        context.CancelFunc
	jobs          chan queuedRun
	wg            sync.WaitGroup
	mu            sync.Mutex
	cancelFns     map[string]context.CancelFunc
}

type queuedRun struct {
	id          string
	compiled    validation.CompiledRun
	executor    validation.AsyncRunnable
	accessToken tools.AccessToken
}

func newLocalManager(ctx context.Context, cfg Config, provider validation.RuntimeProvider, store Store) *localManager {
	runCtx, cancel := context.WithCancel(ctx)
	maxConcurrent := cfg.MaxConcurrentRuns
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	m := &localManager{
		cfg:           cfg,
		provider:      provider,
		store:         store,
		maxConcurrent: maxConcurrent,
		ctx:           runCtx,
		cancel:        cancel,
		jobs:          make(chan queuedRun, maxConcurrent*4),
		cancelFns:     make(map[string]context.CancelFunc),
	}
	_ = store.MarkRunningInterrupted(runCtx, "nocfoundry-restart")
	for i := 0; i < maxConcurrent; i++ {
		m.wg.Add(1)
		go m.worker()
	}
	if cfg.ResultRetention > 0 || cfg.EventRetention > 0 {
		m.wg.Add(1)
		go m.cleanupLoop()
	}
	return m
}

func (m *localManager) Submit(ctx context.Context, req SubmitRequest) (RunHandle, error) {
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

	runID := uuid.NewString()
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

	m.jobs <- queuedRun{id: runID, compiled: req.Compiled, executor: req.Executor, accessToken: req.AccessToken}
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

func (m *localManager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case job := <-m.jobs:
			m.executeJob(job)
		}
	}
}

func (m *localManager) executeJob(job queuedRun) {
	runCtx, cancel := context.WithCancel(m.ctx)
	m.mu.Lock()
	m.cancelFns[job.id] = cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.cancelFns, job.id)
		m.mu.Unlock()
	}()

	started := time.Now().UTC()
	summary := "validation run started"
	markRunRunning(runCtx, m.store, job.id, started, summary)

	runtime, err := validation.ResolveCompiledRuntime(job.compiled)
	if err != nil {
		completed := time.Now().UTC()
		msg := err.Error()
		failed := StatusFailed
		_ = markRunTerminal(runCtx, m.store, m.cfg, job.id, failed, "", msg, completed)
		appendRunEvent(runCtx, m.store, RunEvent{
			RunID:     job.id,
			Timestamp: completed,
			Type:      "failed",
			Message:   msg,
		})
		return
	}

	evidence := make(map[string]json.RawMessage)
	stepResults := make([]json.RawMessage, 0, len(job.compiled.Steps))
	var result any
	var tbErr error
	terminalStop := false
runLoop:
	for _, ref := range job.compiled.Steps {
		input := validation.StepExecutionInput{
			Attempt:  1,
			Evidence: evidence,
		}
		for {
			now := time.Now().UTC()
			updateStepExecutionState(runCtx, m.store, job.id, ref, input.Attempt, now)
			output, stepErr := runtime.ExecuteCompiledStep(runCtx, m.provider, job.compiled, ref, input, job.accessToken)
			if stepErr != nil {
				appendStepFailed(runCtx, m.store, job.id, ref, input.Attempt, time.Now().UTC(), stepErr.Error())
				tbErr = stepErr
				break runLoop
			}
			for name, raw := range output.EvidenceDelta {
				evidence[name] = raw
			}
			completed := time.Now().UTC()
			appendStepCompleted(runCtx, m.store, job.id, ref, input.Attempt, completed, output.StepResult)
			if output.StepCompleted {
				stepResults = append(stepResults, output.StepResult)
				if output.FailFastStop || output.Terminal {
					terminalStop = true
					break runLoop
				}
				break
			}
			appendStepRetryScheduled(runCtx, m.store, job.id, ref, input.Attempt, completed, output.RetryAfter)
			wait, err := time.ParseDuration(output.RetryAfter)
			if err != nil {
				tbErr = fmt.Errorf("failed to parse retry interval: %w", err)
				break runLoop
			}
			if !sleepWithContext(runCtx, wait) {
				break runLoop
			}
			input.Attempt++
			input.Evidence = evidence
			input.ConvergenceState = output.ConvergenceState
		}
	}
	if tbErr == nil && !errors.Is(runCtx.Err(), context.Canceled) && (len(stepResults) > 0 || terminalStop || len(job.compiled.Steps) == 0) {
		result, tbErr = runtime.AssembleCompiledValidationResult(runCtx, m.provider, job.compiled, evidence, stepResults)
	}
	completed := time.Now().UTC()
	if errors.Is(runCtx.Err(), context.Canceled) {
		cancelled := StatusCancelled
		summary := "validation run cancelled"
		_ = markRunTerminal(runCtx, m.store, m.cfg, job.id, cancelled, "", summary, completed)
		appendRunEvent(context.Background(), m.store, RunEvent{
			RunID:     job.id,
			Timestamp: completed,
			Type:      "cancelled",
			Message:   summary,
		})
		return
	}

	if tbErr != nil {
		failed := StatusFailed
		msg := tbErr.Error()
		_ = markRunTerminal(runCtx, m.store, m.cfg, job.id, failed, "", msg, completed)
		appendRunEvent(context.Background(), m.store, RunEvent{
			RunID:     job.id,
			Timestamp: completed,
			Type:      "failed",
			Message:   msg,
		})
		return
	}

	raw, err := json.Marshal(result)
	if err != nil {
		failed := StatusFailed
		msg := fmt.Sprintf("failed to marshal validation result: %v", err)
		_ = markRunTerminal(runCtx, m.store, m.cfg, job.id, failed, "", msg, completed)
		appendRunEvent(context.Background(), m.store, RunEvent{
			RunID:     job.id,
			Timestamp: completed,
			Type:      "failed",
			Message:   msg,
		})
		return
	}
	if record, err := m.store.GetRun(runCtx, job.id); err == nil && record.CancellationRequested {
		cancelled := StatusCancelled
		summary := "validation run cancelled"
		_ = markRunTerminal(runCtx, m.store, m.cfg, job.id, cancelled, "", summary, completed)
		appendRunEvent(context.Background(), m.store, RunEvent{
			RunID:     job.id,
			Timestamp: completed,
			Type:      "cancelled",
			Message:   summary,
		})
		return
	}
	completedStatus := StatusCompleted
	completedStatus = extractValidationStatus(raw)
	summary = extractValidationSummary(raw)
	if summary == "" {
		if completedStatus == StatusFailed {
			summary = "validation run failed"
		} else {
			summary = "validation run completed"
		}
	}
	outcome := extractOutcome(raw)
	resultRecord := RunResult{
		RunID:             job.id,
		Status:            completedStatus,
		Outcome:           outcome,
		ConfigFingerprint: job.compiled.ConfigFingerprint,
		PlanFingerprint:   job.compiled.PlanFingerprint,
		Result:            raw,
		StoredAt:          completed,
	}
	_ = m.store.PutResult(runCtx, resultRecord)
	_ = markRunTerminal(runCtx, m.store, m.cfg, job.id, completedStatus, outcome, summary, completed)
	appendRunEvent(runCtx, m.store, RunEvent{
		RunID:     job.id,
		Timestamp: completed,
		Type:      string(completedStatus),
		Message:   summary,
	})
}

func (m *localManager) cleanupLoop() {
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

func (m *localManager) Get(ctx context.Context, runID string) (RunRecord, error) {
	return m.store.GetRun(ctx, runID)
}

func (m *localManager) GetResult(ctx context.Context, runID string) (RunResult, error) {
	return m.store.GetResult(ctx, runID)
}

func (m *localManager) ListEvents(ctx context.Context, runID string, after int64, limit int) ([]RunEvent, error) {
	return m.store.ListEvents(ctx, runID, after, limit)
}

func (m *localManager) Cancel(ctx context.Context, runID, reason string) error {
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

	m.mu.Lock()
	cancel, ok := m.cancelFns[runID]
	m.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

func (m *localManager) Shutdown(ctx context.Context) error {
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
		return nil
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
