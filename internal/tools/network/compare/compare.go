package compare

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/tools/network/profilequery"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

const kind = "network-compare"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

type Config struct {
	Name           string                       `yaml:"name" validate:"required"`
	Type           string                       `yaml:"type" validate:"required"`
	SourceSelector *profilequery.SourceSelector `yaml:"sourceSelector" validate:"required"`
	Description    string                       `yaml:"description"`
	AuthRequired   []string                     `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations       `yaml:"annotations"`
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name, Type: kind}
	if decoder != nil {
		if err := decoder.DecodeContext(ctx, &actual); err != nil {
			return nil, err
		}
	}
	return actual, nil
}

func (cfg Config) ToolConfigType() string { return kind }

func (cfg Config) Initialize(_ map[string]sources.Source) (tools.Tool, error) {
	if cfg.SourceSelector == nil {
		return nil, fmt.Errorf("tool %q must specify sourceSelector", cfg.Name)
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Compare one profile-routed network operation across selected devices."
	}
	params := parameters.Parameters{
		parameters.NewStringParameter("operation", "Operation ID to compare, such as get_interfaces or get_route_table."),
		parameters.NewStringParameterWithRequired("devices", "Optional comma-separated device names to compare within the selector.", false),
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
	operation := paramString(params, "operation")
	if operation == "" {
		return nil, util.NewClientServerError("missing operation", http.StatusBadRequest, fmt.Errorf("operation is required"))
	}
	srcs, err := resourceMgr.GetSourcesByLabels(ctx, selectorLabels(t.SourceSelector))
	if err != nil {
		return nil, util.NewClientServerError("failed to resolve comparison sources", http.StatusInternalServerError, err)
	}
	selectedDevices := deviceSet(paramString(params, "devices"))
	sourceNames := make([]string, 0, len(srcs))
	for name := range srcs {
		if len(selectedDevices) == 0 || selectedDevices[extractDevice(name)] {
			sourceNames = append(sourceNames, name)
		}
	}
	if len(sourceNames) == 0 {
		return nil, util.NewClientServerError("no sources matched comparison selector", http.StatusNotFound, fmt.Errorf("sourceSelector matched 0 devices"))
	}
	maxConc := fanout.DefaultMaxConcurrency
	if t.SourceSelector.MaxConcurrency > 0 {
		maxConc = t.SourceSelector.MaxConcurrency
	}
	opts := query.ExecuteOptions{Params: profilequery.ParamsToMap(params)}
	result := fanout.Execute(ctx, sourceNames, maxConc, func(ctx context.Context, sourceName string) (any, error) {
		rawSource, ok := resourceMgr.GetSource(sourceName)
		if !ok {
			return nil, fmt.Errorf("source %q not found", sourceName)
		}
		return t.executor.ExecuteWithOptions(ctx, rawSource, operation, sourceName, opts)
	})
	return buildComparison(operation, result), nil
}

func buildComparison(operation string, result fanout.Result) models.ComparisonResult {
	comparison := models.ComparisonResult{Operation: operation}
	payloads := map[string]any{}
	fingerprints := map[string]string{}
	for _, deviceResult := range result.Results {
		device := models.ComparisonDevice{Name: deviceResult.Device}
		if deviceResult.Status != "success" {
			device.Error = deviceResult.Error
			comparison.Devices = append(comparison.Devices, device)
			continue
		}
		record, ok := deviceResult.Data.(*models.Record)
		if !ok {
			device.Error = "result was not a canonical record"
			comparison.Devices = append(comparison.Devices, device)
			continue
		}
		device.Data = record.Payload
		comparison.Devices = append(comparison.Devices, device)
		payloads[device.Name] = record.Payload
		raw, _ := json.Marshal(record.Payload)
		fingerprints[string(raw)] = device.Name
	}
	if len(payloads) > 1 && len(fingerprints) > 1 {
		comparison.Differences = append(comparison.Differences, models.ComparisonDiff{
			Field:  "payload",
			Values: payloads,
		})
	}
	return comparison
}

func selectorLabels(selector *profilequery.SourceSelector) map[string]string {
	matchLabels := selector.MatchLabels
	if selector.Template == "" {
		return matchLabels
	}
	merged := make(map[string]string, len(matchLabels)+1)
	for k, v := range matchLabels {
		merged[k] = v
	}
	merged["template"] = selector.Template
	return merged
}

func deviceSet(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func paramString(params parameters.ParamValues, name string) string {
	for _, p := range params {
		if p.Name == name && p.Value != nil {
			return fmt.Sprint(p.Value)
		}
	}
	return ""
}

func extractDevice(sourceName string) string {
	parts := strings.Split(sourceName, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return sourceName
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
