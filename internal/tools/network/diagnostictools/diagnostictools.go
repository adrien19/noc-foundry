package diagnostictools

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

var diagSafeRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_\-.:@]*$`)

type diagSpec struct {
	kind        string
	description string
	params      parameters.Parameters
	command     func(capabilities.SourceIdentity, parameters.ParamValues) (string, string, error)
}

var diagSpecs = []diagSpec{
	{
		kind:        "network-ping",
		description: "Run a read-only ping from a network device.",
		params: parameters.Parameters{
			parameters.NewStringParameter("target", "Target address or hostname."),
			parameters.NewStringParameterWithRequired("source", "Optional source address or interface.", false),
			parameters.NewStringParameterWithRequired("vrf", "Optional network instance/VRF.", false),
			parameters.NewIntParameterWithDefault("count", 5, "Packet count."),
		},
		command: buildPingCommand,
	},
	{
		kind:        "network-traceroute",
		description: "Run a read-only traceroute from a network device.",
		params: parameters.Parameters{
			parameters.NewStringParameter("target", "Target address or hostname."),
			parameters.NewStringParameterWithRequired("source", "Optional source address or interface.", false),
			parameters.NewStringParameterWithRequired("vrf", "Optional network instance/VRF.", false),
			parameters.NewIntParameterWithDefault("max_hops", 30, "Maximum hop count."),
		},
		command: buildTracerouteCommand,
	},
	{
		kind:        "network-show-config-diff",
		description: "Show a read-only configuration diff on a network device.",
		params: parameters.Parameters{
			parameters.NewStringParameterWithRequired("source", "Source datastore, default running.", false),
			parameters.NewStringParameterWithRequired("target", "Target datastore, default candidate.", false),
		},
		command: buildConfigDiffCommand,
	},
}

func init() {
	for _, spec := range diagSpecs {
		spec := spec
		if !tools.Register(spec.kind, func(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
			return newConfig(ctx, name, decoder, spec)
		}) {
			panic(fmt.Sprintf("tool kind %q already registered", spec.kind))
		}
	}
}

type Config struct {
	Name         string                 `yaml:"name" validate:"required"`
	Type         string                 `yaml:"type" validate:"required"`
	Source       string                 `yaml:"source"`
	Description  string                 `yaml:"description"`
	AuthRequired []string               `yaml:"authRequired"`
	Annotations  *tools.ToolAnnotations `yaml:"annotations"`
	spec         diagSpec
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder, spec diagSpec) (tools.ToolConfig, error) {
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
	if cfg.Source == "" {
		return nil, fmt.Errorf("tool %q must specify source", cfg.Name)
	}
	if rawS, ok := srcs[cfg.Source]; ok {
		if _, ok := rawS.(capabilities.CommandRunner); !ok {
			return nil, fmt.Errorf("source %q does not support CLI command execution", cfg.Source)
		}
	}
	desc := cfg.Description
	if desc == "" {
		desc = cfg.spec.description
	}
	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	return Tool{
		Config:      cfg,
		executor:    query.NewExecutor(),
		manifest:    tools.Manifest{Description: desc, Parameters: cfg.spec.params.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, cfg.spec.params, annotations),
		Parameters:  cfg.spec.params,
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
	rawSource, ok := resourceMgr.GetSource(t.Source)
	if !ok {
		return nil, util.NewClientServerError("unable to retrieve source", 500, fmt.Errorf("source %q not found", t.Source))
	}
	identity, ok := rawSource.(capabilities.SourceIdentity)
	if !ok {
		return nil, util.NewClientServerError("source does not expose device identity", 500, fmt.Errorf("source %q missing identity", t.Source))
	}
	command, recordType, err := t.spec.command(identity, params)
	if err != nil {
		return nil, util.NewClientServerError("invalid diagnostic parameters", 400, err)
	}
	record, err := t.executor.ExecuteCommand(ctx, rawSource, command, t.Source)
	if err != nil {
		return nil, util.NewClientServerError("diagnostic command failed", 500, err)
	}
	record.RecordType = recordType
	return record, nil
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

func buildPingCommand(identity capabilities.SourceIdentity, params parameters.ParamValues) (string, string, error) {
	target, err := requiredSafe(params, "target")
	if err != nil {
		return "", "", err
	}
	count := intParam(params, "count", 5, 1, 100)
	cmd := fmt.Sprintf("ping %s count %d", target, count)
	vrf, err := optionalSafe(params, "vrf")
	if err != nil {
		return "", "", err
	}
	if vrf != "" {
		cmd += " network-instance " + vrf
	}
	source, err := optionalSafe(params, "source")
	if err != nil {
		return "", "", err
	}
	if source != "" {
		cmd += " source " + source
	}
	return cmd, "run_ping", nil
}

func buildTracerouteCommand(identity capabilities.SourceIdentity, params parameters.ParamValues) (string, string, error) {
	target, err := requiredSafe(params, "target")
	if err != nil {
		return "", "", err
	}
	maxHops := intParam(params, "max_hops", 30, 1, 255)
	cmd := fmt.Sprintf("traceroute %s max-hops %d", target, maxHops)
	vrf, err := optionalSafe(params, "vrf")
	if err != nil {
		return "", "", err
	}
	if vrf != "" {
		cmd += " network-instance " + vrf
	}
	source, err := optionalSafe(params, "source")
	if err != nil {
		return "", "", err
	}
	if source != "" {
		cmd += " source " + source
	}
	return cmd, "run_traceroute", nil
}

func buildConfigDiffCommand(identity capabilities.SourceIdentity, params parameters.ParamValues) (string, string, error) {
	source, err := optionalSafe(params, "source")
	if err != nil {
		return "", "", err
	}
	if source == "" {
		source = "running"
	}
	target, err := optionalSafe(params, "target")
	if err != nil {
		return "", "", err
	}
	if target == "" {
		target = "candidate"
	}
	if !allowedDatastore(source) || !allowedDatastore(target) {
		return "", "", fmt.Errorf("source and target must be running, candidate, or startup")
	}
	return fmt.Sprintf("show configuration diff %s %s", source, target), "get_configuration_diff", nil
}

func requiredSafe(params parameters.ParamValues, name string) (string, error) {
	value, err := optionalSafe(params, name)
	if err != nil {
		return "", err
	}
	if value == "" {
		return "", fmt.Errorf("missing required parameter %q", name)
	}
	return value, nil
}

func optionalSafe(params parameters.ParamValues, name string) (string, error) {
	for _, p := range params {
		if p.Name != name || p.Value == nil {
			continue
		}
		value := fmt.Sprint(p.Value)
		if diagSafeRe.MatchString(value) {
			return value, nil
		}
		return "", fmt.Errorf("parameter %q contains unsafe characters", name)
	}
	return "", nil
}

func intParam(params parameters.ParamValues, name string, def, min, max int) int {
	value := def
	for _, p := range params {
		if p.Name == name && p.Value != nil {
			_, _ = fmt.Sscan(fmt.Sprint(p.Value), &value)
		}
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func allowedDatastore(value string) bool {
	switch strings.ToLower(value) {
	case "running", "candidate", "startup":
		return true
	default:
		return false
	}
}

var _ = models.PingResult{}
var _ = models.TracerouteResult{}
var _ = models.ConfigDiff{}
