// Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package showroutes implements the network-show-routes MCP tool.
// This tool retrieves the IP routing table from any device with a registered profile.
// It delegates to the query executor, which routes through the best available protocol
// and uses the schema-driven canonical mapper for normalization.
package showroutes

import (
	"context"
	"fmt"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/tools/network/profilequery"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

const kind = "network-show-routes"

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name, Type: kind}
	if decoder != nil {
		if err := decoder.DecodeContext(ctx, &actual); err != nil {
			return nil, err
		}
	}
	return actual, nil
}

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

type Config struct {
	Name           string                       `yaml:"name" validate:"required"`
	Type           string                       `yaml:"type" validate:"required"`
	Source         string                       `yaml:"source,omitempty"`
	SourceSelector *profilequery.SourceSelector `yaml:"sourceSelector,omitempty"`
	Description    string                       `yaml:"description"`
	AuthRequired   []string                     `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations       `yaml:"annotations"`
}

var _ tools.ToolConfig = Config{}

func (cfg Config) ToolConfigType() string { return kind }

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	if err := profilequery.ValidateConfig(cfg.Name, kind, cfg.Source, cfg.SourceSelector, srcs); err != nil {
		return nil, err
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Show IP routing table on a network device. Returns canonical route data via the best available protocol (gNMI, NETCONF, or CLI)."
	}
	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	var allParameters parameters.Parameters
	if cfg.SourceSelector != nil {
		allParameters = append(allParameters, parameters.NewStringParameterWithRequired("device", "Optional device name to query within the source selector.", false))
	}
	allParameters = append(allParameters,
		parameters.NewStringParameterWithRequired("prefix", "Optional route prefix filter.", false),
		parameters.NewStringParameterWithRequired("protocol", "Optional route protocol filter.", false),
		parameters.NewStringParameterWithRequired("network_instance", "Optional network instance/VRF filter.", false),
	)
	mcpManifest := tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, allParameters, annotations)
	return Tool{
		Config:      cfg,
		executor:    query.NewExecutor(),
		manifest:    tools.Manifest{Description: desc, Parameters: allParameters.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: mcpManifest,
		Parameters:  allParameters,
	}, nil
}

type Tool struct {
	Config
	executor    *query.Executor
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	Parameters  parameters.Parameters
}

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	return profilequery.Invoke(ctx, resourceMgr, t.executor, t.Source, t.SourceSelector, profiles.OpGetRouteTable, params)
}

func (t Tool) Manifest() tools.Manifest          { return t.manifest }
func (t Tool) McpManifest() tools.McpManifest    { return t.mcpManifest }
func (t Tool) Authorized(services []string) bool { return tools.IsAuthorized(t.AuthRequired, services) }
func (t Tool) RequiresClientAuthorization(resourceMgr tools.SourceProvider) (bool, error) {
	return false, nil
}
func (t Tool) ToConfig() tools.ToolConfig { return t.Config }
func (t Tool) GetAuthTokenHeaderName(resourceMgr tools.SourceProvider) (string, error) {
	return "Authorization", nil
}
func (t Tool) GetParameters() parameters.Parameters { return t.Parameters }
func (t Tool) EmbedParams(ctx context.Context, paramValues parameters.ParamValues, embeddingModelsMap map[string]embeddingmodels.EmbeddingModel) (parameters.ParamValues, error) {
	return paramValues, nil
}
