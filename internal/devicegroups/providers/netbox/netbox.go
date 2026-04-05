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

// Package netbox implements an inventory provider that discovers devices
// from a NetBox DCIM instance. Devices are queried via the NetBox REST API
// and mapped to DeviceEntry structs with labels derived from NetBox metadata
// (site, role, platform, tenant, tags).
package netbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/devicegroups"
	"github.com/goccy/go-yaml"
)

const ProviderType = "netbox"

func init() {
	devicegroups.RegisterProvider(ProviderType, func(ctx context.Context, decoder *yaml.Decoder) (devicegroups.InventoryProviderConfig, error) {
		var cfg Config
		if err := decoder.DecodeContext(ctx, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	})
}

// Config is the YAML configuration for a NetBox inventory provider.
type Config struct {
	Type    string            `yaml:"type"`
	URL     string            `yaml:"url"`
	Token   string            `yaml:"token"`
	Filters map[string]string `yaml:"filters,omitempty"`
	Labels  map[string]string `yaml:"labels,omitempty"`
	Timeout int               `yaml:"timeout,omitempty"`
}

func (c *Config) ProviderConfigType() string { return ProviderType }

func (c *Config) Initialize(_ context.Context) (devicegroups.InventoryProvider, error) {
	if c.URL == "" {
		return nil, fmt.Errorf("netbox provider: 'url' is required")
	}
	if c.Token == "" {
		return nil, fmt.Errorf("netbox provider: 'token' is required")
	}

	// Validate URL scheme
	parsed, err := url.Parse(c.URL)
	if err != nil {
		return nil, fmt.Errorf("netbox provider: invalid url: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("netbox provider: url scheme must be http or https, got %q", parsed.Scheme)
	}

	timeout := 30
	if c.Timeout > 0 {
		timeout = c.Timeout
	}
	return &Provider{
		baseURL: strings.TrimRight(c.URL, "/"),
		token:   c.Token,
		filters: c.Filters,
		labels:  c.Labels,
		client:  &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}, nil
}

// Provider queries the NetBox REST API to discover devices.
type Provider struct {
	baseURL string
	token   string
	filters map[string]string
	labels  map[string]string
	client  *http.Client
}

func (p *Provider) ProviderType() string { return ProviderType }

func (p *Provider) Discover(ctx context.Context) ([]devicegroups.DeviceEntry, error) {
	devices, err := p.fetchDevices(ctx)
	if err != nil {
		return nil, fmt.Errorf("netbox discovery: %w", err)
	}

	var entries []devicegroups.DeviceEntry
	for _, dev := range devices {
		entry, err := p.toDeviceEntry(dev)
		if err != nil {
			// Skip devices without required fields (no primary IP, etc.)
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// NetBox API response structures
type apiResponse struct {
	Count   int         `json:"count"`
	Next    *string     `json:"next"`
	Results []apiDevice `json:"results"`
}

type apiDevice struct {
	ID        int      `json:"id"`
	Name      string   `json:"name"`
	PrimaryIP *apiIP   `json:"primary_ip"`
	Platform  *apiRef  `json:"platform"`
	Role      *apiRef  `json:"device_role"`
	Site      *apiRef  `json:"site"`
	Tenant    *apiRef  `json:"tenant"`
	Tags      []apiTag `json:"tags"`
}

type apiIP struct {
	Address string `json:"address"`
}

type apiRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type apiTag struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

const maxPages = 100 // safety limit to prevent infinite pagination

func (p *Provider) fetchDevices(ctx context.Context) ([]apiDevice, error) {
	endpoint, err := url.JoinPath(p.baseURL, "/api/dcim/devices/")
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	params := url.Values{}
	params.Set("limit", "1000")
	params.Set("status", "active")
	for k, v := range p.filters {
		params.Set(k, v)
	}

	var allDevices []apiDevice
	requestURL := endpoint + "?" + params.Encode()

	for page := 0; requestURL != "" && page < maxPages; page++ {
		devices, nextURL, err := p.fetchPage(ctx, requestURL)
		if err != nil {
			return nil, err
		}
		allDevices = append(allDevices, devices...)
		requestURL = nextURL
	}

	return allDevices, nil
}

func (p *Provider) fetchPage(ctx context.Context, requestURL string) ([]apiDevice, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Token "+p.token)
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, "", fmt.Errorf("decode response: %w", err)
	}

	nextURL := ""
	if apiResp.Next != nil {
		nextURL = *apiResp.Next
	}
	return apiResp.Results, nextURL, nil
}

func (p *Provider) toDeviceEntry(dev apiDevice) (devicegroups.DeviceEntry, error) {
	if dev.Name == "" {
		return devicegroups.DeviceEntry{}, fmt.Errorf("device missing name")
	}

	host := ""
	if dev.PrimaryIP != nil {
		// Strip CIDR prefix length if present: "10.0.0.1/24" → "10.0.0.1"
		host = dev.PrimaryIP.Address
		if idx := strings.IndexByte(host, '/'); idx >= 0 {
			host = host[:idx]
		}
	}
	if host == "" {
		return devicegroups.DeviceEntry{}, fmt.Errorf("device %q has no primary IP", dev.Name)
	}

	labels := make(map[string]string)
	// Apply provider-level labels first
	for k, v := range p.labels {
		labels[k] = v
	}
	// Add NetBox metadata as labels
	labels["netbox_id"] = strconv.Itoa(dev.ID)
	if dev.Platform != nil && dev.Platform.Slug != "" {
		labels["platform"] = dev.Platform.Slug
	}
	if dev.Role != nil && dev.Role.Slug != "" {
		labels["role"] = dev.Role.Slug
	}
	if dev.Site != nil && dev.Site.Slug != "" {
		labels["site"] = dev.Site.Slug
	}
	if dev.Tenant != nil && dev.Tenant.Slug != "" {
		labels["tenant"] = dev.Tenant.Slug
	}
	for _, tag := range dev.Tags {
		if tag.Slug != "" {
			labels["tag/"+tag.Slug] = "true"
		}
	}

	return devicegroups.DeviceEntry{
		Name:   dev.Name,
		Host:   host,
		Labels: labels,
	}, nil
}
