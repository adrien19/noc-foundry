package showcoverage

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/profiles"
	"github.com/adrien19/noc-foundry/internal/network/schemas"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/tools/network/profilequery"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

const kind = "network-show-coverage"

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

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	if cfg.Source != "" && cfg.SourceSelector != nil {
		return nil, fmt.Errorf("tool %q cannot specify both 'source' and 'sourceSelector'", cfg.Name)
	}
	if cfg.Source != "" {
		if rawS, ok := srcs[cfg.Source]; ok {
			if _, ok := rawS.(capabilities.SourceIdentity); !ok {
				return nil, fmt.Errorf("source %q does not expose vendor/platform identity", cfg.Source)
			}
		}
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Show schema/profile operation coverage and readiness for registered network profiles."
	}
	params := parameters.Parameters{
		parameters.NewStringParameterWithRequired("vendor", "Optional vendor filter.", false),
		parameters.NewStringParameterWithRequired("platform", "Optional platform filter.", false),
	}
	if cfg.SourceSelector != nil {
		params = append(params, parameters.NewStringParameterWithRequired("device", "Optional device name to query within the source selector.", false))
	}
	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	return Tool{
		Config:      cfg,
		manifest:    tools.Manifest{Description: desc, Parameters: params.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, params, annotations),
		Parameters:  params,
	}, nil
}

type Tool struct {
	Config
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	Parameters  parameters.Parameters
}

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	if t.Source != "" {
		report, err := t.coverageForSource(resourceMgr, t.Source)
		if err != nil {
			return nil, util.NewClientServerError("failed to build coverage for source", http.StatusInternalServerError, err)
		}
		return map[string]any{"devices": []DeviceCoverage{report}, "total": 1}, nil
	}
	if t.SourceSelector != nil {
		return t.coverageForSelector(ctx, resourceMgr, params)
	}

	vendor := paramString(params, "vendor")
	platform := paramString(params, "platform")
	all := profiles.AllProfiles()
	keys := make([]string, 0, len(all))
	for key := range all {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	reports := make([]schemas.CoverageReport, 0, len(keys))
	for _, key := range keys {
		profile := all[key]
		if vendor != "" && profile.Vendor != vendor {
			continue
		}
		if platform != "" && profile.Platform != platform {
			continue
		}
		reports = append(reports, schemas.BuildCoverageReport(profile, nil))
	}
	return map[string]any{"profiles": reports, "total": len(reports)}, nil
}

type DeviceCoverage struct {
	Source string                 `json:"source"`
	Device string                 `json:"device"`
	Report schemas.CoverageReport `json:"report"`
}

func (t Tool) coverageForSelector(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues) (any, util.NOCFoundryError) {
	matchLabels := t.SourceSelector.MatchLabels
	if t.SourceSelector.Template != "" {
		merged := make(map[string]string, len(matchLabels)+1)
		for k, v := range matchLabels {
			merged[k] = v
		}
		merged["template"] = t.SourceSelector.Template
		matchLabels = merged
	}
	srcs, err := resourceMgr.GetSourcesByLabels(ctx, matchLabels)
	if err != nil {
		return nil, util.NewClientServerError("failed to resolve sources by selector", http.StatusInternalServerError, err)
	}
	deviceName := paramString(params, "device")
	sourceNames := make([]string, 0, len(srcs))
	for name := range srcs {
		if deviceName == "" || extractDevice(name) == deviceName {
			sourceNames = append(sourceNames, name)
		}
	}
	if len(sourceNames) == 0 {
		return nil, util.NewClientServerError("no sources matched coverage selector", http.StatusNotFound, fmt.Errorf("sourceSelector matched 0 devices"))
	}
	maxConc := fanout.DefaultMaxConcurrency
	if t.SourceSelector.MaxConcurrency > 0 {
		maxConc = t.SourceSelector.MaxConcurrency
	}
	result := fanout.Execute(ctx, sourceNames, maxConc, func(ctx context.Context, sourceName string) (any, error) {
		report, err := t.coverageForSource(resourceMgr, sourceName)
		if err != nil {
			return nil, err
		}
		return report, nil
	})
	return result, nil
}

func (t Tool) coverageForSource(resourceMgr tools.SourceProvider, sourceName string) (DeviceCoverage, error) {
	rawSource, ok := resourceMgr.GetSource(sourceName)
	if !ok {
		return DeviceCoverage{}, fmt.Errorf("source %q not found", sourceName)
	}
	identity, ok := rawSource.(capabilities.SourceIdentity)
	if !ok {
		return DeviceCoverage{}, fmt.Errorf("source %q does not expose vendor/platform identity", sourceName)
	}
	profile, ok := profiles.LookupForDevice(identity.DeviceVendor(), identity.DevicePlatform(), identity.DeviceVersion())
	if !ok {
		return DeviceCoverage{}, fmt.Errorf("no profile for %s/%s/%s", identity.DeviceVendor(), identity.DevicePlatform(), identity.DeviceVersion())
	}
	return DeviceCoverage{
		Source: sourceName,
		Device: extractDevice(sourceName),
		Report: schemas.BuildCoverageReport(profile, nil),
	}, nil
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

func paramString(params parameters.ParamValues, name string) string {
	for _, p := range params {
		if p.Name == name && p.Value != nil {
			if s, ok := p.Value.(string); ok {
				return s
			}
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
