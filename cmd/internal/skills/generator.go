// Copyright 2026 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package skills

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/adrien19/noc-foundry/internal/prompts"
	customprompt "github.com/adrien19/noc-foundry/internal/prompts/custom"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	yaml "github.com/goccy/go-yaml"
)

const (
	toolTypeValidation       = "nokia-validate"
	toolTypeValidationStart  = "validation-run-start"
	toolTypeValidationStatus = "validation-run-status"
	toolTypeValidationResult = "validation-run-result"
	toolTypeValidationCancel = "validation-run-cancel"
	defaultJSONParamsExample = `{"param_name":"param_value"}`
	skillManifestAPIVersion  = "noc-foundry/v1alpha1"
	skillManifestKind        = "Skill"
)

type skillSpec struct {
	Name            string
	Description     string
	RawToolsetName  string
	ToolsetName     string
	PromptsetName   string
	Tools           []toolBinding
	Prompts         []promptBinding
	ExecutionArgs   []string
	Assets          []assetReference
	EnvVarNames     []string
	AdditionalNotes string
}

type toolBinding struct {
	Name string
	Tool tools.Tool
}

type promptBinding struct {
	Name   string
	Prompt prompts.Prompt
}

type assetReference struct {
	Type string `yaml:"type"`
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path,omitempty"`
}

type skillManifest struct {
	APIVersion  string                `yaml:"apiVersion"`
	Kind        string                `yaml:"kind"`
	Name        string                `yaml:"name"`
	Description string                `yaml:"description"`
	Toolset     string                `yaml:"toolset"`
	Promptset   string                `yaml:"promptset,omitempty"`
	Tools       []skillToolManifest   `yaml:"tools"`
	Prompts     []skillPromptManifest `yaml:"prompts,omitempty"`
	Execution   executionManifest     `yaml:"execution"`
	Assets      []assetReference      `yaml:"assets,omitempty"`
}

type executionManifest struct {
	Command string   `yaml:"command"`
	Args    []string `yaml:"args,omitempty"`
}

type skillToolManifest struct {
	Name        string                         `yaml:"name"`
	Description string                         `yaml:"description,omitempty"`
	Annotations *tools.ToolAnnotations         `yaml:"annotations,omitempty"`
	Parameters  []parameters.ParameterManifest `yaml:"parameters,omitempty"`
}

type skillPromptManifest struct {
	Name        string                         `yaml:"name"`
	Description string                         `yaml:"description,omitempty"`
	Arguments   []parameters.ParameterManifest `yaml:"arguments,omitempty"`
	Messages    []skillPromptMessage           `yaml:"messages,omitempty"`
}

type skillPromptMessage struct {
	Role    string `yaml:"role"`
	Content string `yaml:"content"`
}

func generateSkillMarkdown(spec skillSpec, envVars map[string]string) (string, error) {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", spec.Name))
	sb.WriteString(fmt.Sprintf("description: %s\n", spec.Description))
	sb.WriteString("---\n\n")

	sb.WriteString("## Overview\n\n")
	sb.WriteString(fmt.Sprintf("This agent skill packages the `%s` network operations workflow for NOCFoundry.\n\n", spec.ToolsetName))
	if spec.PromptsetName != "" {
		sb.WriteString(fmt.Sprintf("It includes the `%s` promptset so an agent can pair executable steps with workflow-specific reasoning guidance.\n\n", spec.PromptsetName))
	}

	sb.WriteString("## Preconditions\n\n")
	sb.WriteString("- `nocfoundry` must be available in `PATH`.\n")
	sb.WriteString("- Run commands from the generated skill directory so relative `assets/` paths resolve correctly.\n")
	if len(spec.ExecutionArgs) > 0 {
		sb.WriteString(fmt.Sprintf("- Base config arguments: `%s`\n", strings.Join(spec.ExecutionArgs, " ")))
	}
	if len(spec.EnvVarNames) > 0 {
		sb.WriteString(fmt.Sprintf("- Provide environment values for: `%s`.\n", strings.Join(spec.EnvVarNames, "`, `")))
	}
	if len(spec.Prompts) > 0 {
		sb.WriteString("- Prompts are bundle-native guidance for agents; NOCFoundry does not execute them directly.\n")
	}
	sb.WriteString("\n")

	sb.WriteString("## Workflow\n\n")
	for idx, binding := range spec.Tools {
		sb.WriteString(fmt.Sprintf("%d. %s: `%s`\n", idx+1, workflowStepLabel(binding.Tool), binding.Name))
		if desc := binding.Tool.Manifest().Description; desc != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", desc))
		}
	}
	sb.WriteString("\n")

	if hasValidationLifecycleTools(spec.Tools) {
		sb.WriteString("## Async Validation Guidance\n\n")
		sb.WriteString("- Start the validation run once, then poll status until the run reaches a terminal state.\n")
		sb.WriteString("- Fetch the terminal result after status reports completion or failure.\n")
		sb.WriteString("- Use cancellation only when the workflow needs to abort an in-flight validation.\n\n")
	}

	sb.WriteString("## Tools\n\n")
	for _, binding := range spec.Tools {
		manifest := binding.Tool.Manifest()
		sb.WriteString(fmt.Sprintf("### %s\n\n", binding.Name))
		if manifest.Description != "" {
			sb.WriteString(fmt.Sprintf("%s\n\n", manifest.Description))
		}
		sb.WriteString(fmt.Sprintf("Safety: %s\n\n", safetySummary(binding.Tool)))
		sb.WriteString("Invoke:\n\n")
		sb.WriteString("```bash\n")
		sb.WriteString(buildInvokeExample(spec.ExecutionArgs, binding.Name))
		sb.WriteString("\n```\n\n")
		parametersSchema, err := formatParameters(manifest.Parameters, envVars)
		if err != nil {
			return "", err
		}
		if parametersSchema != "" {
			sb.WriteString(parametersSchema)
			sb.WriteString("\n")
		}
	}

	if len(spec.Prompts) > 0 {
		sb.WriteString("## Prompts\n\n")
		for _, binding := range spec.Prompts {
			manifest := binding.Prompt.Manifest()
			sb.WriteString(fmt.Sprintf("### %s\n\n", binding.Name))
			if manifest.Description != "" {
				sb.WriteString(fmt.Sprintf("%s\n\n", manifest.Description))
			}
			argsTable, err := formatParameters(manifest.Arguments, nil)
			if err != nil {
				return "", err
			}
			if argsTable != "" {
				sb.WriteString(strings.Replace(argsTable, "#### Parameters", "#### Arguments", 1))
				sb.WriteString("\n")
			}
			messages := promptTemplateMessages(binding.Prompt)
			if len(messages) > 0 {
				sb.WriteString("#### Template Messages\n\n")
				for _, msg := range messages {
					sb.WriteString(fmt.Sprintf("- `%s`: %s\n", msg.Role, msg.Content))
				}
				sb.WriteString("\n")
			}
		}
	}

	if spec.AdditionalNotes != "" {
		sb.WriteString("## Additional Notes\n\n")
		sb.WriteString(spec.AdditionalNotes)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func generateSkillManifest(spec skillSpec) ([]byte, error) {
	manifest := skillManifest{
		APIVersion:  skillManifestAPIVersion,
		Kind:        skillManifestKind,
		Name:        spec.Name,
		Description: spec.Description,
		Toolset:     spec.ToolsetName,
		Promptset:   spec.PromptsetName,
		Tools:       make([]skillToolManifest, 0, len(spec.Tools)),
		Execution: executionManifest{
			Command: "nocfoundry",
			Args:    append([]string(nil), spec.ExecutionArgs...),
		},
		Assets: slices.Clone(spec.Assets),
	}

	for _, binding := range spec.Tools {
		mcp := binding.Tool.McpManifest()
		toolManifest := skillToolManifest{
			Name:        binding.Name,
			Description: binding.Tool.Manifest().Description,
			Annotations: mcp.Annotations,
			Parameters:  binding.Tool.Manifest().Parameters,
		}
		manifest.Tools = append(manifest.Tools, toolManifest)
	}

	for _, binding := range spec.Prompts {
		promptManifest := skillPromptManifest{
			Name:        binding.Name,
			Description: binding.Prompt.Manifest().Description,
			Arguments:   binding.Prompt.Manifest().Arguments,
		}
		for _, msg := range promptTemplateMessages(binding.Prompt) {
			promptManifest.Messages = append(promptManifest.Messages, skillPromptMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
		manifest.Prompts = append(manifest.Prompts, promptManifest)
	}

	return yaml.Marshal(manifest)
}

func buildInvokeExample(configArgs []string, toolName string) string {
	args := []string{"nocfoundry"}
	for _, arg := range configArgs {
		args = append(args, shellQuote(arg))
	}
	args = append(args, "invoke", shellQuote(toolName), shellQuote(defaultJSONParamsExample))
	return strings.Join(args, " ")
}

func promptTemplateMessages(prompt prompts.Prompt) []prompts.Message {
	switch cfg := prompt.ToConfig().(type) {
	case customprompt.Config:
		return slices.Clone(cfg.Messages)
	case *customprompt.Config:
		if cfg == nil {
			return nil
		}
		return slices.Clone(cfg.Messages)
	default:
		return nil
	}
}

func hasValidationLifecycleTools(toolsInSkill []toolBinding) bool {
	for _, binding := range toolsInSkill {
		switch binding.Tool.ToConfig().ToolConfigType() {
		case toolTypeValidationStart, toolTypeValidationStatus, toolTypeValidationResult, toolTypeValidationCancel:
			return true
		}
	}
	return false
}

func workflowStepLabel(tool tools.Tool) string {
	switch tool.ToConfig().ToolConfigType() {
	case toolTypeValidation:
		return "Validation"
	case toolTypeValidationStart:
		return "Validation start"
	case toolTypeValidationStatus:
		return "Status polling"
	case toolTypeValidationResult:
		return "Result retrieval"
	case toolTypeValidationCancel:
		return "Cancellation"
	default:
	}

	if annotation := tool.McpManifest().Annotations; annotation != nil {
		if annotation.ReadOnlyHint != nil && *annotation.ReadOnlyHint {
			if strings.HasPrefix(tool.McpManifest().Name, "list_") {
				return "Inventory"
			}
			return "Inspection"
		}
		if annotation.DestructiveHint != nil && *annotation.DestructiveHint {
			return "Change operation"
		}
	}
	return "Operation"
}

func safetySummary(tool tools.Tool) string {
	annotations := tool.McpManifest().Annotations
	if annotations == nil {
		return "No explicit safety hints are declared."
	}

	parts := make([]string, 0, 3)
	if annotations.ReadOnlyHint != nil {
		if *annotations.ReadOnlyHint {
			parts = append(parts, "read-only")
		} else {
			parts = append(parts, "not read-only")
		}
	}
	if annotations.DestructiveHint != nil && *annotations.DestructiveHint {
		parts = append(parts, "destructive")
	}
	if annotations.IdempotentHint != nil && *annotations.IdempotentHint {
		parts = append(parts, "idempotent")
	}
	if annotations.OpenWorldHint != nil && *annotations.OpenWorldHint {
		parts = append(parts, "open-world")
	}
	if len(parts) == 0 {
		return "No explicit safety hints are declared."
	}
	return strings.Join(parts, ", ") + "."
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"{}[]:$") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// formatParameters converts a list of parameter manifests into a formatted table string.
func formatParameters(params []parameters.ParameterManifest, envVars map[string]string) (string, error) {
	if len(params) == 0 {
		return "", nil
	}

	var sb strings.Builder
	sb.WriteString("#### Parameters\n\n")
	sb.WriteString("| Name | Type | Description | Required | Default |\n")
	sb.WriteString("| :--- | :--- | :--- | :--- | :--- |\n")

	for _, p := range params {
		required := "No"
		if p.Required {
			required = "Yes"
		}
		defaultValue := ""
		if p.Default != nil {
			defaultValue = fmt.Sprintf("`%v`", p.Default)
			if strVal, ok := p.Default.(string); ok {
				for _, envVal := range envVars {
					if envVal == strVal {
						defaultValue = ""
						break
					}
				}
			}
		}
		fmt.Fprintf(&sb, "| %s | %s | %s | %s | %s |\n", p.Name, p.Type, p.Description, required, defaultValue)
	}

	return sb.String(), nil
}

func sortedEnvNames(envVars map[string]string) []string {
	names := make([]string, 0, len(envVars))
	for name := range envVars {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
