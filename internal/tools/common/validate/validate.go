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

// Package validate implements the validate MCP tool, a read-only
// declarative validation engine for network devices and blast-radius checks.
package validate

import (
	"context"
	"fmt"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validation"
	"github.com/goccy/go-yaml"
)

const kind = "validate"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
	if !validation.RegisterCompiledRuntime(kind, func(compiled validation.CompiledRun) (validation.CompiledRuntime, error) {
		return Tool{}, nil
	}) {
		panic(fmt.Sprintf("compiled runtime for %q already registered", kind))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

type Config struct {
	Name           string                       `yaml:"name" validate:"required"`
	Type           string                       `yaml:"type" validate:"required"`
	Source         string                       `yaml:"source,omitempty"`
	SourceSelector *SourceSelector              `yaml:"sourceSelector,omitempty"`
	Description    string                       `yaml:"description"`
	AuthRequired   []string                     `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations       `yaml:"annotations"`
	Defaults       *Defaults                    `yaml:"defaults,omitempty"`
	Groups         map[string]TargetGroup       `yaml:"groups,omitempty"`
	UseProfile     string                       `yaml:"useProfile,omitempty"`
	Profiles       map[string]ValidationProfile `yaml:"profiles,omitempty"`
	Templates      map[string]StepTemplate      `yaml:"templates,omitempty"`
	Phases         []Phase                      `yaml:"phases,omitempty"`
}

var _ tools.ToolConfig = Config{}


func (cfg Config) ToolConfigType() string {
	return kind
}

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	resolved, err := cfg.expandProfilesAndTemplates()
	if err != nil {
		return nil, err
	}
	if resolved.Source == "" && resolved.SourceSelector == nil {
		return nil, fmt.Errorf("tool %q must specify either 'source' or 'sourceSelector'", resolved.Name)
	}
	if resolved.Source != "" && resolved.SourceSelector != nil {
		return nil, fmt.Errorf("tool %q cannot specify both 'source' and 'sourceSelector'", resolved.Name)
	}
	if len(resolved.Phases) == 0 {
		return nil, fmt.Errorf("tool %q must define at least one phase", resolved.Name)
	}

	compiled, err := resolved.compileConfig(srcs)
	if err != nil {
		return nil, err
	}

	desc := resolved.Description
	if desc == "" {
		desc = "Validate network device or blast-radius state across one or more collection and assertion phases."
	}
	allParameters := buildParameters(resolved)
	annotations := tools.GetAnnotationsOrDefault(resolved.Annotations, tools.NewReadOnlyAnnotations)
	mcpManifest := tools.GetMcpManifest(resolved.Name, desc, resolved.AuthRequired, allParameters, annotations)

	return Tool{
		Config:       resolved,
		compiled:     compiled,
		manifest:     tools.Manifest{Description: desc, Parameters: allParameters.Manifest(), AuthRequired: resolved.AuthRequired},
		mcpManifest:  mcpManifest,
		Parameters:   allParameters,
		baseExecutor: query.NewExecutor(),
	}, nil
}

func buildParameters(cfg Config) parameters.Parameters {
	var params parameters.Parameters
	if len(cfg.Phases) > 1 {
		params = append(params, parameters.NewStringParameterWithRequired(
			"phase",
			"Validation phase to execute. Required when the tool defines multiple phases.",
			true,
		))
	}
	if cfg.SourceSelector != nil {
		params = append(params, parameters.NewStringParameterWithRequired(
			"device",
			"Device name to narrow selector scope to a single device.",
			false,
		))
	}
	return params
}

func (cfg Config) expandProfilesAndTemplates() (Config, error) {
	resolved := cfg
	if cfg.UseProfile != "" && len(cfg.Phases) > 0 {
		return Config{}, fmt.Errorf("tool %q cannot define both useProfile and phases", cfg.Name)
	}
	if cfg.UseProfile != "" {
		profile, ok := cfg.Profiles[cfg.UseProfile]
		if !ok {
			return Config{}, fmt.Errorf("tool %q references unknown profile %q", cfg.Name, cfg.UseProfile)
		}
		if resolved.Defaults == nil && profile.Defaults != nil {
			resolved.Defaults = profile.Defaults
		}
		resolved.Phases = make([]Phase, 0, len(profile.Phases))
		for _, pt := range profile.Phases {
			phase := Phase{Name: pt.Name}
			for _, st := range pt.Steps {
				phase.Steps = append(phase.Steps, stepFromTemplate(st))
			}
			resolved.Phases = append(resolved.Phases, phase)
		}
	}

	if len(resolved.Phases) == 0 {
		return resolved, nil
	}
	expanded := make([]Phase, 0, len(resolved.Phases))
	for _, phase := range resolved.Phases {
		next := Phase{Name: phase.Name, Description: phase.Description}
		for _, name := range phase.Use {
			template, ok := cfg.Templates[name]
			if !ok {
				return Config{}, fmt.Errorf("phase %q references unknown template %q", phase.Name, name)
			}
			next.Steps = append(next.Steps, stepFromTemplate(template))
		}
		next.Steps = append(next.Steps, phase.Steps...)
		expanded = append(expanded, next)
	}
	resolved.Phases = expanded
	return resolved, nil
}

func stepFromTemplate(st StepTemplate) Step {
	return Step(st)
}

// Tool is the initialized, ready-to-invoke validation tool instance.
type Tool struct {
	Config

	compiled     compiledConfig
	manifest     tools.Manifest
	mcpManifest  tools.McpManifest
	Parameters   parameters.Parameters
	baseExecutor *query.Executor
}

func (t Tool) EmbedParams(ctx context.Context, paramValues parameters.ParamValues, embeddingModelsMap map[string]embeddingmodels.EmbeddingModel) (parameters.ParamValues, error) {
	return parameters.EmbedParams(ctx, t.Parameters, paramValues, embeddingModelsMap, nil)
}

func (t Tool) Manifest() tools.Manifest {
	return t.manifest
}

func (t Tool) McpManifest() tools.McpManifest {
	return t.mcpManifest
}

func (t Tool) Authorized(services []string) bool {
	return tools.IsAuthorized(t.AuthRequired, services)
}

func (t Tool) RequiresClientAuthorization(resourceMgr tools.SourceProvider) (bool, error) {
	return false, nil
}

func (t Tool) ToConfig() tools.ToolConfig {
	return t.Config
}

func (t Tool) GetAuthTokenHeaderName(resourceMgr tools.SourceProvider) (string, error) {
	return "Authorization", nil
}

func (t Tool) GetParameters() parameters.Parameters {
	return t.Parameters
}
