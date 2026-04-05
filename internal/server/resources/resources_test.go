// Copyright 2025 Google LLC
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

package resources_test

import (
	"context"
	"testing"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/server/resources"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/trace"
)

type mockSourceConfig struct {
	typ string
}

func (c mockSourceConfig) SourceConfigType() string {
	return c.typ
}

func (c mockSourceConfig) Initialize(_ context.Context, _ trace.Tracer) (sources.Source, error) {
	return &mockSource{}, nil
}

type mockSource struct {
}

func (s *mockSource) SourceType() string {
	return "mock-source"
}

func (s *mockSource) ToConfig() sources.SourceConfig {
	return mockSourceConfig{typ: "mock-source"}
}

func TestUpdateServer(t *testing.T) {
	newSources := map[string]sources.Source{
		"example-source": &mockSource{},
	}
	newAuth := map[string]auth.AuthService{"example-auth": nil}
	newEmbeddingModels := map[string]embeddingmodels.EmbeddingModel{"example-model": nil}
	newTools := map[string]tools.Tool{"example-tool": nil}
	newToolsets := map[string]tools.Toolset{
		"example-toolset": {
			ToolsetConfig: tools.ToolsetConfig{
				Name: "example-toolset",
			},
			Tools: []*tools.Tool{},
		},
	}
	newPrompts := map[string]prompts.Prompt{"example-prompt": nil}
	newPromptsets := map[string]prompts.Promptset{
		"example-promptset": {
			PromptsetConfig: prompts.PromptsetConfig{
				Name: "example-promptset",
			},
			Prompts: []*prompts.Prompt{},
		},
	}
	resMgr := resources.NewResourceManager(newSources, newAuth, newEmbeddingModels, newTools, newToolsets, newPrompts, newPromptsets)

	gotSource, _ := resMgr.GetSource("example-source")
	if diff := cmp.Diff(gotSource, newSources["example-source"]); diff != "" {
		t.Errorf("error updating server, sources (-want +got):\n%s", diff)
	}

	gotAuthService, _ := resMgr.GetAuthService("example-auth")
	if diff := cmp.Diff(gotAuthService, newAuth["example-auth"]); diff != "" {
		t.Errorf("error updating server, authServices (-want +got):\n%s", diff)
	}

	gotTool, _ := resMgr.GetTool("example-tool")
	if diff := cmp.Diff(gotTool, newTools["example-tool"]); diff != "" {
		t.Errorf("error updating server, tools (-want +got):\n%s", diff)
	}

	gotToolset, _ := resMgr.GetToolset("example-toolset")
	if diff := cmp.Diff(gotToolset, newToolsets["example-toolset"]); diff != "" {
		t.Errorf("error updating server, toolset (-want +got):\n%s", diff)
	}

	gotPrompt, _ := resMgr.GetPrompt("example-prompt")
	if diff := cmp.Diff(gotPrompt, newPrompts["example-prompt"]); diff != "" {
		t.Errorf("error updating server, prompts (-want +got):\n%s", diff)
	}

	gotPromptset, _ := resMgr.GetPromptset("example-promptset")
	if diff := cmp.Diff(gotPromptset, newPromptsets["example-promptset"]); diff != "" {
		t.Errorf("error updating server, promptset (-want +got):\n%s", diff)
	}

	updateSource := map[string]sources.Source{
		"example-source2": &mockSource{},
	}

	resMgr.SetResources(updateSource, newAuth, newEmbeddingModels, newTools, newToolsets, newPrompts, newPromptsets)
	gotSource, _ = resMgr.GetSource("example-source2")
	if diff := cmp.Diff(gotSource, updateSource["example-source2"]); diff != "" {
		t.Errorf("error updating server, sources (-want +got):\n%s", diff)
	}
}
