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

// Package listdevices implements the network-list-devices MCP tool,
// which returns all devices in the device pool along with their labels.
package listdevices

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	yaml "github.com/goccy/go-yaml"
)

const kind = "network-list-devices"

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

// Config holds the YAML-decoded configuration for the list-devices tool.
type Config struct {
	Name         string                 `yaml:"name" validate:"required"`
	Type         string                 `yaml:"type" validate:"required"`
	Description  string                 `yaml:"description"`
	AuthRequired []string               `yaml:"authRequired"`
	Annotations  *tools.ToolAnnotations `yaml:"annotations"`
}

var _ tools.ToolConfig = Config{}

func (cfg Config) ToolConfigType() string {
	return kind
}

// Initialize creates a new Tool instance.
func (cfg Config) Initialize(_ map[string]sources.Source) (tools.Tool, error) {
	desc := cfg.Description
	if desc == "" {
		desc = "List all network devices in the device pool with their labels. Use this to discover available devices before querying them."
	}

	allParameters := parameters.Parameters{
		parameters.NewStringParameterWithRequired("vendor", "Filter devices by vendor label (e.g. 'nokia', 'arista'). Omit for all.", false),
		parameters.NewStringParameterWithRequired("role", "Filter devices by role label (e.g. 'spine', 'leaf'). Omit for all.", false),
	}

	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	mcpManifest := tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, allParameters, annotations)

	return Tool{
		Config:      cfg,
		manifest:    tools.Manifest{Description: desc, Parameters: allParameters.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: mcpManifest,
		Parameters:  allParameters,
	}, nil
}

// Tool implements the network-list-devices operation.
type Tool struct {
	Config

	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	Parameters  parameters.Parameters
}

var _ tools.Tool = Tool{}

// DeviceInfo represents a single physical device in the response.
// Multiple source templates (e.g. gnmi, netconf, ssh) are merged into one
// entry; the available transports are listed in the Transports field.
type DeviceInfo struct {
	Name       string            `json:"name"`
	Group      string            `json:"group,omitempty"`
	Labels     map[string]string `json:"labels"`
	Transports []string          `json:"transports"`
}

// poolMetaLabels are injected automatically by the device pool and are not
// meaningful as user-defined device attributes. They are stripped from the
// Labels map returned to callers.
var poolMetaLabels = map[string]bool{
	"device":   true,
	"group":    true,
	"template": true,
	"type":     true,
}

// Invoke returns devices from the pool, optionally filtered by vendor/role.
// Sources that share the same physical device (i.e. multiple transport
// templates for one device) are merged into a single DeviceInfo entry.
func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	allLabels := resourceMgr.GetDevicePoolLabels()
	if allLabels == nil {
		return map[string]any{
			"devices": []DeviceInfo{},
			"total":   0,
		}, nil
	}

	vendorFilter := ""
	roleFilter := ""
	for _, p := range params {
		if p.Name == "vendor" && p.Value != nil {
			if s, ok := p.Value.(string); ok {
				vendorFilter = s
			}
		}
		if p.Name == "role" && p.Value != nil {
			if s, ok := p.Value.(string); ok {
				roleFilter = s
			}
		}
	}

	type deviceEntry struct {
		group      string
		name       string
		labels     map[string]string
		transports []string
	}

	merged := make(map[string]*deviceEntry)

	for sourceName, labels := range allLabels {
		if vendorFilter != "" {
			if v := labels["vendor"]; v != vendorFilter {
				continue
			}
		}
		if roleFilter != "" {
			if r := labels["role"]; r != roleFilter {
				continue
			}
		}

		deviceName, groupName := resolveDeviceIdentity(sourceName, labels)
		groupKey := deviceName
		if groupName != "" {
			groupKey = groupName + "/" + deviceName
		}

		entry, exists := merged[groupKey]
		if !exists {
			// Strip pool-internal metadata; expose only user-defined labels.
			userLabels := make(map[string]string, len(labels))
			for k, v := range labels {
				if !poolMetaLabels[k] {
					userLabels[k] = v
				}
			}
			entry = &deviceEntry{
				group:  groupName,
				name:   deviceName,
				labels: userLabels,
			}
			merged[groupKey] = entry
		}

		// Collect transport from the metadata label, or from the source name.
		transport := labels["template"]
		if transport == "" {
			transport = extractTransport(sourceName)
		}
		if transport != "" {
			entry.transports = append(entry.transports, transport)
		}
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	devices := make([]DeviceInfo, 0, len(merged))
	for _, key := range keys {
		entry := merged[key]
		sort.Strings(entry.transports)
		devices = append(devices, DeviceInfo{
			Name:       entry.name,
			Group:      entry.group,
			Labels:     entry.labels,
			Transports: entry.transports,
		})
	}

	return map[string]any{
		"devices": devices,
		"total":   len(devices),
	}, nil
}

// resolveDeviceIdentity returns the device name and group for a source.
// It prefers the metadata labels injected by the device pool; falls back to
// parsing the "group/device/template" source name format.
func resolveDeviceIdentity(sourceName string, labels map[string]string) (device, group string) {
	if d := labels["device"]; d != "" {
		return d, labels["group"]
	}
	parts := strings.SplitN(sourceName, "/", 3)
	if len(parts) == 3 {
		return parts[1], parts[0]
	}
	return sourceName, ""
}

// extractTransport returns the template segment from a "group/device/template"
// source name, or an empty string if the name has no path separator.
func extractTransport(sourceName string) string {
	idx := strings.LastIndex(sourceName, "/")
	if idx >= 0 {
		return sourceName[idx+1:]
	}
	return ""
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

func (t Tool) Authorized(verifiedAuthServices []string) bool {
	return tools.IsAuthorized(t.AuthRequired, verifiedAuthServices)
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
