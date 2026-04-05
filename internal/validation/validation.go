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

package validation

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

type CompiledRun struct {
	RunType           string          `json:"run_type"`
	ToolName          string          `json:"tool_name"`
	ToolType          string          `json:"tool_type"`
	Phase             string          `json:"phase,omitempty"`
	Steps             []StepRef       `json:"steps,omitempty"`
	SubmittedParams   map[string]any  `json:"submitted_params"`
	ResourceVersion   uint64          `json:"resource_version"`
	ConfigFingerprint string          `json:"config_fingerprint"`
	PlanFingerprint   string          `json:"plan_fingerprint"`
	PayloadVersion    string          `json:"payload_version"`
	Payload           json.RawMessage `json:"payload"`
	CreatedAt         time.Time       `json:"created_at"`
}

type RunSnapshot struct {
	RunID      string      `json:"run_id"`
	Status     string      `json:"status"`
	Compiled   CompiledRun `json:"compiled"`
	StartedAt  *time.Time  `json:"started_at,omitempty"`
	FinishedAt *time.Time  `json:"finished_at,omitempty"`
}

type ExecutionState struct {
	CurrentStage string `json:"current_stage,omitempty"`
	CurrentStep  string `json:"current_step,omitempty"`
	Attempt      int    `json:"attempt,omitempty"`
}

type StepRef struct {
	Phase string `json:"phase"`
	Index int    `json:"index"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
}

type StepExecutionInput struct {
	Attempt          int                        `json:"attempt"`
	Evidence         map[string]json.RawMessage `json:"evidence,omitempty"`
	ConvergenceState *ConvergenceState          `json:"convergence_state,omitempty"`
}

type StepExecutionOutput struct {
	StepResult       json.RawMessage            `json:"step_result"`
	EvidenceDelta    map[string]json.RawMessage `json:"evidence_delta,omitempty"`
	Terminal         bool                       `json:"terminal,omitempty"`
	FailFastStop     bool                       `json:"fail_fast_stop,omitempty"`
	StepCompleted    bool                       `json:"step_completed,omitempty"`
	RetryAfter       string                     `json:"retry_after,omitempty"`
	ConvergenceState *ConvergenceState          `json:"convergence_state,omitempty"`
}

type ConvergenceState struct {
	Attempt          int             `json:"attempt"`
	PassStreak       int             `json:"pass_streak"`
	ObservedFailures int             `json:"observed_failures"`
	StartedAt        time.Time       `json:"started_at"`
	FirstPassAt      *time.Time      `json:"first_pass_at,omitempty"`
	LastPassedAt     *time.Time      `json:"last_passed_at,omitempty"`
	LastActual       json.RawMessage `json:"last_actual,omitempty"`
	Completed        bool            `json:"completed,omitempty"`
	TimeoutReached   bool            `json:"timeout_reached,omitempty"`
}

type AsyncRunnable interface {
	CompileValidationRun(context.Context, tools.SourceProvider, parameters.ParamValues) (CompiledRun, error)
	ExecuteCompiledValidation(context.Context, tools.SourceProvider, CompiledRun, tools.AccessToken) (any, util.NOCFoundryError)
}

type StepExecutable interface {
	ExecuteCompiledStep(context.Context, tools.SourceProvider, CompiledRun, StepRef, StepExecutionInput, tools.AccessToken) (StepExecutionOutput, util.NOCFoundryError)
}

type ResultAssembler interface {
	AssembleCompiledValidationResult(context.Context, tools.SourceProvider, CompiledRun, map[string]json.RawMessage, []json.RawMessage) (any, util.NOCFoundryError)
}

type CompiledRuntime interface {
	StepExecutable
	ResultAssembler
}

type CompiledRuntimeFactory func(CompiledRun) (CompiledRuntime, error)

var (
	compiledRuntimeRegistryMu sync.RWMutex
	compiledRuntimeRegistry   = map[string]CompiledRuntimeFactory{}
)

func RegisterCompiledRuntime(toolType string, factory CompiledRuntimeFactory) bool {
	compiledRuntimeRegistryMu.Lock()
	defer compiledRuntimeRegistryMu.Unlock()
	if _, exists := compiledRuntimeRegistry[toolType]; exists {
		return false
	}
	compiledRuntimeRegistry[toolType] = factory
	return true
}

func ResolveCompiledRuntime(compiled CompiledRun) (CompiledRuntime, error) {
	compiledRuntimeRegistryMu.RLock()
	factory, ok := compiledRuntimeRegistry[compiled.ToolType]
	compiledRuntimeRegistryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no compiled runtime registered for tool type %q", compiled.ToolType)
	}
	return factory(compiled)
}

type RuntimeProvider interface {
	tools.SourceProvider
	GetTool(string) (tools.Tool, bool)
	GetEmbeddingModelMap() map[string]embeddingmodels.EmbeddingModel
	GetResourceVersion() uint64
}
