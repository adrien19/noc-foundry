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

// Package showinterfaces implements the network-show-interfaces MCP tool,
// a vendor-agnostic tool that retrieves interface status and counters from
// any device that has a registered profile. It delegates to the query
// executor which routes through the best available protocol and uses the
// schema-driven canonical mapper for normalization.
package showinterfaces

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

const kind = "network-show-interfaces"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

// Config holds the YAML-decoded configuration for the network show interfaces tool.
type Config struct {
	Name           string                         `yaml:"name" validate:"required"`
	Type           string                         `yaml:"type" validate:"required"`
	Source         string                         `yaml:"source,omitempty"`
	SourceSelector *SourceSelector                `yaml:"sourceSelector,omitempty"`
	Parameters     parameters.Parameters          `yaml:"parameters,omitempty"`
	Description    string                         `yaml:"description"`
	AuthRequired   []string                       `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations         `yaml:"annotations"`
	Transforms     map[string]query.TransformSpec `yaml:"transforms,omitempty"`
}

// SourceSelector defines label-based device targeting for fleet operations.
type SourceSelector struct {
	MatchLabels    map[string]string `yaml:"matchLabels"`
	MaxConcurrency int               `yaml:"maxConcurrency,omitempty"`
	Template       string            `yaml:"template,omitempty"`
}

var _ tools.ToolConfig = Config{}

func (cfg Config) ToolConfigType() string {
	return kind
}

type compatibleSource = capabilities.SourceIdentity

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	if cfg.Source == "" && cfg.SourceSelector == nil {
		return nil, fmt.Errorf("tool %q must specify either 'source' or 'sourceSelector'", cfg.Name)
	}
	if cfg.Source != "" && cfg.SourceSelector != nil {
		return nil, fmt.Errorf("tool %q cannot specify both 'source' and 'sourceSelector'", cfg.Name)
	}

	if cfg.Source != "" {
		if rawS, ok := srcs[cfg.Source]; ok {
			if _, ok := rawS.(compatibleSource); !ok {
				return nil, fmt.Errorf("invalid source for %q tool: source %q does not expose vendor/platform identity", kind, cfg.Source)
			}
		}
	}

	desc := cfg.Description
	if desc == "" {
		if cfg.SourceSelector != nil {
			desc = "Show interface status and counters on network devices matching a label selector. Pass 'device' to target a specific device, or omit to query all matching devices."
		} else {
			desc = "Show interface status and counters on a network device. Returns canonical interface data via the best available protocol (gNMI, NETCONF, or CLI)."
		}
	}

	allParameters := cfg.Parameters
	if allParameters == nil {
		allParameters = parameters.Parameters{}
	}

	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	mcpManifest := tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, allParameters, annotations)

	executor := query.NewExecutor()
	if len(cfg.Transforms) > 0 {
		executor = executor.WithTransforms(query.TransformSet(cfg.Transforms))
	}

	return Tool{
		Config:      cfg,
		executor:    executor,
		manifest:    tools.Manifest{Description: desc, Parameters: allParameters.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: mcpManifest,
		Parameters:  allParameters,
	}, nil
}

// Tool implements the vendor-agnostic show interfaces operation.
type Tool struct {
	Config

	executor    *query.Executor
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	Parameters  parameters.Parameters
}

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	if t.Source != "" {
		return t.invokeSingle(ctx, resourceMgr, t.Source)
	}
	return t.invokeWithSelector(ctx, resourceMgr, params)
}

func (t Tool) invokeSingle(ctx context.Context, resourceMgr tools.SourceProvider, sourceName string) (any, util.NOCFoundryError) {
	rawSource, ok := resourceMgr.GetSource(sourceName)
	if !ok {
		return nil, util.NewClientServerError("unable to retrieve source", http.StatusInternalServerError, fmt.Errorf("source %q not found", sourceName))
	}

	record, err := t.executor.Execute(ctx, rawSource, profiles.OpGetInterfaces, sourceName)
	if err != nil {
		return nil, util.NewClientServerError("failed to execute get_interfaces operation", http.StatusInternalServerError, err)
	}
	return record, nil
}

func (t Tool) invokeWithSelector(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues) (any, util.NOCFoundryError) {
	sel := t.SourceSelector

	deviceName := ""
	for _, p := range params {
		if p.Name == "device" && p.Value != nil {
			if s, ok := p.Value.(string); ok && s != "" {
				deviceName = s
			}
		}
	}

	matchLabels := sel.MatchLabels
	if sel.Template != "" {
		merged := make(map[string]string, len(matchLabels)+1)
		for k, v := range matchLabels {
			merged[k] = v
		}
		merged["template"] = sel.Template
		matchLabels = merged
	}

	srcs, err := resourceMgr.GetSourcesByLabels(ctx, matchLabels)
	if err != nil {
		return nil, util.NewClientServerError("failed to resolve sources by selector", http.StatusInternalServerError, err)
	}
	if len(srcs) == 0 {
		return nil, util.NewClientServerError("no sources matched the selector", http.StatusNotFound, fmt.Errorf("sourceSelector matched 0 devices"))
	}

	srcs = pickBestPerDevice(srcs)

	if deviceName != "" {
		filtered := filterByDevice(srcs, deviceName)
		if len(filtered) == 0 {
			return nil, util.NewClientServerError(
				fmt.Sprintf("device %q not found among matched sources", deviceName),
				http.StatusNotFound,
				fmt.Errorf("device %q not in selector results", deviceName),
			)
		}
		for sn := range filtered {
			return t.invokeSingle(ctx, resourceMgr, sn)
		}
	}

	sourceNames := make([]string, 0, len(srcs))
	for name := range srcs {
		sourceNames = append(sourceNames, name)
	}

	maxConc := fanout.DefaultMaxConcurrency
	if sel.MaxConcurrency > 0 {
		maxConc = sel.MaxConcurrency
	}

	result := fanout.Execute(ctx, sourceNames, maxConc, func(ctx context.Context, sourceName string) (any, error) {
		rawSource, ok := resourceMgr.GetSource(sourceName)
		if !ok {
			return nil, fmt.Errorf("source %q not found", sourceName)
		}
		return t.executor.Execute(ctx, rawSource, profiles.OpGetInterfaces, sourceName)
	})

	return result, nil
}

func filterByDevice(srcs map[string]sources.Source, device string) map[string]sources.Source {
	filtered := make(map[string]sources.Source)
	for name, src := range srcs {
		if extractDevice(name) == device {
			filtered[name] = src
		}
	}
	return filtered
}

func extractDevice(sourceName string) string {
	first := -1
	for i, c := range sourceName {
		if c == '/' {
			if first < 0 {
				first = i
			} else {
				return sourceName[first+1 : i]
			}
		}
	}
	return sourceName
}

func pickBestPerDevice(srcs map[string]sources.Source) map[string]sources.Source {
	type candidate struct {
		name string
		prio int
	}
	best := make(map[string]candidate)
	for name := range srcs {
		device := extractDevice(name)
		prio := transportPriority(name)
		if c, ok := best[device]; !ok || prio < c.prio || (prio == c.prio && name < c.name) {
			best[device] = candidate{name: name, prio: prio}
		}
	}
	result := make(map[string]sources.Source, len(best))
	for _, c := range best {
		result[c.name] = srcs[c.name]
	}
	return result
}

func transportPriority(sourceName string) int {
	idx := strings.LastIndex(sourceName, "/")
	template := sourceName
	if idx >= 0 {
		template = sourceName[idx+1:]
	}
	switch template {
	case "netconf":
		return 0
	case "gnmi":
		return 1
	case "ssh":
		return 2
	default:
		return 3
	}
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
