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

package query

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/itchyny/gojq"
)

// TransformSpec holds a jq expression used to transform raw CLI output
// into a user-defined canonical structure before the result is returned.
//
// The jq expression receives different inputs depending on the declared
// output format of the CLI path and the Format field:
//
//   - "json" (strict): the raw output is unmarshalled as JSON before jq runs.
//     Returns an error if the output is not valid JSON.
//   - anything else (including "" or "text"): the raw output is auto-detected.
//     If it parses as JSON, jq receives the parsed object/array. If it does
//     not parse as JSON, jq receives {"text": "<raw>"} for .text access.
//
// Auto-detection means callers do not need a format hint to process JSON
// output — jq expressions like `.hostname` work directly on JSON-producing
// commands without a `format: json` annotation. Use `format: json` only when
// you want the strict behaviour (error on invalid JSON rather than silent
// fallback to the text wrapper).
//
// Use the Format field to bind the transform to a specific CLI path format
// (e.g. "json"). When set, the transform only fires when the profile's
// CLI path for the operation declares the same format. CLI paths with other
// formats fall through to the built-in parser untouched — this is what
// makes text fallback work correctly alongside a json-targeted transform.
//
// Example — extract interface names from a SRLinux JSON response:
//
//	format: json
//	jq: ".interface[] | .name"
type TransformSpec struct {
	// Format restricts this transform to CLI paths whose declared format
	// matches. If empty, the transform applies to all CLI paths for the
	// operation regardless of format.
	//
	// Common values: "json", "text". Matches ProtocolPath.Format exactly.
	//
	// When used with applyJQTransform, "json" enables strict JSON parsing
	// (error on invalid JSON). Any other value enables auto-detection:
	// try JSON first, fall back to {"text": raw} wrapper for non-JSON output.
	Format string `yaml:"format,omitempty"`

	// JQ is a jq expression applied to the parsed (or text-wrapped) output.
	// Must be a valid jq expression as accepted by gojq.
	JQ string `yaml:"jq"`
}

// TransformSet maps an operationID to its TransformSpec.
// When provided on a query Executor, it replaces the built-in parser for
// any operation that has an entry here.
type TransformSet map[string]TransformSpec

// parseOutput parses raw CLI output according to the format annotation,
// without running any jq expression. It is used in the fallback path when
// no jq transform is configured.
//
//   - "json" (strict): unmarshal JSON or return an error.
//   - anything else: auto-detect — tries JSON unmarshal first; falls back
//     to returning the raw string unchanged.
//
// Returns (parsedValue, wasJSON, error).
func parseOutput(raw, format string) (any, bool, error) {
	switch format {
	case "json":
		var parsed any
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, false, fmt.Errorf("format: json configured but output is not valid JSON: %w", err)
		}
		return parsed, true, nil
	default:
		var parsed any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			return parsed, true, nil
		}
		return raw, false, nil
	}
}

// applyJQTransform runs the jq expression in spec against raw CLI output.
// The format parameter controls how raw is presented to jq:
//   - "json": raw is strictly unmarshalled as JSON; returns an error if invalid.
//   - anything else: auto-detect — tries JSON unmarshal first; if raw is not
//     valid JSON, wraps it as {"text": raw} so jq can access it via .text.
//
// Returns the jq output as a single value when the query produces exactly one
// result, or a []any when the query produces multiple results.
func applyJQTransform(_ context.Context, spec TransformSpec, raw string, format string) (any, error) {
	if spec.JQ == "" {
		return nil, fmt.Errorf("empty jq expression in transform spec")
	}

	query, err := gojq.Parse(spec.JQ)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression %q: %w", spec.JQ, err)
	}

	var input any
	switch format {
	case "json":
		// Strict mode: unmarshal or return an error.
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			return nil, fmt.Errorf("cannot unmarshal JSON output for jq transform: %w", err)
		}
	default:
		// Auto-detect: try JSON first; fall back to {"text": raw} for non-JSON output.
		// This lets callers write jq expressions like `.hostname` directly on
		// JSON-producing commands without needing an explicit `format: json` hint.
		if err := json.Unmarshal([]byte(raw), &input); err != nil {
			input = map[string]any{"text": raw}
		}
	}

	return runJQ(query, input)
}

// applyPayloadTransform runs a jq expression against an already-parsed
// payload (from gNMI or NETCONF normalization). The payload is marshalled
// to JSON, fed through the jq expression, and the result replaces the
// original payload.
//
// This enables the same user-configured jq transforms that work on CLI
// output to also filter/reshape gNMI and NETCONF responses.
func applyPayloadTransform(_ context.Context, spec TransformSpec, payload any) (any, error) {
	if spec.JQ == "" {
		return nil, fmt.Errorf("empty jq expression in transform spec")
	}

	query, err := gojq.Parse(spec.JQ)
	if err != nil {
		return nil, fmt.Errorf("invalid jq expression %q: %w", spec.JQ, err)
	}

	// Marshal the structured payload to JSON then unmarshal into a generic
	// any so gojq can traverse it. This round-trip normalises Go types
	// (e.g. []models.InterfaceState) into map/slice/string/float64 that
	// gojq understands.
	raw, merr := json.Marshal(payload)
	if merr != nil {
		return nil, fmt.Errorf("cannot marshal payload for jq transform: %w", merr)
	}
	var input any
	if err := json.Unmarshal(raw, &input); err != nil {
		return nil, fmt.Errorf("cannot unmarshal payload for jq transform: %w", err)
	}

	return runJQ(query, input)
}

// runJQ executes a compiled jq query against an input value and collects
// results. Returns a single value for one result, []any for multiple,
// or nil for no results.
func runJQ(query *gojq.Query, input any) (any, error) {
	iter := query.Run(input)
	var results []any
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if jqErr, ok := v.(error); ok {
			return nil, fmt.Errorf("jq execution error: %w", jqErr)
		}
		results = append(results, v)
	}

	switch len(results) {
	case 0:
		return nil, nil
	case 1:
		return results[0], nil
	default:
		return results, nil
	}
}
