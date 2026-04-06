// Copyright 2026 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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

package skills

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/cmd/internal"
	_ "github.com/adrien19/noc-foundry/internal/prompts/custom"
	_ "github.com/adrien19/noc-foundry/internal/sources/http"
	_ "github.com/adrien19/noc-foundry/internal/tools/http"
	"github.com/spf13/cobra"
)

func invokeCommand(args []string) (string, error) {
	parentCmd := &cobra.Command{Use: "nocfoundry"}

	buf := new(bytes.Buffer)
	opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
	internal.PersistentFlags(parentCmd, opts)

	cmd := NewCommand(opts)
	parentCmd.AddCommand(cmd)
	parentCmd.SetArgs(args)

	err := parentCmd.Execute()
	return buf.String(), err
}

func writeToolsFile(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "tools.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}
	return path
}

func TestGenerateSkill_WorkflowBundle(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "skills")
	toolsFilePath := writeToolsFile(t, tmpDir, `
---
kind: sources
name: my-http
type: http
baseUrl: https://example.com

---
kind: tools
name: hello-http
type: http
source: my-http
description: hello tool
method: GET
path: /hello

---
kind: prompts
name: summarize-http
description: Summarize the workflow output for an operator.
arguments:
  - name: result_json
    description: Result payload.
messages:
  - role: user
    content: Summarize {{.result_json}} for an operator.

---
kind: promptsets
name: inspection-guidance
prompts:
  - summarize-http

---
kind: toolsets
name: inspection-workflow
description: Inspect the HTTP endpoint and summarize the result.
promptset: inspection-guidance
tools:
  - hello-http
`)

	args := []string{
		"skills-generate",
		"--tools-file", toolsFilePath,
		"--output-dir", outputDir,
		"--name", "inspection-skill",
		"--toolset", "inspection-workflow",
	}

	got, err := invokeCommand(args)
	if err != nil {
		t.Fatalf("command failed: %v\nOutput: %s", err, got)
	}

	skillPath := filepath.Join(outputDir, "inspection-skill")
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		t.Fatalf("skill directory not created: %s", skillPath)
	}

	if _, err := os.Stat(filepath.Join(skillPath, "scripts")); !os.IsNotExist(err) {
		t.Fatalf("scripts directory should not exist in workflow bundle")
	}

	skillMarkdown, err := os.ReadFile(filepath.Join(skillPath, "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read SKILL.md: %v", err)
	}
	for _, want := range []string{
		"## Workflow",
		"## Tools",
		"## Prompts",
		"nocfoundry --tools-file assets/tools.yaml invoke hello-http",
		"Safety:",
		"Summarize {{.result_json}} for an operator.",
	} {
		if !strings.Contains(string(skillMarkdown), want) {
			t.Errorf("SKILL.md missing %q\n%s", want, string(skillMarkdown))
		}
	}

	manifestContent, err := os.ReadFile(filepath.Join(skillPath, "skill.yaml"))
	if err != nil {
		t.Fatalf("failed to read skill.yaml: %v", err)
	}
	for _, want := range []string{
		"apiVersion: noc-foundry/v1alpha1",
		"kind: Skill",
		"name: inspection-skill",
		"toolset: inspection-workflow",
		"promptset: inspection-guidance",
		"path: assets/tools.yaml",
	} {
		if !strings.Contains(string(manifestContent), want) {
			t.Errorf("skill.yaml missing %q\n%s", want, string(manifestContent))
		}
	}

	if _, err := os.Stat(filepath.Join(skillPath, "assets", "tools.yaml")); os.IsNotExist(err) {
		t.Fatalf("asset file not created")
	}
}

func TestGenerateSkill_PerToolsetByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "skills")
	toolsFilePath := writeToolsFile(t, tmpDir, `
---
kind: sources
name: my-http
type: http
baseUrl: https://example.com

---
kind: tools
name: hello-http
type: http
source: my-http
description: hello tool
method: GET
path: /hello

---
kind: tools
name: bye-http
type: http
source: my-http
description: bye tool
method: GET
path: /bye

---
kind: toolsets
name: inspection
description: Inspection workflow.
tools:
  - hello-http

---
kind: toolsets
name: triage
description: Triage workflow.
tools:
  - bye-http
`)

	args := []string{
		"skills-generate",
		"--tools-file", toolsFilePath,
		"--output-dir", outputDir,
		"--name", "network-ops",
	}

	got, err := invokeCommand(args)
	if err != nil {
		t.Fatalf("command failed: %v\nOutput: %s", err, got)
	}

	for _, skillName := range []string{"network-ops-inspection", "network-ops-triage"} {
		if _, err := os.Stat(filepath.Join(outputDir, skillName)); os.IsNotExist(err) {
			t.Fatalf("expected generated skill %q", skillName)
		}
	}
}

func TestGenerateSkill_FallbackAllTools(t *testing.T) {
	tmpDir := t.TempDir()
	outputDir := filepath.Join(tmpDir, "skills")
	toolsFilePath := writeToolsFile(t, tmpDir, `
sources:
  my-http:
    kind: http
    baseUrl: https://example.com
tools:
  hello-http:
    kind: http
    source: my-http
    description: hello tool
    method: GET
    path: /hello
`)

	args := []string{
		"skills-generate",
		"--tools-file", toolsFilePath,
		"--output-dir", outputDir,
		"--name", "fallback-skill",
	}

	got, err := invokeCommand(args)
	if err != nil {
		t.Fatalf("command failed: %v\nOutput: %s", err, got)
	}

	if _, err := os.Stat(filepath.Join(outputDir, "fallback-skill")); os.IsNotExist(err) {
		t.Fatalf("fallback skill directory not created")
	}
}

func TestGenerateSkill_MissingPromptset(t *testing.T) {
	tmpDir := t.TempDir()
	toolsFilePath := writeToolsFile(t, tmpDir, `
---
kind: sources
name: my-http
type: http
baseUrl: https://example.com

---
kind: tools
name: hello-http
type: http
source: my-http
description: hello tool
method: GET
path: /hello

---
kind: toolsets
name: inspection-workflow
description: Broken workflow.
promptset: missing-promptset
tools:
  - hello-http
`)

	args := []string{
		"skills-generate",
		"--tools-file", toolsFilePath,
		"--name", "broken-skill",
		"--toolset", "inspection-workflow",
	}

	got, err := invokeCommand(args)
	if err == nil {
		t.Fatalf("expected command to fail\nOutput: %s", got)
	}
	if !strings.Contains(got, `missing-promptset`) && !strings.Contains(err.Error(), `missing-promptset`) {
		t.Fatalf("expected missing promptset error, got output=%q err=%v", got, err)
	}
}

func TestGenerateSkill_MissingArguments(t *testing.T) {
	tmpDir := t.TempDir()
	toolsFilePath := writeToolsFile(t, tmpDir, "tools: {}")

	got, err := invokeCommand([]string{"skills-generate", "--tools-file", toolsFilePath})
	if err == nil {
		t.Fatalf("expected command to fail due to missing --name, got output: %s", got)
	}
}
