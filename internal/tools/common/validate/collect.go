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
	"slices"
	"sort"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

func (t Tool) resolvePhase(params parameters.ParamValues) (compiledPhase, error) {
	phaseName := extractStringParam(params, "phase")
	if len(t.compiled.order) == 1 {
		name := t.compiled.order[0]
		if phaseName == "" || phaseName == name {
			return t.compiled.phases[name], nil
		}
		return compiledPhase{}, fmt.Errorf("unknown phase %q", phaseName)
	}
	if phaseName == "" {
		return compiledPhase{}, fmt.Errorf("missing required parameter 'phase'")
	}
	phase, ok := t.compiled.phases[phaseName]
	if !ok {
		return compiledPhase{}, fmt.Errorf("unknown phase %q", phaseName)
	}
	return phase, nil
}

func (t Tool) resolveScope(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues) (ResultScope, map[string][]ResolvedTarget, error) {
	if t.Source != "" {
		src, ok := resourceMgr.GetSource(t.Source)
		if !ok {
			return ResultScope{}, nil, fmt.Errorf("source %q not found", t.Source)
		}
		target := ResolvedTarget{
			DeviceID:     extractDevice(t.Source),
			SourceName:   t.Source,
			Labels:       nil,
			GroupNames:   []string{"all"},
			Transport:    templateOf(t.Source),
			Capabilities: getCapabilities(src),
		}
		groups := map[string][]ResolvedTarget{
			"":    {target},
			"all": {target},
		}
		return ResultScope{Source: t.Source, Devices: []string{target.DeviceID}}, groups, nil
	}

	matchLabels := cloneMap(t.SourceSelector.MatchLabels)
	if t.SourceSelector.Template != "" {
		matchLabels["template"] = t.SourceSelector.Template
	}
	srcs, err := resourceMgr.GetSourcesByLabels(ctx, matchLabels)
	if err != nil {
		return ResultScope{}, nil, err
	}
	if len(srcs) == 0 {
		return ResultScope{}, nil, fmt.Errorf("sourceSelector matched 0 devices")
	}

	deviceParam := extractStringParam(params, "device")
	poolLabels := resourceMgr.GetDevicePoolLabels()
	base := make([]ResolvedTarget, 0, len(srcs))
	for name, src := range srcs {
		deviceID := extractDevice(name)
		if deviceParam != "" && deviceID != deviceParam {
			continue
		}
		base = append(base, ResolvedTarget{
			DeviceID:     deviceID,
			SourceName:   name,
			Labels:       cloneMap(poolLabels[name]),
			Transport:    templateOf(name),
			Capabilities: getCapabilities(src),
		})
	}
	if len(base) == 0 {
		return ResultScope{}, nil, fmt.Errorf("device %q not found among matched sources", deviceParam)
	}
	sort.Slice(base, func(i, j int) bool { return base[i].SourceName < base[j].SourceName })
	groups := t.resolveGroups(base)
	return ResultScope{
		Selector: cloneMap(t.SourceSelector.MatchLabels),
		Devices:  uniqueDevices(base),
	}, groups, nil
}

func (t Tool) resolveGroups(base []ResolvedTarget) map[string][]ResolvedTarget {
	groups := map[string][]ResolvedTarget{
		"":    slices.Clone(base),
		"all": slices.Clone(base),
	}
	for name, group := range t.Groups {
		filtered := make([]ResolvedTarget, 0)
		for _, target := range base {
			if !matchesGroupTarget(target, group) {
				continue
			}
			next := target
			next.GroupNames = appendUnique(next.GroupNames, name)
			filtered = append(filtered, next)
		}
		groups[name] = filtered
	}
	return groups
}

func matchesGroupTarget(target ResolvedTarget, group TargetGroup) bool {
	if len(group.Devices) > 0 && !slices.Contains(group.Devices, target.DeviceID) {
		return false
	}
	if group.Template != "" && group.Template != target.Transport {
		return false
	}
	if len(group.MatchLabels) > 0 && !matchesAllLabels(target.Labels, group.MatchLabels) {
		return false
	}
	return true
}

func appendUnique(in []string, val string) []string {
	if slices.Contains(in, val) {
		return in
	}
	return append(in, val)
}

func getCapabilities(source sources.Source) capabilities.SourceCapabilities {
	if cp, ok := source.(capabilities.CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return capabilities.SourceCapabilities{}
}

func (t Tool) runCompiledPlanStep(ctx context.Context, resourceMgr tools.SourceProvider, step compiledRunStep, store *EvidenceStore) (StepResult, map[string]json.RawMessage, collectFailureCategory, error) {
	if step.Collect != nil {
		attempt, err := t.collectEvidenceFromPlans(ctx, resourceMgr, step.Collect)
		if err != nil {
			return StepResult{}, nil, collectFailureNone, err
		}
		delta := map[string]json.RawMessage{}
		raw, err := json.Marshal(attempt.Evidence)
		if err != nil {
			return StepResult{}, nil, collectFailureNone, err
		}
		delta[step.Collect.Spec.Into] = raw
		return StepResult{
			Name:           step.Name,
			Kind:           StepKindCollect,
			Status:         attempt.Status,
			Code:           attempt.Code,
			Summary:        fmt.Sprintf("collected %d records into %q", attempt.Evidence.Summary.Total, step.Collect.Spec.Into),
			Groups:         slices.Clone(step.Collect.Spec.Targets),
			Recommendation: attempt.Recommendation,
			Attempts:       1,
		}, delta, attempt.FailureCategory, nil
	}
	result, err := t.runCompiledAssertStep(ctx, step, store)
	return result, nil, collectFailureNone, err
}

func (t Tool) collectEvidenceFromPlans(ctx context.Context, resourceMgr tools.SourceProvider, cc *compiledRunCollectStep) (collectAttemptResult, error) {
	evidence := Evidence{Name: cc.Spec.Into}
	sourceNames := make([]string, 0, len(cc.Plans))
	planBySource := make(map[string]compiledPlanEntry, len(cc.Plans))
	allRetryable := true
	for _, plan := range cc.Plans {
		if plan.Target.SourceName == "" {
			evidence.Records = append(evidence.Records, EvidenceRecord{
				DeviceID:      plan.Target.DeviceID,
				Labels:        cloneMap(plan.Target.Labels),
				Groups:        slices.Clone(plan.Target.GroupNames),
				Transport:     plan.Target.Transport,
				SelectedBy:    plan.SelectedBy,
				SelectionNote: plan.SelectionNote,
				Error:         plan.SelectionNote,
			})
			evidence.Summary.Failed++
			allRetryable = false
			continue
		}
		sourceNames = append(sourceNames, plan.Target.SourceName)
		planBySource[plan.Target.SourceName] = plan
	}
	if len(sourceNames) > 0 {
		result := fanout.Execute(ctx, sourceNames, t.maxConcurrency(), func(ctx context.Context, sourceName string) (any, error) {
			plan := planBySource[sourceName]
			rawSource, ok := resourceMgr.GetSource(sourceName)
			if !ok {
				return nil, fmt.Errorf("source %q not found", sourceName)
			}
			exec := t.baseExecutor
			if len(cc.Spec.Transforms) > 0 {
				exec = exec.WithTransforms(query.TransformSet(cc.Spec.Transforms))
			}
			switch cc.Mode {
			case modeCommand:
				record, err := exec.ExecuteCommand(ctx, rawSource, cc.Spec.Command, sourceName)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", plan.Target.DeviceID, err)
				}
				return record, nil
			default:
				return exec.Execute(ctx, rawSource, cc.Spec.Operation, sourceName)
			}
		})
		if ctx.Err() != nil {
			return collectAttemptResult{}, ctx.Err()
		}
		for _, item := range result.Results {
			plan := planBySource[sourceNameForDevice(item.Device, sourceNames)]
			rec := EvidenceRecord{
				DeviceID:      item.Device,
				SourceName:    plan.Target.SourceName,
				Labels:        cloneMap(plan.Target.Labels),
				Groups:        slices.Clone(plan.Target.GroupNames),
				Transport:     plan.Target.Transport,
				SelectedBy:    plan.SelectedBy,
				SelectionNote: plan.SelectionNote,
			}
			if item.Status == "success" {
				record, ok := item.Data.(*models.Record)
				if !ok {
					return collectAttemptResult{}, fmt.Errorf("unexpected collect result type %T", item.Data)
				}
				rec.Record = record
				evidence.Summary.Succeeded++
			} else {
				rec.Error = item.Error
				evidence.Summary.Failed++
				if classifyCollectFailure(item.Error) != collectFailureRetryable {
					allRetryable = false
				}
			}
			evidence.Records = append(evidence.Records, rec)
		}
	}
	evidence.Summary.Total = len(evidence.Records)
	sort.Slice(evidence.Records, func(i, j int) bool { return evidence.Records[i].SourceName < evidence.Records[j].SourceName })
	switch {
	case evidence.Summary.Failed == 0:
		return collectAttemptResult{Evidence: evidence, Status: StatusPass}, nil
	case evidence.Summary.Succeeded == 0:
		failureCategory := collectFailureNonRetryable
		if allRetryable {
			failureCategory = collectFailureRetryable
		}
		return collectAttemptResult{
			Evidence:        evidence,
			Status:          StatusFail,
			Code:            "collection_failed",
			Recommendation:  "verify transport and source capabilities for this step",
			FailureCategory: failureCategory,
		}, nil
	default:
		return collectAttemptResult{
			Evidence:        evidence,
			Status:          StatusPartial,
			Code:            "collection_partial",
			Recommendation:  "review failed collection records before continuing",
			FailureCategory: collectFailureNonRetryable,
		}, nil
	}
}

func buildTransportPlans(targets []ResolvedTarget, cc *compiledCollect) []transportPlan {
	byDevice := map[string][]ResolvedTarget{}
	for _, target := range targets {
		byDevice[target.DeviceID] = append(byDevice[target.DeviceID], target)
	}
	plans := make([]transportPlan, 0, len(byDevice))
	for deviceID, candidates := range byDevice {
		plan := selectTransportPlan(deviceID, candidates, cc)
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].target.DeviceID < plans[j].target.DeviceID })
	return plans
}

func serializePlans(in []transportPlan) []compiledPlanEntry {
	out := make([]compiledPlanEntry, 0, len(in))
	for _, plan := range in {
		out = append(out, compiledPlanEntry{
			Target:        plan.target,
			SelectedBy:    plan.selectedBy,
			SelectionNote: plan.selectionNote,
		})
	}
	return out
}

func selectTransportPlan(deviceID string, candidates []ResolvedTarget, cc *compiledCollect) transportPlan {
	filtered := make([]ResolvedTarget, 0, len(candidates))
	for _, candidate := range candidates {
		if !collectModeCompatible(cc.mode, candidate) {
			continue
		}
		if len(cc.transport.Require) > 0 && !slices.Contains(cc.transport.Require, candidate.Transport) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return transportPlan{
			target:        ResolvedTarget{DeviceID: deviceID},
			selectedBy:    "transport_policy",
			selectionNote: fmt.Sprintf("no compatible source found for mode=%s require=%v", cc.mode, cc.transport.Require),
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return transportRank(filtered[i].Transport, cc.transport.Prefer) < transportRank(filtered[j].Transport, cc.transport.Prefer)
	})
	chosen := filtered[0]
	selectedBy := "transport_policy"
	note := fmt.Sprintf("selected %s for mode=%s", chosen.Transport, cc.mode)
	return transportPlan{target: chosen, selectedBy: selectedBy, selectionNote: note}
}

func collectModeCompatible(mode collectionMode, target ResolvedTarget) bool {
	switch mode {
	case modeCommand:
		return target.Capabilities.CLI || target.Transport == "ssh"
	default:
		return true
	}
}

func transportRank(transport string, prefer []string) int {
	for i, pref := range prefer {
		if pref == transport {
			return i
		}
	}
	switch transport {
	case "netconf":
		return len(prefer) + 0
	case "gnmi":
		return len(prefer) + 1
	case "ssh":
		return len(prefer) + 2
	default:
		return len(prefer) + 3
	}
}

func sourceNameForDevice(device string, sourceNames []string) string {
	for _, name := range sourceNames {
		if extractDevice(name) == device {
			return name
		}
	}
	return device
}

func selectTargets(names []string, groups map[string][]ResolvedTarget) ([]ResolvedTarget, error) {
	if len(names) == 0 {
		return groups[""], nil
	}
	seen := map[string]ResolvedTarget{}
	for _, name := range names {
		targets, ok := groups[name]
		if !ok {
			return nil, fmt.Errorf("unknown target group %q", name)
		}
		for _, target := range targets {
			seen[target.SourceName] = target
		}
	}
	out := make([]ResolvedTarget, 0, len(seen))
	for _, target := range seen {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceName < out[j].SourceName })
	return out, nil
}

func (t Tool) maxConcurrency() int {
	if t.Defaults != nil && t.Defaults.MaxConcurrency > 0 {
		return t.Defaults.MaxConcurrency
	}
	if t.SourceSelector != nil && t.SourceSelector.MaxConcurrency > 0 {
		return t.SourceSelector.MaxConcurrency
	}
	return fanout.DefaultMaxConcurrency
}
