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
	"time"

	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/validation"
)

type RunStatus string

const (
	StatusAccepted    RunStatus = "accepted"
	StatusRunning     RunStatus = "running"
	StatusCompleted   RunStatus = "completed"
	StatusFailed      RunStatus = "failed"
	StatusCancelled   RunStatus = "cancelled"
	StatusInterrupted RunStatus = "interrupted"
)

type Config struct {
	ExecutionBackend      string
	StoreBackend          string
	SQLitePath            string
	DurableTaskSQLitePath string
	MaxConcurrentRuns     int
	MaxConcurrentSteps    int
	ResultRetention       time.Duration
	EventRetention        time.Duration
}

type SubmitRequest struct {
	Compiled       validation.CompiledRun
	Executor       validation.AsyncRunnable
	AccessToken    tools.AccessToken
	IdempotencyKey string
}

type RunHandle struct {
	RunID             string    `json:"run_id"`
	Status            RunStatus `json:"status"`
	ToolName          string    `json:"tool_name"`
	ConfigFingerprint string    `json:"config_fingerprint"`
	PlanFingerprint   string    `json:"plan_fingerprint"`
	ResourceVersion   uint64    `json:"resource_version"`
	SubmittedAt       time.Time `json:"submitted_at"`
}

type RunRecord struct {
	ID                    string     `json:"id"`
	RunType               string     `json:"run_type"`
	ToolName              string     `json:"tool_name"`
	ToolType              string     `json:"tool_type"`
	Status                RunStatus  `json:"status"`
	Outcome               string     `json:"outcome,omitempty"`
	ResourceVersion       uint64     `json:"resource_version"`
	ConfigFingerprint     string     `json:"config_fingerprint"`
	PlanFingerprint       string     `json:"plan_fingerprint"`
	CurrentStage          string     `json:"current_stage,omitempty"`
	CurrentStep           string     `json:"current_step,omitempty"`
	Attempt               int        `json:"attempt,omitempty"`
	CancellationRequested bool       `json:"cancellation_requested"`
	IdempotencyKey        string     `json:"idempotency_key,omitempty"`
	Summary               string     `json:"summary,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	UpdatedAt             time.Time  `json:"updated_at"`
	CompletedAt           *time.Time `json:"completed_at,omitempty"`
	ExpiresAt             *time.Time `json:"expires_at,omitempty"`
}

type RunPatch struct {
	ID                    string
	Status                *RunStatus
	Outcome               *string
	CurrentStage          *string
	CurrentStep           *string
	Attempt               *int
	CancellationRequested *bool
	IdempotencyKey        *string
	Summary               *string
	StartedAt             *time.Time
	UpdatedAt             *time.Time
	CompletedAt           *time.Time
	ExpiresAt             *time.Time
}

type RunEvent struct {
	RunID     string          `json:"run_id"`
	Sequence  int64           `json:"sequence"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Stage     string          `json:"stage,omitempty"`
	Step      string          `json:"step,omitempty"`
	Attempt   int             `json:"attempt,omitempty"`
	Message   string          `json:"message,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type RunResult struct {
	RunID             string          `json:"run_id"`
	Status            RunStatus       `json:"status"`
	Outcome           string          `json:"outcome,omitempty"`
	ConfigFingerprint string          `json:"config_fingerprint"`
	PlanFingerprint   string          `json:"plan_fingerprint"`
	Result            json.RawMessage `json:"result"`
	StoredAt          time.Time       `json:"stored_at"`
}

type Provider interface {
	GetValidationRunManager() Manager
	GetResourceVersion() uint64
}

type Manager interface {
	Submit(context.Context, SubmitRequest) (RunHandle, error)
	Get(context.Context, string) (RunRecord, error)
	GetResult(context.Context, string) (RunResult, error)
	ListEvents(context.Context, string, int64, int) ([]RunEvent, error)
	Cancel(context.Context, string, string) error
	Shutdown(context.Context) error
}

type Store interface {
	CreateRun(context.Context, RunRecord) error
	UpdateRun(context.Context, RunPatch) error
	AppendEvent(context.Context, RunEvent) error
	PutResult(context.Context, RunResult) error
	GetRun(context.Context, string) (RunRecord, error)
	GetResult(context.Context, string) (RunResult, error)
	ListEvents(context.Context, string, int64, int) ([]RunEvent, error)
	FindActiveByIdempotencyKey(context.Context, string) (RunRecord, error)
	MarkRunningInterrupted(context.Context, string) error
	DeleteEventsBefore(context.Context, time.Time) error
	DeleteExpired(context.Context, time.Time) error
}

var ErrRunNotFound = errors.New("validation run not found")

func NewManager(ctx context.Context, cfg Config, provider validation.RuntimeProvider, store Store) (Manager, error) {
	storeBackend := cfg.StoreBackend
	if storeBackend == "" {
		if cfg.SQLitePath != "" {
			storeBackend = "sqlite"
		} else {
			storeBackend = "memory"
		}
	}
	if store == nil {
		switch storeBackend {
		case "memory":
			store = NewMemoryStore()
		case "sqlite":
			sqlStore, err := NewSQLiteStore(cfg.SQLitePath)
			if err != nil {
				return nil, err
			}
			store = sqlStore
		default:
			return nil, fmt.Errorf("unknown validation run store backend %q", storeBackend)
		}
	}

	executionBackend := cfg.ExecutionBackend
	if executionBackend == "" {
		executionBackend = "local"
	}
	switch executionBackend {
	case "local":
		return newLocalManager(ctx, cfg, provider, store), nil
	case "durabletask":
		if storeBackend != "sqlite" && cfg.StoreBackend != "sqlite" && cfg.SQLitePath == "" {
			return nil, fmt.Errorf("validation run backend %q requires store backend %q", executionBackend, "sqlite")
		}
		if cfg.DurableTaskSQLitePath == "" {
			return nil, fmt.Errorf("validation run backend %q requires durable task sqlite path", executionBackend)
		}
		return newDurableTaskManager(ctx, cfg, provider, store)
	default:
		return nil, fmt.Errorf("unknown validation run execution backend %q", executionBackend)
	}
}

type runStatusLister interface {
	ListRunsByStatus(context.Context, ...RunStatus) ([]RunRecord, error)
}

func resultExpiresAt(now time.Time, retention time.Duration) *time.Time {
	if retention <= 0 {
		return nil
	}
	expires := now.Add(retention)
	return &expires
}

func appendRunEvent(ctx context.Context, store Store, event RunEvent) {
	_ = store.AppendEvent(ctx, event)
}

func markRunRunning(ctx context.Context, store Store, runID string, now time.Time, summary string) {
	status := StatusRunning
	_ = store.UpdateRun(ctx, RunPatch{
		ID:        runID,
		Status:    &status,
		Summary:   &summary,
		StartedAt: &now,
		UpdatedAt: &now,
	})
	appendRunEvent(ctx, store, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "running",
		Message:   summary,
	})
}

func updateStepExecutionState(ctx context.Context, store Store, runID string, ref validation.StepRef, attempt int, now time.Time) {
	_ = store.UpdateRun(ctx, RunPatch{
		ID:           runID,
		CurrentStage: &ref.Phase,
		CurrentStep:  &ref.Name,
		Attempt:      &attempt,
		UpdatedAt:    &now,
	})
	appendRunEvent(ctx, store, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "step_started",
		Stage:     ref.Phase,
		Step:      ref.Name,
		Attempt:   attempt,
		Message:   "executing " + ref.Kind + " step",
	})
}

func appendStepCompleted(ctx context.Context, store Store, runID string, ref validation.StepRef, attempt int, now time.Time, payload json.RawMessage) {
	appendRunEvent(ctx, store, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "step_completed",
		Stage:     ref.Phase,
		Step:      ref.Name,
		Attempt:   attempt,
		Message:   "step completed",
		Payload:   payload,
	})
}

func appendStepRetryScheduled(ctx context.Context, store Store, runID string, ref validation.StepRef, attempt int, now time.Time, retryAfter string) {
	raw, _ := json.Marshal(map[string]any{"retry_after": retryAfter, "next_attempt": attempt + 1})
	appendRunEvent(ctx, store, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "step_retry_scheduled",
		Stage:     ref.Phase,
		Step:      ref.Name,
		Attempt:   attempt,
		Message:   "step retry scheduled",
		Payload:   raw,
	})
}

func appendStepFailed(ctx context.Context, store Store, runID string, ref validation.StepRef, attempt int, now time.Time, message string) {
	appendRunEvent(ctx, store, RunEvent{
		RunID:     runID,
		Timestamp: now,
		Type:      "step_failed",
		Stage:     ref.Phase,
		Step:      ref.Name,
		Attempt:   attempt,
		Message:   message,
	})
}

func extractOutcome(raw json.RawMessage) string {
	var partial struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(raw, &partial); err != nil {
		return ""
	}
	return partial.Outcome
}

func extractValidationStatus(raw json.RawMessage) RunStatus {
	var partial struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &partial); err != nil {
		return StatusCompleted
	}
	if partial.Status == "fail" {
		return StatusFailed
	}
	return StatusCompleted
}

func extractValidationSummary(raw json.RawMessage) string {
	var partial struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(raw, &partial); err != nil {
		return ""
	}
	return partial.Summary
}

func markRunTerminal(ctx context.Context, store Store, cfg Config, runID string, status RunStatus, outcome, summary string, now time.Time) error {
	clearKey := ""
	expiresAt := resultExpiresAt(now, cfg.ResultRetention)
	return store.UpdateRun(ctx, RunPatch{
		ID:             runID,
		Status:         &status,
		Outcome:        &outcome,
		IdempotencyKey: &clearKey,
		Summary:        &summary,
		UpdatedAt:      &now,
		CompletedAt:    &now,
		ExpiresAt:      expiresAt,
	})
}
