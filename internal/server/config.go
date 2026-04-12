// Copyright 2024 Google LLC
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
package server

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/devicegroups"
	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/embeddingmodels/gemini"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	yaml "github.com/goccy/go-yaml"
)

type ServerConfig struct {
	// Server version
	Version string
	// Address is the address of the interface the server will listen on.
	Address string
	// Port is the port the server will listen on.
	Port int
	// SourceConfigs defines what sources of data are available for tools.
	SourceConfigs SourceConfigs
	// AuthServiceConfigs defines what sources of authentication are available for tools.
	AuthServiceConfigs AuthServiceConfigs
	// EmbeddingModelConfigs defines a models used to embed parameters.
	EmbeddingModelConfigs EmbeddingModelConfigs
	// ToolConfigs defines what tools are available.
	ToolConfigs ToolConfigs
	// ToolsetConfigs defines what tools are available.
	ToolsetConfigs ToolsetConfigs
	// PromptConfigs defines what prompts are available
	PromptConfigs PromptConfigs
	// PromptsetConfigs defines what prompts are available
	PromptsetConfigs PromptsetConfigs
	// DeviceGroupConfigs defines groups of devices with shared templates.
	DeviceGroupConfigs DeviceGroupConfigs
	// LoggingFormat defines whether structured loggings are used.
	LoggingFormat logFormat
	// LogLevel defines the levels to log.
	LogLevel StringLevel
	// TelemetryOTLP defines OTLP collector url for telemetry exports.
	TelemetryOTLP string
	// TelemetryServiceName defines the value of service.name resource attribute.
	TelemetryServiceName string
	// Stdio indicates if NOCFoundry is listening via MCP stdio.
	Stdio bool
	// DisableReload indicates if the user has disabled dynamic reloading for NOCFoundry.
	DisableReload bool
	// UI indicates if NOCFoundry UI endpoints (/ui) are available.
	UI bool
	// Specifies a list of origins permitted to access this server.
	AllowedOrigins []string
	// Specifies a list of hosts permitted to access this server.
	AllowedHosts []string
	// UserAgentMetadata specifies additional metadata to append to the User-Agent string.
	UserAgentMetadata []string
	// PollInterval sets the polling frequency for configuration file updates.
	PollInterval int
	// ValidationRuns configures long-running validation execution.
	ValidationRuns ValidationRunConfig
	// Auth configures server-scoped authentication policy for HTTP surfaces.
	Auth ServerAuthConfig
	// SchemaDir specifies an optional directory containing vendor YANG models.
	// Expected structure: <schema-dir>/<vendor>/<platform>/<version>/*.yang
	// When set, YANG models are compiled at startup and used to build
	// schema-derived profiles (overriding hardcoded defaults for gNMI/NETCONF paths).
	SchemaDir string
}

type ValidationRunConfig struct {
	ExecutionBackend      string        `yaml:"executionBackend,omitempty"`
	StoreBackend          string        `yaml:"storeBackend,omitempty"`
	SQLitePath            string        `yaml:"sqlitePath,omitempty"`
	DurableTaskSQLitePath string        `yaml:"durableTaskSQLitePath,omitempty"`
	MaxConcurrentRuns     int           `yaml:"maxConcurrentRuns,omitempty"`
	MaxConcurrentSteps    int           `yaml:"maxConcurrentSteps,omitempty"`
	ResultRetention       time.Duration `yaml:"resultRetention,omitempty"`
	EventRetention        time.Duration `yaml:"eventRetention,omitempty"`
}

type ServerAuthConfig struct {
	EndpointAuth ServerEndpointAuthConfig `yaml:"endpointAuth,omitempty"`
	UI           ServerUIAuthConfig       `yaml:"ui,omitempty"`
}

type ServerEndpointAuthConfig struct {
	API EndpointAuthPolicyConfig `yaml:"api,omitempty"`
	MCP EndpointAuthPolicyConfig `yaml:"mcp,omitempty"`
}

type EndpointAuthPolicyConfig struct {
	Enabled      bool     `yaml:"enabled,omitempty"`
	AuthServices []string `yaml:"authServices,omitempty"`
	Audience     string   `yaml:"audience,omitempty"`
}

type ServerUIAuthConfig struct {
	Enabled      bool     `yaml:"enabled,omitempty"`
	AuthService  string   `yaml:"authService,omitempty"`
	ClientID     string   `yaml:"clientId,omitempty"`
	Scopes       []string `yaml:"scopes,omitempty"`
	RedirectPath string   `yaml:"redirectPath,omitempty"`
}

func (c ServerAuthConfig) EndpointPolicy(surface auth.EndpointSurface) (EndpointAuthPolicyConfig, bool) {
	switch surface {
	case auth.EndpointSurfaceAPI:
		return c.EndpointAuth.API, true
	case auth.EndpointSurfaceMCP:
		return c.EndpointAuth.MCP, true
	default:
		return EndpointAuthPolicyConfig{}, false
	}
}

func ValidateValidationRunConfig(cfg ValidationRunConfig) error {
	executionBackend := cfg.ExecutionBackend
	if executionBackend == "" {
		executionBackend = "local"
	}
	switch executionBackend {
	case "local", "durabletask":
	default:
		return fmt.Errorf("validation executionBackend must be one of %q or %q", "local", "durabletask")
	}

	storeBackend := cfg.StoreBackend
	if storeBackend == "" {
		if cfg.SQLitePath != "" {
			storeBackend = "sqlite"
		} else {
			storeBackend = "memory"
		}
	}
	switch storeBackend {
	case "memory", "sqlite":
	default:
		return fmt.Errorf("validation storeBackend must be one of %q or %q", "memory", "sqlite")
	}

	if storeBackend == "sqlite" && cfg.SQLitePath == "" {
		return fmt.Errorf("validation sqlitePath is required when storeBackend=%q", "sqlite")
	}
	if executionBackend == "durabletask" {
		if storeBackend != "sqlite" {
			return fmt.Errorf("validation executionBackend=%q requires storeBackend=%q", "durabletask", "sqlite")
		}
		if cfg.DurableTaskSQLitePath == "" {
			return fmt.Errorf("validation durableTaskSQLitePath is required when executionBackend=%q", "durabletask")
		}
	}
	return nil
}

func ValidateServerAuthConfig(cfg ServerAuthConfig) error {
	for _, surface := range []auth.EndpointSurface{auth.EndpointSurfaceAPI, auth.EndpointSurfaceMCP} {
		policy, _ := cfg.EndpointPolicy(surface)
		if !policy.Enabled {
			continue
		}
		if strings.TrimSpace(policy.Audience) == "" {
			return fmt.Errorf("server auth endpointAuth.%s.audience is required when enabled", surface)
		}
		if len(policy.AuthServices) == 0 {
			return fmt.Errorf("server auth endpointAuth.%s.authServices is required when enabled", surface)
		}
		for _, name := range policy.AuthServices {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("server auth endpointAuth.%s.authServices cannot contain empty values", surface)
			}
		}
	}

	if cfg.UI.Enabled {
		if strings.TrimSpace(cfg.UI.AuthService) == "" {
			return fmt.Errorf("server auth ui.authService is required when enabled")
		}
		if strings.TrimSpace(cfg.UI.ClientID) == "" {
			return fmt.Errorf("server auth ui.clientId is required when enabled")
		}
		if strings.TrimSpace(cfg.UI.RedirectPath) == "" {
			return fmt.Errorf("server auth ui.redirectPath is required when enabled")
		}
		if !strings.HasPrefix(cfg.UI.RedirectPath, "/") {
			return fmt.Errorf("server auth ui.redirectPath must start with \"/\"")
		}
		apiPolicy := cfg.EndpointAuth.API
		if !apiPolicy.Enabled {
			return fmt.Errorf("server auth ui.enabled requires endpointAuth.api.enabled")
		}
		if !slices.Contains(apiPolicy.AuthServices, cfg.UI.AuthService) {
			return fmt.Errorf("server auth ui.authService %q must be included in endpointAuth.api.authServices", cfg.UI.AuthService)
		}
	}
	return nil
}

type logFormat string

// String is used by both fmt.Print and by Cobra in help text
func (f *logFormat) String() string {
	if string(*f) != "" {
		return strings.ToLower(string(*f))
	}
	return "standard"
}

// validate logging format flag
func (f *logFormat) Set(v string) error {
	switch strings.ToLower(v) {
	case "standard", "json":
		*f = logFormat(v)
		return nil
	default:
		return fmt.Errorf(`log format must be one of "standard", or "json"`)
	}
}

// Type is used in Cobra help text
func (f *logFormat) Type() string {
	return "logFormat"
}

type StringLevel string

// String is used by both fmt.Print and by Cobra in help text
func (s *StringLevel) String() string {
	if string(*s) != "" {
		return strings.ToLower(string(*s))
	}
	return "info"
}

// validate log level flag
func (s *StringLevel) Set(v string) error {
	switch strings.ToLower(v) {
	case "debug", "info", "warn", "error":
		*s = StringLevel(v)
		return nil
	default:
		return fmt.Errorf(`log level must be one of "debug", "info", "warn", or "error"`)
	}
}

// Type is used in Cobra help text
func (s *StringLevel) Type() string {
	return "stringLevel"
}

type SourceConfigs map[string]sources.SourceConfig
type AuthServiceConfigs map[string]auth.AuthServiceConfig
type EmbeddingModelConfigs map[string]embeddingmodels.EmbeddingModelConfig
type ToolConfigs map[string]tools.ToolConfig
type ToolsetConfigs map[string]tools.ToolsetConfig
type PromptConfigs map[string]prompts.PromptConfig
type PromptsetConfigs map[string]prompts.PromptsetConfig
type DeviceGroupConfigs []*devicegroups.Config

func UnmarshalResourceConfig(ctx context.Context, raw []byte) (SourceConfigs, AuthServiceConfigs, EmbeddingModelConfigs, ToolConfigs, ToolsetConfigs, PromptConfigs, PromptsetConfigs, DeviceGroupConfigs, error) {
	// prepare configs map
	var sourceConfigs SourceConfigs
	var authServiceConfigs AuthServiceConfigs
	var embeddingModelConfigs EmbeddingModelConfigs
	var toolConfigs ToolConfigs
	var toolsetConfigs ToolsetConfigs
	var promptConfigs PromptConfigs
	var promptsetConfigs PromptsetConfigs
	var deviceGroupConfigs DeviceGroupConfigs

	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	// for loop to unmarshal documents with the `---` separator
	for {
		var resource map[string]any
		if err := decoder.DecodeContext(ctx, &resource); err != nil {
			if err == io.EOF {
				break
			}
			return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("unable to decode YAML document: %w", err)
		}
		var kind, name string
		var ok bool
		if kind, ok = resource["kind"].(string); !ok {
			return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("missing 'kind' field or it is not a string: %v", resource)
		}
		if name, ok = resource["name"].(string); !ok {
			return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("missing 'name' field or it is not a string")
		}
		// remove 'kind' from map for strict unmarshaling
		delete(resource, "kind")

		switch kind {
		case "sources":
			c, err := UnmarshalYAMLSourceConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if sourceConfigs == nil {
				sourceConfigs = make(SourceConfigs)
			}
			sourceConfigs[name] = c
		case "authServices":
			c, err := UnmarshalYAMLAuthServiceConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if authServiceConfigs == nil {
				authServiceConfigs = make(AuthServiceConfigs)
			}
			authServiceConfigs[name] = c
		case "tools":
			c, err := UnmarshalYAMLToolConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if toolConfigs == nil {
				toolConfigs = make(ToolConfigs)
			}
			toolConfigs[name] = c
		case "toolsets":
			c, err := UnmarshalYAMLToolsetConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if toolsetConfigs == nil {
				toolsetConfigs = make(ToolsetConfigs)
			}
			toolsetConfigs[name] = c
		case "embeddingModels":
			c, err := UnmarshalYAMLEmbeddingModelConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if embeddingModelConfigs == nil {
				embeddingModelConfigs = make(EmbeddingModelConfigs)
			}
			embeddingModelConfigs[name] = c
		case "prompts":
			c, err := UnmarshalYAMLPromptConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if promptConfigs == nil {
				promptConfigs = make(PromptConfigs)
			}
			promptConfigs[name] = c
		case "promptsets":
			c, err := UnmarshalYAMLPromptsetConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			if promptsetConfigs == nil {
				promptsetConfigs = make(PromptsetConfigs)
			}
			promptsetConfigs[name] = c
		case "deviceGroups":
			c, err := UnmarshalYAMLDeviceGroupConfig(ctx, name, resource)
			if err != nil {
				return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("error unmarshaling %s: %s", kind, err)
			}
			deviceGroupConfigs = append(deviceGroupConfigs, c)
		default:
			return nil, nil, nil, nil, nil, nil, nil, nil, fmt.Errorf("invalid kind %s", kind)
		}
	}
	return sourceConfigs, authServiceConfigs, embeddingModelConfigs, toolConfigs, toolsetConfigs, promptConfigs, promptsetConfigs, deviceGroupConfigs, nil
}

func UnmarshalYAMLSourceConfig(ctx context.Context, name string, r map[string]any) (sources.SourceConfig, error) {
	resourceType, ok := r["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'type' field or it is not a string")
	}
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %w", err)
	}
	sourceConfig, err := sources.DecodeConfig(ctx, resourceType, name, dec)
	if err != nil {
		return nil, err
	}
	return sourceConfig, nil
}

func UnmarshalYAMLDeviceGroupConfig(ctx context.Context, name string, r map[string]any) (*devicegroups.Config, error) {
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %w", err)
	}
	cfg := &devicegroups.Config{Name: name}
	if err := dec.DecodeContext(ctx, cfg); err != nil {
		return nil, fmt.Errorf("unable to parse device group %q: %w", name, err)
	}
	return cfg, nil
}

func UnmarshalYAMLAuthServiceConfig(ctx context.Context, name string, r map[string]any) (auth.AuthServiceConfig, error) {
	resourceType, ok := r["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'type' field or it is not a string")
	}
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %s", err)
	}
	return auth.DecodeConfig(ctx, resourceType, name, dec)
}

func UnmarshalYAMLEmbeddingModelConfig(ctx context.Context, name string, r map[string]any) (embeddingmodels.EmbeddingModelConfig, error) {
	resourceType, ok := r["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'type' field or it is not a string")
	}
	if resourceType != gemini.EmbeddingModelType {
		return nil, fmt.Errorf("%s is not a valid type of embedding model", resourceType)
	}
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %s", err)
	}
	actual := gemini.Config{Name: name}
	if err := dec.DecodeContext(ctx, &actual); err != nil {
		return nil, fmt.Errorf("unable to parse as %q: %w", name, err)
	}
	return actual, nil
}

func UnmarshalYAMLToolConfig(ctx context.Context, name string, r map[string]any) (tools.ToolConfig, error) {
	resourceType, ok := r["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing 'type' field or it is not a string")
	}
	// `authRequired` and `useClientOAuth` cannot be specified together
	if r["authRequired"] != nil && r["useClientOAuth"] == true {
		return nil, fmt.Errorf("`authRequired` and `useClientOAuth` are mutually exclusive. Choose only one authentication method")
	}
	// Make `authRequired` an empty list instead of nil for Tool manifest
	if r["authRequired"] == nil {
		r["authRequired"] = []string{}
	}

	// validify parameter references
	if rawParams, ok := r["parameters"]; ok {
		if paramsList, ok := rawParams.([]any); ok {
			// Turn params into a map
			validParamNames := make(map[string]bool)
			for _, rawP := range paramsList {
				if pMap, ok := rawP.(map[string]any); ok {
					if pName, ok := pMap["name"].(string); ok && pName != "" {
						validParamNames[pName] = true
					}
				}
			}

			// Validate references
			for i, rawP := range paramsList {
				pMap, ok := rawP.(map[string]any)
				if !ok {
					continue
				}

				pName, _ := pMap["name"].(string)
				refName, _ := pMap["valueFromParam"].(string)

				if refName != "" {
					// Check if the referenced parameter exists
					if !validParamNames[refName] {
						return nil, fmt.Errorf("tool %q config error: parameter %q (index %d) references '%q' in the 'valueFromParam' field, which is not a defined parameter", name, pName, i, refName)
					}

					// Check for self-reference
					if refName == pName {
						return nil, fmt.Errorf("tool %q config error: parameter %q cannot copy value from itself", name, pName)
					}
				}
			}
		}
	}

	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %s", err)
	}
	toolCfg, err := tools.DecodeConfig(ctx, resourceType, name, dec)
	if err != nil {
		return nil, err
	}
	return toolCfg, nil
}

func UnmarshalYAMLToolsetConfig(ctx context.Context, name string, r map[string]any) (tools.ToolsetConfig, error) {
	var toolsetConfig tools.ToolsetConfig
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return toolsetConfig, fmt.Errorf("error creating decoder: %s", err)
	}
	toolsetConfig = tools.ToolsetConfig{Name: name}
	if err := dec.DecodeContext(ctx, &toolsetConfig); err != nil {
		return toolsetConfig, fmt.Errorf("unable to unmarshal toolset: %s", err)
	}
	return toolsetConfig, nil
}

func UnmarshalYAMLPromptsetConfig(ctx context.Context, name string, r map[string]any) (prompts.PromptsetConfig, error) {
	var promptsetConfig prompts.PromptsetConfig
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return promptsetConfig, fmt.Errorf("error creating decoder: %s", err)
	}
	promptsetConfig = prompts.PromptsetConfig{Name: name}
	if err := dec.DecodeContext(ctx, &promptsetConfig); err != nil {
		return promptsetConfig, fmt.Errorf("unable to unmarshal promptset: %s", err)
	}
	return promptsetConfig, nil
}

func UnmarshalYAMLPromptConfig(ctx context.Context, name string, r map[string]any) (prompts.PromptConfig, error) {
	// Look for the 'type' field. If it's not present, typeStr will be an
	// empty string, which prompts.DecodeConfig will correctly default to "custom".
	var resourceType string
	if typeVal, ok := r["type"]; ok {
		var isString bool
		resourceType, isString = typeVal.(string)
		if !isString {
			return nil, fmt.Errorf("invalid 'type' field for prompt %q (must be a string)", name)
		}
	}
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %s", err)
	}

	// Use the central registry to decode the prompt based on its type.
	promptCfg, err := prompts.DecodeConfig(ctx, resourceType, name, dec)
	if err != nil {
		return nil, err
	}
	return promptCfg, nil
}

// Tools naming validation is added in the MCP v2025-11-25, but we'll be
// implementing it across NOCFoundry
// Tool names SHOULD be between 1 and 128 characters in length (inclusive).
// Tool names SHOULD be considered case-sensitive.
// The following SHOULD be the only allowed characters: uppercase and lowercase ASCII letters (A-Z, a-z), digits (0-9), underscore (_), hyphen (-), and dot (.)
// Tool names SHOULD NOT contain spaces, commas, or other special characters.
// Tool names SHOULD be unique within a server.
func NameValidation(name string) error {
	strLen := len(name)
	if strLen < 1 || strLen > 128 {
		return fmt.Errorf("resource name SHOULD be between 1 and 128 characters in length (inclusive)")
	}
	validChars := regexp.MustCompile("^[a-zA-Z0-9_.-]+$")
	isValid := validChars.MatchString(name)
	if !isValid {
		return fmt.Errorf("invalid character for resource name; only uppercase and lowercase ASCII letters (A-Z, a-z), digits (0-9), underscore (_), hyphen (-), and dot (.) is allowed")
	}
	return nil
}
