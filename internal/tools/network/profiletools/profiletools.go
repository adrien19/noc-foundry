package profiletools

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

type toolSpec struct {
	kind        string
	operationID string
	description string
	params      func() parameters.Parameters
}

var specs = []toolSpec{
	{"network-show-lldp", profiles.OpGetLLDPNeighbors, "Show LLDP neighbors on a network device.", optionalStringParams("interface")},
	{"network-show-bgp-routes", profiles.OpGetBGPRIB, "Show BGP routes on a network device.", bgpRouteParams},
	{"network-show-ospf-neighbors", profiles.OpGetOSPFNeighbors, "Show OSPF neighbors on a network device.", nil},
	{"network-show-isis-adjacencies", profiles.OpGetISISAdjacencies, "Show IS-IS adjacencies on a network device.", nil},
	{"network-show-platform", profiles.OpGetPlatformComponents, "Show platform components on a network device.", optionalStringParams("component_type")},
	{"network-show-optics", profiles.OpGetTransceiverState, "Show transceiver optics state on a network device.", optionalStringParams("interface")},
	{"network-show-acl", profiles.OpGetACL, "Show ACL configuration and counters on a network device.", optionalStringParams("acl_name", "interface")},
	{"network-show-qos", profiles.OpGetQoSInterfaces, "Show QoS interface state on a network device.", optionalStringParams("interface")},
	{"network-show-routing-policy", profiles.OpGetRoutingPolicy, "Show routing policy definitions on a network device.", optionalStringParams("policy_name")},
	{"network-show-logs", profiles.OpGetLogEntries, "Show recent log entries on a network device.", logParams},
	{"network-show-config", profiles.OpGetConfigSection, "Show a targeted read-only configuration section on a network device.", configParams},
}

func init() {
	for _, spec := range specs {
		spec := spec
		if !tools.Register(spec.kind, func(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
			return newConfig(ctx, name, decoder, spec)
		}) {
			panic(fmt.Sprintf("tool kind %q already registered", spec.kind))
		}
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
	spec           toolSpec
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder, spec toolSpec) (tools.ToolConfig, error) {
	actual := Config{Name: name, Type: spec.kind, spec: spec}
	if decoder != nil {
		if err := decoder.DecodeContext(ctx, &actual); err != nil {
			return nil, err
		}
	}
	actual.spec = spec
	return actual, nil
}

func (cfg Config) ToolConfigType() string { return cfg.spec.kind }

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	if err := profilequery.ValidateConfig(cfg.Name, cfg.spec.kind, cfg.Source, cfg.SourceSelector, srcs); err != nil {
		return nil, err
	}
	desc := cfg.Description
	if desc == "" {
		desc = cfg.spec.description
	}
	params := parameters.Parameters{}
	if cfg.spec.params != nil {
		params = append(params, cfg.spec.params()...)
	}
	if cfg.SourceSelector != nil {
		params = append(params, parameters.NewStringParameterWithRequired("device", "Optional device name to query within the source selector.", false))
	}
	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	return Tool{
		Config:      cfg,
		executor:    query.NewExecutor(),
		manifest:    tools.Manifest{Description: desc, Parameters: params.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, params, annotations),
		Parameters:  params,
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
	return profilequery.Invoke(ctx, resourceMgr, t.executor, t.Source, t.SourceSelector, t.spec.operationID, params)
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

func optionalStringParams(names ...string) func() parameters.Parameters {
	return func() parameters.Parameters {
		out := parameters.Parameters{}
		for _, name := range names {
			out = append(out, parameters.NewStringParameterWithRequired(name, "Optional "+name+" filter.", false))
		}
		return out
	}
}

func bgpRouteParams() parameters.Parameters {
	return parameters.Parameters{
		parameters.NewStringParameter("afi_safi", "BGP address family to query, such as ipv4-unicast or ipv6-unicast."),
		parameters.NewStringParameterWithRequired("prefix", "Optional prefix filter.", false),
		parameters.NewStringParameterWithRequired("network_instance", "Optional network instance/VRF filter.", false),
	}
}

func logParams() parameters.Parameters {
	return parameters.Parameters{
		parameters.NewStringParameterWithRequired("severity", "Optional severity filter.", false),
		parameters.NewStringParameterWithRequired("since", "Optional lower timestamp bound.", false),
		parameters.NewIntParameterWithDefault("count", 100, "Maximum number of log entries to return."),
	}
}

func configParams() parameters.Parameters {
	return parameters.Parameters{
		parameters.NewStringParameter("path", "YANG/config path to retrieve."),
		parameters.NewStringParameterWithRequired("section", "Named configuration section to retrieve.", false),
	}
}
