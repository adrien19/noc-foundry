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
	"reflect"
	"slices"

	"github.com/itchyny/gojq"
)

func evaluateAssertion(spec *AssertSpec, store *EvidenceStore) (AssertionResult, error) {
	evidence, err := store.MustGetMany(spec.effectiveFrom())
	if err != nil {
		return AssertionResult{}, err
	}
	input, err := buildAssertionInput(spec, evidence, store)
	if err != nil {
		return AssertionResult{}, err
	}

	result := AssertionResult{
		Name:           spec.effectiveName(),
		Scope:          spec.effectiveScope(),
		Expression:     spec.effectiveExpression(),
		Expected:       spec.Expect,
		Severity:       spec.effectiveSeverity(),
		Recommendation: spec.Message,
	}

	switch spec.effectiveScope() {
	case ScopeAggregate:
		passed, actual, err := runCompiledAssertion(spec, input)
		if err != nil {
			return AssertionResult{}, err
		}
		result.Actual = actual
		if passed {
			result.Status = StatusPass
			result.Passed = 1
		} else {
			result.Status = StatusFail
			result.Failed = 1
			result.Code = failureCode(spec)
			result.Reason = "aggregate assertion failed"
		}
		return result, nil
	case ScopePerRecord:
		records := flattenEvidence(evidence)
		if len(records) == 0 {
			return AssertionResult{}, fmt.Errorf("assertion %q referenced empty evidence", result.Name)
		}
		for _, record := range records {
			passed, actual, err := runCompiledAssertion(spec, record)
			if err != nil {
				return AssertionResult{}, err
			}
			if passed {
				result.Passed++
				continue
			}
			result.Failed++
			result.Failures = append(result.Failures, AssertionFailure{
				DeviceID: recordString(record, "device_id"),
				Message:  "expression evaluated to false",
				Evidence: fmt.Sprintf("%v", actual),
			})
		}
		result.Status = passRateStatus(result.Passed, result.Failed, spec.effectiveMinPassRate())
		if result.Status != StatusPass {
			result.Code = failureCode(spec)
			result.Reason = "per-record assertion failed"
		}
		return result, nil
	case ScopePerDevice:
		byDevice := groupEvidenceByDevice(evidence)
		if len(byDevice) == 0 {
			return AssertionResult{}, fmt.Errorf("assertion %q referenced empty evidence", result.Name)
		}
		for deviceID, records := range byDevice {
			passed, actual, err := runCompiledAssertion(spec, map[string]any{
				"device_id": deviceID,
				"records":   records,
				"summary": map[string]any{
					"count": len(records),
				},
			})
			if err != nil {
				return AssertionResult{}, err
			}
			if passed {
				result.Passed++
				continue
			}
			result.Failed++
			result.Failures = append(result.Failures, AssertionFailure{
				DeviceID: deviceID,
				Message:  "expression evaluated to false",
				Evidence: fmt.Sprintf("%v", actual),
			})
		}
		result.Status = passRateStatus(result.Passed, result.Failed, spec.effectiveMinPassRate())
		if result.Status != StatusPass {
			result.Code = failureCode(spec)
			result.Reason = "per-device assertion failed"
		}
		return result, nil
	default:
		return AssertionResult{}, fmt.Errorf("unsupported assertion scope %q", spec.Scope)
	}
}

func buildAssertionInput(spec *AssertSpec, evidence []Evidence, store *EvidenceStore) (map[string]any, error) {
	input := buildAggregateInput(evidence)
	inputs := map[string]any{}
	for name, ref := range spec.Inputs {
		evs, err := resolveEvidenceRef(ref, store)
		if err != nil {
			return nil, err
		}
		inputs[ref.effectiveAlias(name)] = toJSONValue(flattenEvidence(evs))
	}
	comparisons := make([]any, 0, len(spec.Compare))
	for idx, cmp := range spec.Compare {
		left, err := resolveEvidenceRef(cmp.Left, store)
		if err != nil {
			return nil, err
		}
		right, err := resolveEvidenceRef(cmp.Right, store)
		if err != nil {
			return nil, err
		}
		lflat := flattenEvidence(left)
		rflat := flattenEvidence(right)
		if len(lflat) == 0 || len(rflat) == 0 {
			return nil, fmt.Errorf("comparison %q has an empty side", cmp.effectiveName(idx))
		}
		comparisons = append(comparisons, toJSONValue(map[string]any{
			"name":  cmp.effectiveName(idx),
			"left":  lflat,
			"right": rflat,
		}))
	}
	input["inputs"] = inputs
	input["comparisons"] = comparisons
	return input, nil
}

func buildAggregateInput(evidence []Evidence) map[string]any {
	evidenceMap := make(map[string]any, len(evidence))
	flattened := make([]any, 0)
	total := 0
	failed := 0
	for _, ev := range evidence {
		entries := make([]any, 0, len(ev.Records))
		for _, rec := range ev.Records {
			normalized := normalizeEvidenceRecord(rec)
			entries = append(entries, normalized)
			flattened = append(flattened, normalized)
		}
		evidenceMap[ev.Name] = entries
		total += ev.Summary.Total
		failed += ev.Summary.Failed
	}
	return map[string]any{
		"evidence": evidenceMap,
		"records":  flattened,
		"summary": map[string]any{
			"total":  total,
			"failed": failed,
			"passed": total - failed,
		},
	}
}

func resolveEvidenceRef(ref EvidenceRef, store *EvidenceStore) ([]Evidence, error) {
	ev, ok := store.Get(ref.Evidence)
	if !ok {
		return nil, fmt.Errorf("evidence %q not found", ref.Evidence)
	}
	if ref.Group == "" {
		return []Evidence{ev}, nil
	}
	filtered := Evidence{Name: ev.Name}
	for _, rec := range ev.Records {
		if slices.Contains(rec.Groups, ref.Group) {
			filtered.Records = append(filtered.Records, rec)
		}
	}
	filtered.Summary.Total = len(filtered.Records)
	for _, rec := range filtered.Records {
		if rec.Error == "" {
			filtered.Summary.Succeeded++
		} else {
			filtered.Summary.Failed++
		}
	}
	if len(filtered.Records) == 0 {
		return nil, fmt.Errorf("evidence %q has no records for group %q", ref.Evidence, ref.Group)
	}
	return []Evidence{filtered}, nil
}

func flattenEvidence(evidence []Evidence) []map[string]any {
	out := make([]map[string]any, 0)
	for _, ev := range evidence {
		for _, rec := range ev.Records {
			out = append(out, normalizeEvidenceRecord(rec))
		}
	}
	return out
}

func groupEvidenceByDevice(evidence []Evidence) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, ev := range evidence {
		for _, rec := range ev.Records {
			norm := normalizeEvidenceRecord(rec)
			deviceID := recordString(norm, "device_id")
			out[deviceID] = append(out[deviceID], norm)
		}
	}
	return out
}

func normalizeEvidenceRecord(rec EvidenceRecord) map[string]any {
	out := map[string]any{
		"device_id":      rec.DeviceID,
		"source_name":    rec.SourceName,
		"labels":         rec.Labels,
		"groups":         rec.Groups,
		"transport":      rec.Transport,
		"selected_by":    rec.SelectedBy,
		"selection_note": rec.SelectionNote,
		"error":          rec.Error,
	}
	if rec.Record == nil {
		return out
	}
	out["record_type"] = rec.Record.RecordType
	out["payload"] = toJSONValue(rec.Record.Payload)
	out["quality"] = toJSONValue(rec.Record.Quality)
	out["collection"] = toJSONValue(rec.Record.Collection)
	out["native"] = toJSONValue(rec.Record.Native)
	return out
}

func runCompiledAssertion(spec *AssertSpec, input any) (bool, any, error) {
	if spec.Primitive == nil {
		return runAssertionExpr(spec.Expr, input)
	}
	return evaluatePrimitiveAssertion(spec.Primitive, input)
}

func runAssertionExpr(expr string, input any) (bool, any, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return false, nil, fmt.Errorf("invalid assertion expression %q: %w", expr, err)
	}
	iter := query.Run(input)
	v, ok := iter.Next()
	if !ok {
		return false, nil, nil
	}
	if err, ok := v.(error); ok {
		return false, nil, fmt.Errorf("assertion evaluation failed: %w", err)
	}
	b, ok := v.(bool)
	if !ok {
		return false, v, nil
	}
	return b, v, nil
}

func evaluatePrimitiveAssertion(check *AssertionCheck, input any) (bool, any, error) {
	values, err := extractPathValues(input, check.Path)
	if err != nil {
		return false, nil, err
	}
	if check.Filter != "" && check.Type != AssertionDeltaWithin {
		values, err = filterValues(values, check.Filter)
		if err != nil {
			return false, nil, err
		}
	}

	switch check.Type {
	case AssertionEquals:
		if len(values) == 0 {
			return false, nil, nil
		}
		return reflect.DeepEqual(values[0], check.Value), values[0], nil
	case AssertionNotEquals:
		if len(values) == 0 {
			return false, nil, nil
		}
		return !reflect.DeepEqual(values[0], check.Value), values[0], nil
	case AssertionExists:
		return len(values) > 0 && values[0] != nil, values, nil
	case AssertionCountEQ:
		return len(values) == derefInt(check.Count), len(values), nil
	case AssertionCountGTE:
		return len(values) >= derefInt(check.Count), len(values), nil
	case AssertionCountLTE:
		return len(values) <= derefInt(check.Count), len(values), nil
	case AssertionAllMatch:
		return len(values) > 0 && len(values) == countMatches(values, check.Filter), values, nil
	case AssertionAnyMatch:
		return countMatches(values, check.Filter) > 0, values, nil
	case AssertionNoneMatch:
		return countMatches(values, check.Filter) == 0, values, nil
	case AssertionSameAcross:
		if len(values) == 0 {
			return false, values, nil
		}
		first := values[0]
		for _, v := range values[1:] {
			if !reflect.DeepEqual(first, v) {
				return false, values, nil
			}
		}
		return true, values, nil
	case AssertionDeltaWithin:
		comparisons, ok := input.(map[string]any)["comparisons"].([]any)
		if !ok || len(comparisons) == 0 {
			return false, nil, fmt.Errorf("delta_within requires comparisons input")
		}
		comp, ok := comparisons[0].(map[string]any)
		if !ok {
			return false, nil, fmt.Errorf("invalid comparison input")
		}
		left, err := reduceValues(comp["left"], check.Path, check.Reduce)
		if err != nil {
			return false, nil, err
		}
		right, err := reduceValues(comp["right"], check.Path, check.Reduce)
		if err != nil {
			return false, nil, err
		}
		delta := left - right
		if delta < 0 {
			delta = -delta
		}
		return delta <= derefFloat(check.Tolerance), delta, nil
	default:
		return false, nil, fmt.Errorf("unsupported primitive %q", check.Type)
	}
}

func reduceValues(input any, path string, reduce AssertionReduce) (float64, error) {
	values, err := extractPathValues(input, path)
	if err != nil {
		return 0, err
	}
	switch reduce {
	case ReduceCount:
		return float64(len(values)), nil
	case ReduceFirst:
		if len(values) == 0 {
			return 0, nil
		}
		return toFloat(values[0]), nil
	case ReduceSum:
		var total float64
		for _, v := range values {
			total += toFloat(v)
		}
		return total, nil
	default:
		return 0, fmt.Errorf("unsupported reduce %q", reduce)
	}
}

func extractPathValues(input any, path string) ([]any, error) {
	if path == "" {
		switch vv := input.(type) {
		case []any:
			return vv, nil
		default:
			return []any{input}, nil
		}
	}
	query, err := gojq.Parse("." + path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	iter := query.Run(input)
	out := []any{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, err
		}
		if arr, ok := v.([]any); ok {
			out = append(out, arr...)
		} else {
			out = append(out, v)
		}
	}
	return out, nil
}

func filterValues(values []any, filter string) ([]any, error) {
	out := make([]any, 0, len(values))
	for _, v := range values {
		ok, _, err := runAssertionExpr(filter, v)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func countMatches(values []any, filter string) int {
	n := 0
	for _, v := range values {
		ok, _, err := runAssertionExpr(filter, v)
		if err == nil && ok {
			n++
		}
	}
	return n
}

func passRateStatus(passed, failed int, minPassRate float64) ValidationStatus {
	total := passed + failed
	if total == 0 {
		return StatusFail
	}
	if float64(passed)/float64(total) >= minPassRate {
		return StatusPass
	}
	return StatusFail
}

func failureCode(spec *AssertSpec) string {
	if spec == nil {
		return "assertion_failed"
	}
	if spec.Primitive != nil {
		return string(spec.Primitive.Type) + "_failed"
	}
	if spec.Name != "" {
		return spec.Name + "_failed"
	}
	return "assertion_failed"
}

func (spec *AssertSpec) effectiveScope() AssertionScope {
	if spec == nil || spec.Scope == "" {
		return ScopeAggregate
	}
	return spec.Scope
}

func (spec *AssertSpec) effectiveExpression() string {
	if spec == nil {
		return ""
	}
	if spec.Expr != "" {
		return spec.Expr
	}
	if spec.Primitive != nil {
		return string(spec.Primitive.Type)
	}
	return ""
}

func (spec *AssertSpec) effectiveName() string {
	if spec == nil || spec.Name == "" {
		return "assertion"
	}
	return spec.Name
}

func (spec *AssertSpec) effectiveFrom() []string {
	if spec == nil {
		return nil
	}
	return spec.From
}

func (spec *AssertSpec) effectiveMinPassRate() float64 {
	if spec == nil || spec.MinPassRate == nil {
		return 1.0
	}
	return *spec.MinPassRate
}

func (spec *AssertSpec) effectiveSeverity() ResultSeverity {
	if spec == nil || spec.Severity == "" {
		return SeverityError
	}
	return spec.Severity
}

func (ref EvidenceRef) effectiveAlias(name string) string {
	if ref.Alias != "" {
		return ref.Alias
	}
	return name
}

func (cmp EvidenceComparisonRef) effectiveName(idx int) string {
	if cmp.Name != "" {
		return cmp.Name
	}
	return fmt.Sprintf("comparison_%d", idx+1)
}
