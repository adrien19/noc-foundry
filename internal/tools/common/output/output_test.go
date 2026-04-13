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

package output_test

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/tools/common/output"
)

func TestNewToolResult(t *testing.T) {
	cr := models.CommandResult{
		RawOutput: "some output",
		Command:   "show interface",
		ToolKind:  "network-show-interfaces",
		Source:    "my-device",
	}

	result := output.NewToolResult(cr)

	raw, ok := result["raw_output"].(string)
	if !ok || raw != "some output" {
		t.Fatalf("raw_output = %v, want %q", result["raw_output"], "some output")
	}

	meta, ok := result["metadata"].(map[string]string)
	if !ok {
		t.Fatal("metadata is not map[string]string")
	}
	if meta["command"] != "show interface" {
		t.Errorf("command = %q, want %q", meta["command"], "show interface")
	}
	if meta["tool"] != "network-show-interfaces" {
		t.Errorf("tool = %q, want %q", meta["tool"], "network-show-interfaces")
	}
	if meta["source"] != "my-device" {
		t.Errorf("source = %q, want %q", meta["source"], "my-device")
	}
}
