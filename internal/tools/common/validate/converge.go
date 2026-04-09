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
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/validation"
)

func (t Tool) runCompiledAssertStep(ctx context.Context, step compiledRunStep, store *EvidenceStore) (StepResult, error) {
	startedAt := time.Now()
	passStreak := 0
	observedFailures := 0
	var firstPassAt *time.Time
	var lastPassedAt *time.Time
	var lastActual any
	var last AssertionResult
	maxAttempts := 1
	compiledConverge := step.Converge.toCompiled()
	if compiledConverge != nil {
		maxAttempts = compiledConverge.maxAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := evaluateAssertion(&step.Assert.Spec, store)
		if err != nil {
			return StepResult{}, err
		}
		last = result
		lastActual = result.Actual
		if result.Status == StatusPass {
			passStreak++
			now := time.Now()
			if firstPassAt == nil {
				firstPassAt = &now
			}
			lastPassedAt = &now
			if compiledConverge == nil || convergenceSatisfied(compiledConverge, passStreak, firstPassAt, now) {
				return StepResult{
					Name:       step.Name,
					Kind:       StepKindAssert,
					Status:     result.Status,
					Summary:    fmt.Sprintf("assertion %q completed with status %s", result.Name, result.Status),
					Assertions: []AssertionResult{result},
					Attempts:   attempt,
					Convergence: &ConvergenceResult{
						Met:              true,
						Attempts:         attempt,
						PassStreak:       passStreak,
						ObservedFailures: observedFailures,
						StartedAt:        startedAt,
						CompletedAt:      now,
						LastPassedAt:     lastPassedAt,
						LastActual:       lastActual,
					},
				}, nil
			}
		} else {
			passStreak = 0
			firstPassAt = nil
			observedFailures++
		}
		if attempt < maxAttempts {
			wait := intervalForAttempt(compiledConverge, attempt)
			if !sleepWithContext(ctx, wait) {
				break
			}
		}
	}

	return StepResult{
		Name:       step.Name,
		Kind:       StepKindAssert,
		Status:     last.Status,
		Code:       last.Code,
		Summary:    fmt.Sprintf("assertion %q completed with status %s", last.Name, last.Status),
		Assertions: []AssertionResult{last},
		Attempts:   maxAttempts,
		Convergence: &ConvergenceResult{
			Met:              false,
			Attempts:         maxAttempts,
			PassStreak:       passStreak,
			ObservedFailures: observedFailures,
			StartedAt:        startedAt,
			CompletedAt:      time.Now(),
			LastPassedAt:     lastPassedAt,
			LastActual:       lastActual,
			TimeoutReached:   compiledConverge != nil,
		},
	}, nil
}

func (t Tool) runCompiledAssertAttempt(step compiledRunStep, store *EvidenceStore, input validation.StepExecutionInput) (StepResult, *validation.ConvergenceState, string, bool, error) {
	_ = input.Attempt
	now := time.Now()
	result, err := evaluateAssertion(&step.Assert.Spec, store)
	if err != nil {
		return StepResult{}, nil, "", false, err
	}
	conv := step.Converge.toCompiled()
	state := cloneConvergenceState(input.ConvergenceState)
	if state == nil {
		state = &validation.ConvergenceState{StartedAt: now}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = now
	}
	state.Attempt = input.Attempt
	state.LastActual = marshalActual(result.Actual)
	if result.Status == StatusPass {
		state.PassStreak++
		if state.FirstPassAt == nil {
			first := now
			state.FirstPassAt = &first
		}
		last := now
		state.LastPassedAt = &last
	} else {
		state.PassStreak = 0
		state.FirstPassAt = nil
		state.ObservedFailures++
	}
	completed := conv == nil || convergenceSatisfied(conv, state.PassStreak, state.FirstPassAt, now)
	timeoutReached := conv != nil && !completed && input.Attempt >= conv.maxAttempts
	state.Completed = completed || timeoutReached
	state.TimeoutReached = timeoutReached
	stepResult := StepResult{
		Name:       step.Name,
		Kind:       StepKindAssert,
		Status:     result.Status,
		Code:       result.Code,
		Summary:    fmt.Sprintf("assertion %q completed with status %s", result.Name, result.Status),
		Assertions: []AssertionResult{result},
		Attempts:   input.Attempt,
	}
	if state.Completed {
		stepResult.Convergence = buildConvergenceResult(state, result.Actual, completed, now)
		return stepResult, state, "", true, nil
	}
	stepResult.Convergence = buildConvergenceResult(state, result.Actual, false, now)
	return stepResult, state, intervalForAttempt(conv, input.Attempt).String(), false, nil
}

func buildConvergenceResult(state *validation.ConvergenceState, actual any, met bool, now time.Time) *ConvergenceResult {
	if state == nil {
		return nil
	}
	return &ConvergenceResult{
		Met:              met,
		Attempts:         state.Attempt,
		PassStreak:       state.PassStreak,
		ObservedFailures: state.ObservedFailures,
		StartedAt:        state.StartedAt,
		CompletedAt:      now,
		LastPassedAt:     state.LastPassedAt,
		LastActual:       actual,
		TimeoutReached:   state.TimeoutReached,
	}
}

func cloneConvergenceState(in *validation.ConvergenceState) *validation.ConvergenceState {
	if in == nil {
		return nil
	}
	out := *in
	if in.FirstPassAt != nil {
		first := *in.FirstPassAt
		out.FirstPassAt = &first
	}
	if in.LastPassedAt != nil {
		last := *in.LastPassedAt
		out.LastPassedAt = &last
	}
	if in.LastActual != nil {
		out.LastActual = append(json.RawMessage(nil), in.LastActual...)
	}
	return &out
}

func marshalActual(actual any) json.RawMessage {
	if actual == nil {
		return nil
	}
	raw, err := json.Marshal(actual)
	if err != nil {
		return nil
	}
	return raw
}

func (c *compiledRunConverge) toCompiled() *compiledConverge {
	if c == nil {
		return nil
	}
	interval, _ := time.ParseDuration(c.Interval)
	timeout, _ := time.ParseDuration(c.Timeout)
	stabilizeFor, _ := time.ParseDuration(c.StabilizeFor)
	return &compiledConverge{
		until:        c.Until,
		interval:     interval,
		timeout:      timeout,
		maxAttempts:  c.MaxAttempts,
		backoff:      c.Backoff,
		stabilizeFor: stabilizeFor,
		minPasses:    c.MinPasses,
	}
}

func convergenceSatisfied(conv *compiledConverge, passStreak int, firstPassAt *time.Time, now time.Time) bool {
	if conv == nil {
		return true
	}
	if passStreak < conv.minPasses {
		return false
	}
	if conv.stabilizeFor > 0 {
		if firstPassAt == nil {
			return false
		}
		if now.Sub(*firstPassAt) < conv.stabilizeFor {
			return false
		}
	}
	return true
}

func intervalForAttempt(conv *compiledConverge, attempt int) time.Duration {
	if conv == nil || conv.interval <= 0 {
		return 0
	}
	if conv.backoff == "linear" {
		return time.Duration(attempt) * conv.interval
	}
	return conv.interval
}

func withCollectAttemptTimeout(ctx context.Context, retry *compiledRunRetry) (context.Context, context.CancelFunc) {
	if retry == nil || strings.TrimSpace(retry.Timeout) == "" {
		return context.WithCancel(ctx)
	}
	timeout, err := time.ParseDuration(retry.Timeout)
	if err != nil || timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func collectRetryInterval(retry *compiledRunRetry) string {
	if retry == nil {
		return ""
	}
	return retry.Interval
}

func collectAttemptRemaining(retry *compiledRunRetry, attempt int) bool {
	if retry == nil {
		return false
	}
	maxAttempts := retry.MaxAttempts
	if maxAttempts <= 1 {
		return false
	}
	return attempt < maxAttempts
}

func classifyCollectFailure(msg string) collectFailureCategory {
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case lower == "":
		return collectFailureNonRetryable
	case strings.Contains(lower, "context canceled"),
		strings.Contains(lower, "context cancelled"),
		strings.Contains(lower, "operation was canceled"),
		strings.Contains(lower, "operation was cancelled"):
		return collectFailureNone
	case strings.Contains(lower, "deadline exceeded"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "network is unreachable"),
		strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "eof"),
		strings.Contains(lower, "transport is closing"),
		strings.Contains(lower, "temporarily unavailable"),
		strings.Contains(lower, "unavailable"):
		return collectFailureRetryable
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "unable to authenticate"),
		strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "no supported methods remain"),
		strings.Contains(lower, "knownhosts"),
		strings.Contains(lower, "host key"),
		strings.Contains(lower, "not found"),
		strings.Contains(lower, "does not implement"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "unsupported"),
		strings.Contains(lower, "safety check failed"):
		return collectFailureNonRetryable
	default:
		return collectFailureNonRetryable
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
