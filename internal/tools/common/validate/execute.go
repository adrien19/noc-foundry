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

// Package validate implements the validate MCP tool, a read-only
// declarative validation engine for network devices and blast-radius checks.

package validate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validation"
)

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	compiled, err := t.CompileValidationRun(ctx, resourceMgr, params)
	if err != nil {
		return nil, util.NewClientServerError("failed to compile validation run", http.StatusBadRequest, err)
	}
	return t.ExecuteCompiledValidation(ctx, resourceMgr, compiled, accessToken)
}

func (t Tool) CompileValidationRun(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues) (validation.CompiledRun, error) {
	phase, err := t.resolvePhase(params)
	if err != nil {
		return validation.CompiledRun{}, err
	}
	scope, groups, err := t.resolveScope(ctx, resourceMgr, params)
	if err != nil {
		return validation.CompiledRun{}, err
	}

	payload := compiledRunPayload{
		PhaseName: phase.name,
		Scope:     scope,
		Steps:     make([]compiledRunStep, 0, len(phase.steps)),
	}
	for _, step := range phase.steps {
		next := compiledRunStep{
			Name:        step.Name,
			Description: step.Description,
			FailFast:    step.failFast,
		}
		if step.collect != nil {
			targets, err := selectTargets(step.collect.spec.Targets, groups)
			if err != nil {
				return validation.CompiledRun{}, err
			}
			next.Kind = StepKindCollect
			var retry *compiledRunRetry
			if step.collect.retry != nil {
				retry = &compiledRunRetry{
					Interval:    step.collect.retry.interval.String(),
					Timeout:     step.collect.retry.timeout.String(),
					MaxAttempts: step.collect.retry.maxAttempts,
				}
			}
			next.Collect = &compiledRunCollectStep{
				Spec:  step.collect.spec,
				Mode:  step.collect.mode,
				Plans: serializePlans(buildTransportPlans(targets, step.collect)),
				Retry: retry,
			}
		} else {
			next.Kind = StepKindAssert
			next.Assert = &compiledRunAssertStep{Spec: step.assert.spec}
			if step.converge != nil {
				next.Converge = &compiledRunConverge{
					Until:        step.converge.until,
					Interval:     step.converge.interval.String(),
					Timeout:      step.converge.timeout.String(),
					MaxAttempts:  step.converge.maxAttempts,
					Backoff:      step.converge.backoff,
					StabilizeFor: step.converge.stabilizeFor.String(),
					MinPasses:    step.converge.minPasses,
				}
			}
		}
		payload.Steps = append(payload.Steps, next)
	}

	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return validation.CompiledRun{}, fmt.Errorf("failed to encode compiled validation payload: %w", err)
	}
	configRaw, err := json.Marshal(t.ToConfig())
	if err != nil {
		return validation.CompiledRun{}, fmt.Errorf("failed to encode validation config snapshot: %w", err)
	}
	return validation.CompiledRun{
		RunType:           "validation",
		ToolName:          t.Name,
		ToolType:          t.Type,
		Phase:             payload.PhaseName,
		Steps:             compiledStepRefs(payload),
		SubmittedParams:   params.AsMap(),
		ResourceVersion:   resourceVersionFrom(resourceMgr),
		ConfigFingerprint: fingerprint(configRaw),
		PlanFingerprint:   fingerprint(payloadRaw),
		PayloadVersion:    "validate/v1",
		Payload:           payloadRaw,
		CreatedAt:         time.Now(),
	}, nil
}

func compiledStepRefs(payload compiledRunPayload) []validation.StepRef {
	refs := make([]validation.StepRef, 0, len(payload.Steps))
	for index, step := range payload.Steps {
		refs = append(refs, validation.StepRef{
			Phase: payload.PhaseName,
			Index: index,
			Name:  step.Name,
			Kind:  string(step.Kind),
		})
	}
	return refs
}

func (t Tool) ExecuteCompiledValidation(ctx context.Context, resourceMgr tools.SourceProvider, compiled validation.CompiledRun, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	_ = accessToken
	payload, err := decodeCompiledRunPayload(compiled)
	if err != nil {
		return nil, util.NewClientServerError("failed to decode compiled validation payload", http.StatusInternalServerError, err)
	}
	startedAt := time.Now()
	store := newEvidenceStore()
	stepResults := make([]StepResult, 0, len(payload.Steps))
	for index, step := range payload.Steps {
		ref := validation.StepRef{
			Phase: payload.PhaseName,
			Index: index,
			Name:  step.Name,
			Kind:  string(step.Kind),
		}
		input := validation.StepExecutionInput{
			Attempt:  1,
			Evidence: mapEvidenceStore(store),
		}
		for {
			output, stepErr := t.ExecuteCompiledStep(ctx, resourceMgr, compiled, ref, input, accessToken)
			if stepErr != nil {
				return nil, stepErr
			}
			if err := applyEvidenceDelta(store, output.EvidenceDelta); err != nil {
				return nil, util.NewClientServerError("failed to apply step evidence", http.StatusInternalServerError, err)
			}
			if output.StepCompleted {
				var stepResult StepResult
				if err := json.Unmarshal(output.StepResult, &stepResult); err != nil {
					return nil, util.NewClientServerError("failed to decode step result", http.StatusInternalServerError, err)
				}
				stepResults = append(stepResults, stepResult)
				if output.FailFastStop || output.Terminal {
					break
				}
				goto nextStep
			}
			wait, err := time.ParseDuration(output.RetryAfter)
			if err != nil {
				return nil, util.NewClientServerError("failed to parse retry interval", http.StatusInternalServerError, err)
			}
			if !sleepWithContext(ctx, wait) {
				break
			}
			input.Attempt++
			input.Evidence = mapEvidenceStore(store)
			input.ConvergenceState = output.ConvergenceState
		}
		break
	nextStep:
	}
	result, resultErr := t.assembleCompiledValidationResult(payload, store, stepResults, startedAt, time.Now())
	if resultErr != nil {
		return nil, util.NewClientServerError("failed to assemble validation result", http.StatusInternalServerError, resultErr)
	}
	return result, nil
}

func (t Tool) ExecuteCompiledStep(ctx context.Context, resourceMgr tools.SourceProvider, compiled validation.CompiledRun, ref validation.StepRef, input validation.StepExecutionInput, accessToken tools.AccessToken) (validation.StepExecutionOutput, util.NOCFoundryError) {
	_ = accessToken
	payload, err := decodeCompiledRunPayload(compiled)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("failed to decode compiled validation payload", http.StatusInternalServerError, err)
	}
	if ref.Index < 0 || ref.Index >= len(payload.Steps) {
		return validation.StepExecutionOutput{}, util.NewClientServerError("compiled step index out of range", http.StatusBadRequest, nil)
	}
	step := payload.Steps[ref.Index]
	store, err := evidenceStoreFromMap(input.Evidence)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("failed to decode compiled evidence", http.StatusBadRequest, err)
	}
	if step.Collect != nil {
		attemptCtx, cancel := withCollectAttemptTimeout(ctx, step.Collect.Retry)
		defer cancel()
		stepResult, delta, failureClass, err := t.runCompiledPlanStep(attemptCtx, resourceMgr, step, store)
		if err != nil {
			return validation.StepExecutionOutput{}, util.NewClientServerError("validation step failed", http.StatusBadRequest, err)
		}
		stepRaw, err := json.Marshal(stepResult)
		if err != nil {
			return validation.StepExecutionOutput{}, util.NewClientServerError("failed to encode step result", http.StatusInternalServerError, err)
		}
		stepFailed := stepResult.Status == StatusFail || stepResult.Code == "collection_failed"
		retryable := stepFailed && failureClass == collectFailureRetryable && collectAttemptRemaining(step.Collect.Retry, input.Attempt)
		if retryable {
			return validation.StepExecutionOutput{
				StepResult:    stepRaw,
				EvidenceDelta: nil,
				FailFastStop:  false,
				Terminal:      false,
				StepCompleted: false,
				RetryAfter:    collectRetryInterval(step.Collect.Retry),
			}, nil
		}
		return validation.StepExecutionOutput{
			StepResult:    stepRaw,
			EvidenceDelta: delta,
			FailFastStop:  stepFailed || (step.FailFast && stepFailed),
			Terminal:      stepFailed,
			StepCompleted: true,
		}, nil
	}
	stepResult, convState, retryAfter, completed, err := t.runCompiledAssertAttempt(step, store, input)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("validation step failed", http.StatusBadRequest, err)
	}
	stepRaw, err := json.Marshal(stepResult)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("failed to encode step result", http.StatusInternalServerError, err)
	}
	return validation.StepExecutionOutput{
		StepResult:       stepRaw,
		FailFastStop:     completed && step.FailFast && (stepResult.Status == StatusFail || stepResult.Code == "collection_failed"),
		Terminal:         false,
		StepCompleted:    completed,
		RetryAfter:       retryAfter,
		ConvergenceState: convState,
	}, nil
}

func (t Tool) AssembleCompiledValidationResult(ctx context.Context, resourceMgr tools.SourceProvider, compiled validation.CompiledRun, evidence map[string]json.RawMessage, stepResults []json.RawMessage) (any, util.NOCFoundryError) {
	_ = ctx
	_ = resourceMgr
	payload, err := decodeCompiledRunPayload(compiled)
	if err != nil {
		return nil, util.NewClientServerError("failed to decode compiled validation payload", http.StatusInternalServerError, err)
	}
	store, err := evidenceStoreFromMap(evidence)
	if err != nil {
		return nil, util.NewClientServerError("failed to decode compiled evidence", http.StatusBadRequest, err)
	}
	decodedSteps := make([]StepResult, 0, len(stepResults))
	for _, raw := range stepResults {
		var step StepResult
		if err := json.Unmarshal(raw, &step); err != nil {
			return nil, util.NewClientServerError("failed to decode step result", http.StatusInternalServerError, err)
		}
		decodedSteps = append(decodedSteps, step)
	}
	result, err := t.assembleCompiledValidationResult(payload, store, decodedSteps, compiled.CreatedAt, time.Now())
	if err != nil {
		return nil, util.NewClientServerError("failed to assemble validation result", http.StatusInternalServerError, err)
	}
	return result, nil
}

func decodeCompiledRunPayload(compiled validation.CompiledRun) (compiledRunPayload, error) {
	var payload compiledRunPayload
	if err := json.Unmarshal(compiled.Payload, &payload); err != nil {
		return compiledRunPayload{}, err
	}
	return payload, nil
}
