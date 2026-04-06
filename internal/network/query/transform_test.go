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

package query_test

import (
	"context"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/query"
)

// TestWithTransforms_JQTransform_JSON verifies that a jq transform on an
// Executor overrides the built-in CLI parser and returns jq output.
func TestWithTransforms_JQTransform_JSON(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			// JSON path — must match the command + FormatArg in the profile.
			"show interface | as json": `{"interface":[{"name":"ethernet-1/1","admin-state":"enable","oper-state":"up"}]}`,
		},
	}

	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		profiles.OpGetInterfaces: {
			Format: "json",
			JQ:     `.interface[] | {name: .name, status: .["oper-state"]}`,
		},
	})

	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	// With the transform, payload is whatever jq returns (here a map).
	if record.Payload == nil {
		t.Fatal("expected non-nil payload from jq transform")
	}
	result, ok := record.Payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", record.Payload)
	}
	if result["name"] != "ethernet-1/1" {
		t.Errorf("name = %v, want %q", result["name"], "ethernet-1/1")
	}
}

// TestWithTransforms_JQTransform_TextFormat verifies that text-format output
// is wrapped as {"text": raw} before the jq expression runs.
func TestWithTransforms_JQTransform_TextFormat(t *testing.T) {
	// Set up a source that only exposes the text CLI command (no JSON path).
	// The executor will try "show interface | json" first (JSON path), fail,
	// then fall back to "show interface" (text path) and apply the transform.
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show interface": "raw text output from device",
		},
	}

	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		profiles.OpGetInterfaces: {
			JQ: `.text`,
		},
	})

	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}

	if record.Payload != "raw text output from device" {
		t.Errorf("payload = %v, want %q", record.Payload, "raw text output from device")
	}
}

// TestWithTransforms_InvalidJQ verifies that an invalid jq expression causes
// an error at transform time (not at WithTransforms time — compilation is lazy).
func TestWithTransforms_InvalidJQ(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			// JSON path — must match the profile's command + FormatArg.
			"show interface | as json": `{"interface":[]}`,
		},
	}

	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		profiles.OpGetInterfaces: {
			Format: "json",
			JQ:     `this is not valid jq $$$`,
		},
	})

	_, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err == nil {
		t.Fatal("expected error for invalid jq expression, got nil")
	}
}

// TestExecute_CLIFallback_FormatArg verifies that the FormatArg is appended to
// the command sent to the device (JSON path tried first, falls back to text).
func TestExecute_CLIFallback_FormatArg(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			// Only the text command is in the mock output map;
			// the JSON command ("show interface | as json") will return an error,
			// causing the executor to fall back to the text path.
			"show interface": `ethernet-1/1 is up, speed 25G, type None
  oper-status is up`,
		},
	}

	executor := query.NewExecutor()
	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if record.Source.Transport != "cli" {
		t.Errorf("Transport = %q, want %q", record.Source.Transport, "cli")
	}
}

// TestWithTransforms_FormatFilter_SkipsNonMatchingPath verifies that a
// transform with Format:"json" is NOT applied when the executor falls back
// to a text CLI path. In that case the built-in text parser runs instead,
// and the payload is the parsed []models.InterfaceState, not jq output.
func TestWithTransforms_FormatFilter_SkipsNonMatchingPath(t *testing.T) {
	// Source only exposes the text CLI path — the JSON path ("show interface | as json")
	// will fail, forcing fallback to the text path.
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show interface": `ethernet-1/1 is enable, speed 25G, type None
  oper-status is up`,
		},
	}

	// Transform targets json format only — must NOT fire on the text fallback.
	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		profiles.OpGetInterfaces: {
			Format: "json",
			JQ:     `.interface[] | .name`,
		},
	})

	record, err := executor.Execute(context.Background(), src, profiles.OpGetInterfaces, "test-src")
	if err != nil {
		t.Fatalf("Execute() unexpected error: %v", err)
	}

	// Payload must be the built-in text-parser output, not jq's string result.
	if _, ok := record.Payload.(string); ok {
		t.Errorf("payload is a string — jq transform incorrectly ran on the text path")
	}
	ifaces, ok := record.Payload.([]models.InterfaceState)
	if !ok {
		t.Fatalf("payload type = %T, want []models.InterfaceState (built-in text parser)", record.Payload)
	}
	if len(ifaces) == 0 {
		t.Error("expected at least one interface from built-in text parser")
	}
}

// TestExecuteCommand_AutoDetect_JSONOutput verifies that when no format hint is
// configured (format: ""), a JSON-producing command is auto-detected and jq
// receives the parsed object — no {"text": raw} wrapper needed.
func TestExecuteCommand_AutoDetect_JSONOutput(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version | as json": `{"hostname":"spine-1","version":"24.10.1"}`,
		},
	}

	// No format hint — the default "text" path auto-detects JSON.
	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		query.OpRunCommand: {JQ: `.hostname`},
	})

	record, err := executor.ExecuteCommand(context.Background(), src, "show version | as json", "test-src")
	if err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	if record.Payload != "spine-1" {
		t.Errorf("Payload = %v, want %q (auto-detected JSON → jq extracts .hostname)", record.Payload, "spine-1")
	}
	if record.Quality.MappingQuality != models.MappingDerived {
		t.Errorf("MappingQuality = %q, want %q", record.Quality.MappingQuality, models.MappingDerived)
	}
}

// TestExecuteCommand_AutoDetect_TextFallback verifies that non-JSON output still
// falls back to the {"text": raw} wrapper when no format hint is configured.
func TestExecuteCommand_AutoDetect_TextFallback(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version": "SR Linux v24.10.1 spine-1",
		},
	}

	// No format hint — non-JSON output wrapped as {"text": raw}.
	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		query.OpRunCommand: {JQ: `.text`},
	})

	record, err := executor.ExecuteCommand(context.Background(), src, "show version", "test-src")
	if err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	if record.Payload != "SR Linux v24.10.1 spine-1" {
		t.Errorf("Payload = %v, want raw text via .text", record.Payload)
	}
}

// TestExecuteCommand_StrictJSON_ErrorsOnInvalidJSON verifies that format: "json"
// (strict mode) returns an error when the output is not valid JSON, rather than
// silently falling back to the text wrapper.
func TestExecuteCommand_StrictJSON_ErrorsOnInvalidJSON(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version": "not json at all",
		},
	}

	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		query.OpRunCommand: {Format: "json", JQ: `.hostname`},
	})

	_, err := executor.ExecuteCommand(context.Background(), src, "show version", "test-src")
	if err == nil {
		t.Fatal("expected error for strict format:json on non-JSON output")
	}
}

// TestExecuteCommand_NoJQ_JSONAutoDetected verifies that JSON output is parsed
// into a structured payload even with no jq expression configured. The caller
// should not need to pass jq: "." just to receive a parsed JSON object.
func TestExecuteCommand_NoJQ_JSONAutoDetected(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version | as json": `{"hostname":"spine-1","version":"24.10.1"}`,
		},
	}

	// No transforms at all — bare ad-hoc mode.
	executor := query.NewExecutor()

	record, err := executor.ExecuteCommand(context.Background(), src, "show version | as json", "test-src")
	if err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	m, ok := record.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload type = %T, want map[string]any (auto-detected JSON)", record.Payload)
	}
	if m["hostname"] != "spine-1" {
		t.Errorf("Payload[hostname] = %v, want %q", m["hostname"], "spine-1")
	}
	if record.Quality.MappingQuality != models.MappingPartial {
		t.Errorf("MappingQuality = %q, want partial (no jq transform)", record.Quality.MappingQuality)
	}
}

// TestExecuteCommand_NoJQ_FormatJSON_StrictParsed verifies that format: json
// (no jq) parses the output as a structured JSON payload without requiring
// a jq: "." workaround.
func TestExecuteCommand_NoJQ_FormatJSON_StrictParsed(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version | as json": `{"hostname":"spine-1"}`,
		},
	}

	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		query.OpRunCommand: {Format: "json"}, // strict, no jq
	})

	record, err := executor.ExecuteCommand(context.Background(), src, "show version | as json", "test-src")
	if err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	m, ok := record.Payload.(map[string]any)
	if !ok {
		t.Fatalf("Payload type = %T, want map[string]any", record.Payload)
	}
	if m["hostname"] != "spine-1" {
		t.Errorf("Payload[hostname] = %v, want %q", m["hostname"], "spine-1")
	}
}

// TestExecuteCommand_NoJQ_FormatJSON_StrictErrors verifies that format: json
// (no jq) returns an error when the output is not valid JSON.
func TestExecuteCommand_NoJQ_FormatJSON_StrictErrors(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version": "not json",
		},
	}

	executor := query.NewExecutor().WithTransforms(query.TransformSet{
		query.OpRunCommand: {Format: "json"}, // strict, no jq
	})

	_, err := executor.ExecuteCommand(context.Background(), src, "show version", "test-src")
	if err == nil {
		t.Fatal("expected error: format: json but output is not valid JSON")
	}
}

// TestExecuteCommand_NoJQ_TextOutput verifies that plain-text output with no
// transforms annotation returns the raw string as payload.
func TestExecuteCommand_NoJQ_TextOutput(t *testing.T) {
	src := &mockCLISource{
		vendor:   "nokia",
		platform: "srlinux",
		output: map[string]string{
			"show version": "SR Linux v24.10.1 spine-1",
		},
	}

	record, err := query.NewExecutor().ExecuteCommand(context.Background(), src, "show version", "test-src")
	if err != nil {
		t.Fatalf("ExecuteCommand() error: %v", err)
	}

	if record.Payload != "SR Linux v24.10.1 spine-1" {
		t.Errorf("Payload = %v, want raw string", record.Payload)
	}
}
