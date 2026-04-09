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
	"fmt"
	"slices"
	"time"
)

func (t Tool) assembleCompiledValidationResult(payload compiledRunPayload, store *EvidenceStore, stepResults []StepResult, startedAt, completedAt time.Time) (Result, error) {
	result := Result{
		ToolName:    t.Name,
		Phase:       payload.PhaseName,
		Scope:       payload.Scope,
		Steps:       stepResults,
		Evidence:    store.All(),
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}
	result.Status, result.Outcome, result.Blocking = classifyResult(stepResults)
	result.Diagnostics = buildDiagnostics(stepResults, result.Evidence)
	result.Recommendations = collectRecommendations(stepResults)
	result.Summary = fmt.Sprintf("phase %q completed with status %s", payload.PhaseName, result.Status)
	return result, nil
}

func classifyResult(steps []StepResult) (ValidationStatus, ValidationOutcome, bool) {
	hasCollectionFailure := false
	hasWarningFailure := false
	hasErrorFailure := false
	hasConvergingWarning := false

	for _, step := range steps {
		if step.Kind == StepKindCollect && step.Status != StatusPass {
			hasCollectionFailure = true
		}
		for _, assertion := range step.Assertions {
			if assertion.Status == StatusPass {
				continue
			}
			switch assertion.Severity {
			case SeverityWarning, SeverityInfo:
				hasWarningFailure = true
				if step.Convergence != nil && step.Convergence.TimeoutReached {
					hasConvergingWarning = true
				}
			default:
				hasErrorFailure = true
			}
		}
	}

	switch {
	case hasErrorFailure:
		return StatusFail, OutcomeBlocked, true
	case hasCollectionFailure:
		return StatusFail, OutcomeBlocked, true
	case hasConvergingWarning:
		return StatusPartial, OutcomeConverging, false
	case hasWarningFailure:
		return StatusPartial, OutcomeWarning, false
	default:
		return StatusPass, OutcomeHealthy, false
	}
}

func buildDiagnostics(steps []StepResult, evidence []Evidence) ResultDiagnostics {
	diag := ResultDiagnostics{TotalSteps: len(steps)}
	for _, step := range steps {
		switch step.Status {
		case StatusPass:
			diag.PassedSteps++
		case StatusFail:
			diag.FailedSteps++
		case StatusPartial:
			diag.WarningSteps++
		}
	}
	for _, ev := range evidence {
		diag.CollectedRecords += ev.Summary.Total
		diag.CollectionErrors += ev.Summary.Failed
	}
	return diag
}

func collectRecommendations(steps []StepResult) []string {
	out := []string{}
	for _, step := range steps {
		if step.Recommendation != "" && !slices.Contains(out, step.Recommendation) {
			out = append(out, step.Recommendation)
		}
		for _, assertion := range step.Assertions {
			if assertion.Recommendation != "" && !slices.Contains(out, assertion.Recommendation) {
				out = append(out, assertion.Recommendation)
			}
		}
	}
	return out
}
