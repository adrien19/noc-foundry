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

// Package devicegroups provides fleet-scale device management for network
// sources. Device groups define sets of devices with shared templates and
// labels, enabling tools to target multiple devices via label selectors
// rather than individual source references.
package devicegroups

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// CacheConfig defines the TTL and refresh policy for a device group's
// inventory snapshot. All fields are optional; defaults favour availability
// over strict freshness (fail-open, stale-while-revalidate, no startup warm).
type CacheConfig struct {
	// TTL is the maximum age of the inventory snapshot before it is considered
	// stale. Accepts Go duration strings: "5m", "30s", "1h". Default: "5m".
	TTL string `yaml:"ttl,omitempty"`
	// RefreshMode controls how a stale snapshot is handled:
	//   "stale_while_revalidate" — return stale data while refreshing in the
	//                              background (default).
	//   "blocking"               — refresh synchronously before returning.
	//   "manual"                 — never auto-refresh; only refresh on demand.
	RefreshMode string `yaml:"refreshMode,omitempty"`
	// StartupWarm, if true, pre-warms the snapshot at startup so the first
	// request does not need to wait for discovery. Default: false.
	StartupWarm *bool `yaml:"startupWarm,omitempty"`
	// FailOpen, if true, serves a stale snapshot when a refresh fails instead
	// of returning an error. Default: true.
	FailOpen *bool `yaml:"failOpen,omitempty"`
}

// effectiveTTL returns the configured TTL or the 5-minute default.
func (c *CacheConfig) effectiveTTL() time.Duration {
	if c == nil || c.TTL == "" {
		return 5 * time.Minute
	}
	d, err := time.ParseDuration(c.TTL)
	if err != nil {
		return 5 * time.Minute
	}
	return d
}

// effectiveRefreshMode returns the configured refresh mode or "stale_while_revalidate".
func (c *CacheConfig) effectiveRefreshMode() string {
	if c == nil || c.RefreshMode == "" {
		return "stale_while_revalidate"
	}
	return c.RefreshMode
}

// effectiveStartupWarm returns whether startup pre-warming is enabled (default: false).
func (c *CacheConfig) effectiveStartupWarm() bool {
	if c == nil || c.StartupWarm == nil {
		return false
	}
	return *c.StartupWarm
}

// effectiveFailOpen returns whether to serve stale data on refresh failure (default: true).
func (c *CacheConfig) effectiveFailOpen() bool {
	if c == nil || c.FailOpen == nil {
		return true
	}
	return *c.FailOpen
}

// Config represents a device group configuration parsed from YAML.
// A device group defines a set of devices that share common source
// templates (SSH, gNMI, etc.) and can be targeted by label selectors.
//
// Devices can come from a static list (devices:), inventory providers
// (inventoryProviders:), or both. At least one source of devices is required.
type Config struct {
	Name               string                    `yaml:"name" validate:"required"`
	Type               string                    `yaml:"type,omitempty"`
	SourceTemplates    map[string]map[string]any `yaml:"sourceTemplates" validate:"required"`
	Devices            []DeviceEntry             `yaml:"devices,omitempty"`
	InventoryProviders []map[string]any          `yaml:"inventoryProviders,omitempty"`
	// Cache defines the TTL and refresh policy for this group's inventory
	// snapshot.  If omitted, sensible defaults are used (5m TTL,
	// stale-while-revalidate, fail-open, no startup warm).
	Cache *CacheConfig `yaml:"cache,omitempty"`

	// resolvedDevices holds the merged devices after ResolveDevices is called.
	resolvedDevices []DeviceEntry
	resolved        bool
}

// DeviceEntry represents a single device in a device group.
type DeviceEntry struct {
	Name             string            `yaml:"name" validate:"required"`
	Host             string            `yaml:"host" validate:"required"`
	Port             *int              `yaml:"port,omitempty"`
	Username         string            `yaml:"username,omitempty"`
	Password         string            `yaml:"password,omitempty"`
	SSHKeyPath       string            `yaml:"ssh_key_path,omitempty"`
	SSHKeyData       string            `yaml:"ssh_key_data,omitempty"`
	SSHKeyPassphrase string            `yaml:"ssh_key_passphrase,omitempty"`
	Labels           map[string]string `yaml:"labels"`
}

// SourceName returns the fully qualified source name for a device and template.
// Format: "groupName/deviceName/templateName"
func SourceName(groupName, deviceName, templateName string) string {
	return fmt.Sprintf("%s/%s/%s", groupName, deviceName, templateName)
}

// resolveDevices discovers all devices for a group config (static + providers).
// It is a pure function that does NOT mutate cfg; callers receive a new slice.
func resolveDevices(ctx context.Context, cfg *Config) ([]DeviceEntry, error) {
	seen := make(map[string]bool, len(cfg.Devices))
	var merged []DeviceEntry

	for _, d := range cfg.Devices {
		if seen[d.Name] {
			return nil, fmt.Errorf("duplicate device name %q in static list of group %q", d.Name, cfg.Name)
		}
		seen[d.Name] = true
		merged = append(merged, d)
	}

	for i, raw := range cfg.InventoryProviders {
		provCfg, err := decodeProviderFromMap(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("device group %q, inventoryProviders[%d]: %w", cfg.Name, i, err)
		}
		provider, err := provCfg.Initialize(ctx)
		if err != nil {
			return nil, fmt.Errorf("device group %q, inventoryProviders[%d] (%s): initialization failed: %w", cfg.Name, i, provCfg.ProviderConfigType(), err)
		}
		devices, err := provider.Discover(ctx)
		if err != nil {
			return nil, fmt.Errorf("device group %q, inventoryProviders[%d] (%s): discovery failed: %w", cfg.Name, i, provider.ProviderType(), err)
		}
		for _, d := range devices {
			if seen[d.Name] {
				return nil, fmt.Errorf("duplicate device name %q in group %q (from provider %s)", d.Name, cfg.Name, provider.ProviderType())
			}
			seen[d.Name] = true
			merged = append(merged, d)
		}
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("device group %q has no devices (no static devices and no inventory providers produced any)", cfg.Name)
	}
	return merged, nil
}

// ResolveDevices initializes inventory providers, discovers devices, and
// merges them with any statically defined devices. After this call,
// AllDevices() returns the complete device list.
// Duplicate device names (across static list and providers) are an error.
func (c *Config) ResolveDevices(ctx context.Context) error {
	if c.resolved {
		return nil
	}
	merged, err := resolveDevices(ctx, c)
	if err != nil {
		return err
	}
	c.resolvedDevices = merged
	c.resolved = true
	return nil
}

// AllDevices returns the complete list of devices. If ResolveDevices has been
// called, returns the merged list. Otherwise returns the static Devices list.
func (c *Config) AllDevices() []DeviceEntry {
	if c.resolved {
		return c.resolvedDevices
	}
	return c.Devices
}

// ExpandToSourceMaps generates source config maps for each device × template
// combination. Each map is ready to be passed to UnmarshalYAMLSourceConfig
// for creating proper SourceConfig instances through the source registry.
func (c *Config) ExpandToSourceMaps() (map[string]map[string]any, error) {
	devices := c.AllDevices()
	if len(devices) == 0 {
		return nil, fmt.Errorf("device group %q has no devices", c.Name)
	}
	if len(c.SourceTemplates) == 0 {
		return nil, fmt.Errorf("device group %q has no source templates", c.Name)
	}

	result := make(map[string]map[string]any)
	for templateName, template := range c.SourceTemplates {
		if _, ok := template["type"]; !ok {
			return nil, fmt.Errorf("source template %q in group %q missing 'type' field", templateName, c.Name)
		}

		for _, device := range devices {
			if device.Name == "" {
				return nil, fmt.Errorf("device in group %q missing 'name' field", c.Name)
			}
			if device.Host == "" {
				return nil, fmt.Errorf("device %q in group %q missing 'host' field", device.Name, c.Name)
			}

			sourceName := SourceName(c.Name, device.Name, templateName)

			// Copy template fields into a new map
			sourceMap := make(map[string]any, len(template)+2)
			for k, v := range template {
				sourceMap[k] = v
			}

			// Set device-specific values
			sourceMap["name"] = sourceName
			sourceMap["host"] = device.Host

			// Apply per-device overrides if specified
			if device.Port != nil {
				sourceMap["port"] = *device.Port
			}
			if device.Username != "" {
				sourceMap["username"] = device.Username
			}
			if device.Password != "" {
				sourceMap["password"] = device.Password
			}
			if device.SSHKeyPath != "" {
				sourceMap["ssh_key_path"] = device.SSHKeyPath
			}
			if device.SSHKeyData != "" {
				sourceMap["ssh_key_data"] = device.SSHKeyData
			}
			if device.SSHKeyPassphrase != "" {
				sourceMap["ssh_key_passphrase"] = device.SSHKeyPassphrase
			}

			result[sourceName] = sourceMap
		}
	}
	return result, nil
}

// BuildLabelIndex returns labels for all expanded source names.
// Each source inherits the device's labels plus automatic metadata labels
// (group, device, template, type, vendor, platform).
func (c *Config) BuildLabelIndex() map[string]map[string]string {
	devices := c.AllDevices()
	labels := make(map[string]map[string]string)
	for templateName, template := range c.SourceTemplates {
		sourceType, _ := template["type"].(string)
		vendor, _ := template["vendor"].(string)
		platform, _ := template["platform"].(string)

		for _, device := range devices {
			sourceName := SourceName(c.Name, device.Name, templateName)
			deviceLabels := map[string]string{
				"group":    c.Name,
				"device":   device.Name,
				"template": templateName,
			}
			if sourceType != "" {
				deviceLabels["type"] = sourceType
			}
			if vendor != "" {
				deviceLabels["vendor"] = vendor
			}
			if platform != "" {
				deviceLabels["platform"] = platform
			}
			for k, v := range device.Labels {
				deviceLabels[k] = v
			}
			labels[sourceName] = deviceLabels
		}
	}
	return labels
}

// LabelSelector defines criteria for selecting sources by label matching.
// All specified labels must match (AND semantics). An empty selector
// matches everything.
type LabelSelector struct {
	MatchLabels map[string]string `yaml:"matchLabels" json:"matchLabels"`
}

// Matches returns true if the given labels satisfy this selector.
func (s LabelSelector) Matches(labels map[string]string) bool {
	for k, v := range s.MatchLabels {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// SelectSources returns source names whose labels match the selector.
// Results are sorted for deterministic ordering.
func SelectSources(selector LabelSelector, labelIndex map[string]map[string]string) []string {
	var result []string
	for name, labels := range labelIndex {
		if selector.Matches(labels) {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
