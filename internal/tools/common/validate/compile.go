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
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/sources"
)

func (cfg Config) compileConfig(srcs map[string]sources.Source) (compiledConfig, error) {
	seenPhases := map[string]bool{}
	out := compiledConfig{phases: map[string]compiledPhase{}}

	for _, phase := range cfg.Phases {
		if phase.Name == "" {
			return compiledConfig{}, fmt.Errorf("phase name is required")
		}
		if seenPhases[phase.Name] {
			return compiledConfig{}, fmt.Errorf("duplicate phase %q", phase.Name)
		}
		seenPhases[phase.Name] = true
		if len(phase.Steps) == 0 {
			return compiledConfig{}, fmt.Errorf("phase %q must define at least one step", phase.Name)
		}

		cp := compiledPhase{name: phase.Name, description: phase.Description}
		seenEvidence := map[string]bool{}
		for _, step := range phase.Steps {
			cs, err := cfg.compileStep(step, seenEvidence, srcs)
			if err != nil {
				return compiledConfig{}, fmt.Errorf("phase %q step %q: %w", phase.Name, step.Name, err)
			}
			cp.steps = append(cp.steps, cs)
		}
		out.order = append(out.order, phase.Name)
		out.phases[phase.Name] = cp
	}
	return out, nil
}

func (cfg Config) compileStep(step Step, seenEvidence map[string]bool, srcs map[string]sources.Source) (compiledStep, error) {
	if step.Name == "" {
		return compiledStep{}, fmt.Errorf("step name is required")
	}
	if step.Retry != nil && step.Converge != nil {
		return compiledStep{}, fmt.Errorf("cannot define both retry and converge")
	}
	if (step.Collect == nil) == (step.Assert == nil) {
		return compiledStep{}, fmt.Errorf("must define exactly one of collect or assert")
	}

	out := compiledStep{Step: step, failFast: cfg.defaultFailFast(step)}

	if step.Collect != nil {
		cc, err := cfg.compileCollect(*step.Collect, srcs)
		if err != nil {
			return compiledStep{}, err
		}
		if seenEvidence[cc.spec.Into] {
			return compiledStep{}, fmt.Errorf("duplicate evidence name %q", cc.spec.Into)
		}
		seenEvidence[cc.spec.Into] = true
		out.mode = cc.mode
		out.collect = &cc
	}

	if step.Assert != nil {
		if step.Converge != nil && step.Collect != nil {
			return compiledStep{}, fmt.Errorf("converge is only supported on assert steps")
		}
		ca, err := cfg.compileAssert(*step.Assert)
		if err != nil {
			return compiledStep{}, err
		}
		out.assert = &ca
	}

	conv, err := cfg.compileConverge(step.Retry, step.Converge)
	if err != nil {
		return compiledStep{}, err
	}
	out.converge = conv
	return out, nil
}

func (cfg Config) compileCollect(spec CollectSpec, srcs map[string]sources.Source) (compiledCollect, error) {
	if spec.Into == "" {
		return compiledCollect{}, fmt.Errorf("collect.into is required")
	}
	if spec.Operation == "" && spec.Command == "" {
		return compiledCollect{}, fmt.Errorf("collect must define operation or command")
	}
	if spec.Operation != "" && spec.Command != "" {
		return compiledCollect{}, fmt.Errorf("collect cannot define both operation and command")
	}

	mode := modeOperation
	if spec.Command != "" {
		mode = modeCommand
	}
	policy := cfg.defaultTransportPolicy(mode, spec.Transport)
	if cfg.Source != "" && len(policy.Require) > 0 && !slices.Contains(policy.Require, templateOf(cfg.Source)) {
		return compiledCollect{}, fmt.Errorf("fixed source %q does not satisfy required transports %v", cfg.Source, policy.Require)
	}
	if cfg.Source != "" && mode == modeCommand {
		if src, ok := srcs[cfg.Source]; ok {
			if _, ok := src.(capabilities.CommandRunner); !ok {
				return compiledCollect{}, fmt.Errorf("fixed source %q does not support CLI command execution", cfg.Source)
			}
		}
	}
	retry, err := cfg.compileCollectRetry(spec.Retry)
	if err != nil {
		return compiledCollect{}, err
	}

	return compiledCollect{
		spec:      spec,
		mode:      mode,
		transport: policy,
		retry:     retry,
	}, nil
}

func (cfg Config) compileCollectRetry(retry *RetryPolicy) (*compiledRetry, error) {
	if retry == nil {
		return nil, nil
	}
	maxAttempts := retry.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 1
	}
	if maxAttempts < 1 {
		return nil, fmt.Errorf("collect.retry.maxAttempts must be greater than 0")
	}
	var interval time.Duration
	var err error
	if maxAttempts > 1 {
		if strings.TrimSpace(retry.Interval) == "" {
			return nil, fmt.Errorf("collect.retry.interval is required when collect.retry.maxAttempts > 1")
		}
		interval, err = time.ParseDuration(retry.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid collect.retry.interval %q: %w", retry.Interval, err)
		}
		if interval < 0 {
			return nil, fmt.Errorf("collect.retry.interval must be greater than or equal to 0")
		}
	}
	var timeout time.Duration
	if strings.TrimSpace(retry.Timeout) != "" {
		timeout, err = time.ParseDuration(retry.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid collect.retry.timeout %q: %w", retry.Timeout, err)
		}
		if timeout <= 0 {
			return nil, fmt.Errorf("collect.retry.timeout must be greater than 0")
		}
	}
	return &compiledRetry{
		interval:    interval,
		timeout:     timeout,
		maxAttempts: maxAttempts,
	}, nil
}

func (cfg Config) defaultTransportPolicy(mode collectionMode, in *TransportPolicy) TransportPolicy {
	if in != nil {
		return *in
	}
	switch mode {
	case modeCommand:
		return TransportPolicy{
			Prefer:   []string{"ssh"},
			Require:  []string{"ssh"},
			Fallback: false,
		}
	default:
		return TransportPolicy{
			Prefer:   []string{"netconf", "gnmi", "ssh"},
			Fallback: true,
		}
	}
}

func (cfg Config) compileAssert(spec AssertSpec) (compiledAssert, error) {
	if len(spec.From) == 0 && len(spec.Inputs) == 0 && len(spec.Compare) == 0 {
		return compiledAssert{}, fmt.Errorf("assert must reference at least one evidence input")
	}
	if spec.Expr != "" && spec.Primitive != nil {
		return compiledAssert{}, fmt.Errorf("assert cannot define both expr and check")
	}
	if spec.Expr == "" && spec.Primitive == nil {
		return compiledAssert{}, fmt.Errorf("assert requires expr or check")
	}
	scope := spec.effectiveScope()
	if scope != ScopePerRecord && scope != ScopePerDevice && scope != ScopeAggregate {
		return compiledAssert{}, fmt.Errorf("invalid assert.scope %q", scope)
	}
	if spec.Primitive != nil {
		if err := validatePrimitive(spec.Primitive); err != nil {
			return compiledAssert{}, err
		}
	}
	aliases := map[string]bool{}
	for name, ref := range spec.Inputs {
		alias := ref.effectiveAlias(name)
		if aliases[alias] {
			return compiledAssert{}, fmt.Errorf("duplicate assert input alias %q", alias)
		}
		aliases[alias] = true
	}
	for _, cmp := range spec.Compare {
		if cmp.Left.Evidence == "" || cmp.Right.Evidence == "" {
			return compiledAssert{}, fmt.Errorf("compare requires left.evidence and right.evidence")
		}
	}
	return compiledAssert{spec: spec}, nil
}

func validatePrimitive(check *AssertionCheck) error {
	if check == nil {
		return nil
	}
	switch check.Type {
	case AssertionEquals, AssertionNotEquals, AssertionExists,
		AssertionCountEQ, AssertionCountGTE, AssertionCountLTE,
		AssertionAllMatch, AssertionAnyMatch, AssertionNoneMatch,
		AssertionSameAcross, AssertionDeltaWithin:
	default:
		return fmt.Errorf("unsupported check.type %q", check.Type)
	}
	switch check.Type {
	case AssertionEquals, AssertionNotEquals, AssertionExists, AssertionSameAcross:
		if check.Path == "" {
			return fmt.Errorf("path is required")
		}
	case AssertionCountEQ, AssertionCountGTE, AssertionCountLTE:
		if check.Path == "" || check.Count == nil {
			return fmt.Errorf("path and count are required")
		}
	case AssertionAllMatch, AssertionAnyMatch, AssertionNoneMatch:
		if check.Path == "" || check.Filter == "" {
			return fmt.Errorf("path and filter are required")
		}
	case AssertionDeltaWithin:
		if check.Tolerance == nil {
			return fmt.Errorf("tolerance is required")
		}
		if check.Reduce == "" {
			return fmt.Errorf("reduce is required")
		}
	}
	return nil
}

func (cfg Config) compileConverge(retry *RetryPolicy, converge *ConvergePolicy) (*compiledConverge, error) {
	if retry == nil && converge == nil {
		return nil, nil
	}
	if retry != nil && converge != nil {
		return nil, fmt.Errorf("cannot define both retry and converge")
	}

	if retry != nil {
		converge = &ConvergePolicy{
			Until:       ConvergeAssertionPass,
			Interval:    retry.Interval,
			Timeout:     retry.Timeout,
			MaxAttempts: retry.MaxAttempts,
			Backoff:     "fixed",
			MinPasses:   1,
		}
	}

	interval, err := cfg.parseDurationWithDefaults(converge.Interval, "interval")
	if err != nil {
		return nil, err
	}
	timeout, err := cfg.parseDurationWithDefaults(converge.Timeout, "timeout")
	if err != nil {
		return nil, err
	}
	stabilizeFor, err := cfg.parseDurationWithDefaults(converge.StabilizeFor, "stabilizeFor")
	if err != nil {
		return nil, err
	}
	if interval <= 0 {
		interval = time.Second
	}
	maxAttempts := converge.MaxAttempts
	if maxAttempts <= 0 {
		if timeout > 0 {
			maxAttempts = int(timeout/interval) + 1
		} else {
			maxAttempts = 1
		}
	}
	minPasses := converge.MinPasses
	if minPasses <= 0 {
		minPasses = 1
	}
	until := converge.Until
	if until == "" {
		until = ConvergeAssertionPass
	}

	return &compiledConverge{
		until:        until,
		interval:     interval,
		timeout:      timeout,
		maxAttempts:  maxAttempts,
		backoff:      converge.Backoff,
		stabilizeFor: stabilizeFor,
		minPasses:    minPasses,
	}, nil
}

func (cfg Config) defaultFailFast(step Step) bool {
	if step.FailFast != nil {
		return *step.FailFast
	}
	if cfg.Defaults != nil && cfg.Defaults.FailFast != nil {
		return *cfg.Defaults.FailFast
	}
	return step.Assert != nil
}

func (cfg Config) parseDurationWithDefaults(raw, field string) (time.Duration, error) {
	if raw == "" && cfg.Defaults != nil {
		switch field {
		case "interval":
			raw = cfg.Defaults.Interval
		case "timeout":
			raw = cfg.Defaults.Timeout
		}
	}
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration %q: %w", field, raw, err)
	}
	return d, nil
}
