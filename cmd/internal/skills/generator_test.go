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
	"context"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/prompts"
	customprompt "github.com/adrien19/noc-foundry/internal/prompts/custom"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

type fakeToolConfig struct {
	resourceType string
}

func (c fakeToolConfig) ToolConfigType() string {
	return c.resourceType
}

func (c fakeToolConfig) Initialize(map[string]sources.Source) (tools.Tool, error) {
	return nil, nil
}

type fakeTool struct {
	name        string
	description string
	params      parameters.Parameters
	config      fakeToolConfig
	annotations *tools.ToolAnnotations
}

func (t fakeTool) Invoke(context.Context, tools.SourceProvider, parameters.ParamValues, tools.AccessToken) (any, util.NOCFoundryError) {
	return nil, nil
}

func (t fakeTool) EmbedParams(context.Context, parameters.ParamValues, map[string]embeddingmodels.EmbeddingModel) (parameters.ParamValues, error) {
	return parameters.ParamValues{}, nil
}

func (t fakeTool) Manifest() tools.Manifest {
	return tools.Manifest{
		Description: t.description,
		Parameters:  t.params.Manifest(),
	}
}

func (t fakeTool) McpManifest() tools.McpManifest {
	return tools.GetMcpManifest(t.name, t.description, nil, t.params, t.annotations)
}

func (t fakeTool) Authorized([]string) bool {
	return true
}

func (t fakeTool) RequiresClientAuthorization(tools.SourceProvider) (bool, error) {
	return false, nil
}

func (t fakeTool) ToConfig() tools.ToolConfig {
	return t.config
}

func (t fakeTool) GetAuthTokenHeaderName(tools.SourceProvider) (string, error) {
	return "Authorization", nil
}

func (t fakeTool) GetParameters() parameters.Parameters {
	return t.params
}

func makePrompt(t *testing.T) prompts.Prompt {
	t.Helper()
	cfg := customprompt.Config{
		Name:        "summarize-validation",
		Description: "Summarize a validation run for operators.",
		Arguments: prompts.Arguments{
			{
				Parameter: parameters.NewStringParameter("validation_result", "Validation result JSON."),
			},
		},
		Messages: []prompts.Message{
			{Role: "user", Content: "Summarize {{.validation_result}} for the operator."},
		},
	}
	prompt, err := cfg.Initialize()
	if err != nil {
		t.Fatalf("failed to initialize prompt: %v", err)
	}
	return prompt
}

func TestFormatParameters(t *testing.T) {
	tests := []struct {
		name         string
		params       []parameters.ParameterManifest
		envVars      map[string]string
		wantContains []string
		wantErr      bool
	}{
		{
			name:         "empty parameters",
			params:       []parameters.ParameterManifest{},
			wantContains: []string{""},
		},
		{
			name: "single required string parameter",
			params: []parameters.ParameterManifest{
				{
					Name:        "param1",
					Description: "A test parameter",
					Type:        "string",
					Required:    true,
				},
			},
			wantContains: []string{
				"#### Parameters",
				"| Name | Type | Description | Required | Default |",
				"| param1 | string | A test parameter | Yes |  |",
			},
		},
		{
			name: "parameter with env var default",
			params: []parameters.ParameterManifest{
				{
					Name:        "param1",
					Description: "Param 1",
					Type:        "string",
					Default:     "default-value",
					Required:    false,
				},
			},
			envVars: map[string]string{
				"MY_ENV_VAR": "default-value",
			},
			wantContains: []string{
				`param1 | string | Param 1 | No |  |`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := formatParameters(tt.params, tt.envVars)
			if (err != nil) != tt.wantErr {
				t.Errorf("formatParameters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(tt.params) == 0 && got != "" {
				t.Fatalf("expected empty output, got %q", got)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(got, want) {
					t.Errorf("formatParameters() missing %q\nGot:\n%s", want, got)
				}
			}
		})
	}
}

func TestGenerateSkillMarkdown(t *testing.T) {
	readOnly := tools.NewReadOnlyAnnotations()
	spec := skillSpec{
		Name:          "validation-skill",
		Description:   "Durable validation workflow bundle.",
		ToolsetName:   "validation-run-workflow",
		PromptsetName: "validation-guidance",
		Tools: []toolBinding{
			{
				Name: "start_validation_run_v4",
				Tool: fakeTool{
					name:        "start_validation_run_v4",
					description: "Start a validation run.",
					config:      fakeToolConfig{resourceType: toolTypeValidationStart},
				},
			},
			{
				Name: "show_fleet_interfaces",
				Tool: fakeTool{
					name:        "show_fleet_interfaces",
					description: "Inspect fleet interfaces.",
					params: parameters.Parameters{
						parameters.NewStringParameter("site", "Target site."),
					},
					config:      fakeToolConfig{resourceType: "nokia-show-interfaces"},
					annotations: readOnly,
				},
			},
		},
		Prompts: []promptBinding{
			{Name: "summarize-validation", Prompt: makePrompt(t)},
		},
		ExecutionArgs: []string{"--tools-file", "assets/tools.yaml"},
		EnvVarNames:   []string{"NETBOX_TOKEN"},
	}

	got, err := generateSkillMarkdown(spec, nil)
	if err != nil {
		t.Fatalf("generateSkillMarkdown() error = %v", err)
	}

	expected := []string{
		"name: validation-skill",
		"description: Durable validation workflow bundle.",
		"## Workflow",
		"1. Validation start: `start_validation_run_v4`",
		"2. Inspection: `show_fleet_interfaces`",
		"## Async Validation Guidance",
		"## Tools",
		"nocfoundry --tools-file assets/tools.yaml invoke show_fleet_interfaces",
		"Safety: read-only.",
		"## Prompts",
		"### summarize-validation",
		"#### Template Messages",
		"`user`: Summarize {{.validation_result}} for the operator.",
		"`NETBOX_TOKEN`",
	}

	for _, want := range expected {
		if !strings.Contains(got, want) {
			t.Errorf("generateSkillMarkdown() missing substring %q", want)
		}
	}
}

func TestGenerateSkillManifest(t *testing.T) {
	spec := skillSpec{
		Name:          "network-ops",
		Description:   "Workflow bundle.",
		ToolsetName:   "inspection",
		PromptsetName: "inspection-guidance",
		Tools: []toolBinding{
			{
				Name: "show_fleet_interfaces",
				Tool: fakeTool{
					name:        "show_fleet_interfaces",
					description: "Inspect fleet interfaces.",
					config:      fakeToolConfig{resourceType: "nokia-show-interfaces"},
					annotations: tools.NewReadOnlyAnnotations(),
				},
			},
		},
		Prompts: []promptBinding{
			{Name: "summarize-validation", Prompt: makePrompt(t)},
		},
		ExecutionArgs: []string{"--tools-file", "assets/tools.yaml"},
		Assets: []assetReference{
			{Type: "file", Path: "assets/tools.yaml"},
		},
	}

	got, err := generateSkillManifest(spec)
	if err != nil {
		t.Fatalf("generateSkillManifest() error = %v", err)
	}

	expected := []string{
		"apiVersion: noc-foundry/v1alpha1",
		"kind: Skill",
		"name: network-ops",
		"toolset: inspection",
		"promptset: inspection-guidance",
		"name: show_fleet_interfaces",
		"description: Inspect fleet interfaces.",
		"command: nocfoundry",
		"- --tools-file",
		"- assets/tools.yaml",
		"name: summarize-validation",
		"role: user",
		"content: Summarize {{.validation_result}} for the operator.",
	}

	for _, want := range expected {
		if !strings.Contains(string(got), want) {
			t.Errorf("generateSkillManifest() missing substring %q\nGot:\n%s", want, string(got))
		}
	}
}
