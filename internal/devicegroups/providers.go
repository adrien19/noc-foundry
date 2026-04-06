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

package devicegroups

import (
	"context"
	"fmt"

	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/goccy/go-yaml"
)

// InventoryProvider discovers devices from an external inventory system.
type InventoryProvider interface {
	// Discover returns a list of devices from the inventory system.
	Discover(ctx context.Context) ([]DeviceEntry, error)
	// ProviderType returns the type identifier (e.g., "netbox").
	ProviderType() string
}

// InventoryProviderConfig is the configuration interface for inventory providers.
type InventoryProviderConfig interface {
	ProviderConfigType() string
	Initialize(ctx context.Context) (InventoryProvider, error)
}

// ProviderConfigFactory creates an InventoryProviderConfig from a YAML decoder.
type ProviderConfigFactory func(ctx context.Context, decoder *yaml.Decoder) (InventoryProviderConfig, error)

var providerRegistry = make(map[string]ProviderConfigFactory)

// RegisterProvider registers a new inventory provider type with its factory.
func RegisterProvider(providerType string, factory ProviderConfigFactory) bool {
	if _, exists := providerRegistry[providerType]; exists {
		return false
	}
	providerRegistry[providerType] = factory
	return true
}

// DecodeProviderConfig decodes a provider configuration using the registered factory.
func DecodeProviderConfig(ctx context.Context, providerType string, decoder *yaml.Decoder) (InventoryProviderConfig, error) {
	factory, found := providerRegistry[providerType]
	if !found {
		return nil, fmt.Errorf("unknown inventory provider type: %q", providerType)
	}
	return factory(ctx, decoder)
}

// decodeProviderFromMap decodes a raw provider config map into an InventoryProviderConfig.
func decodeProviderFromMap(ctx context.Context, raw map[string]any) (InventoryProviderConfig, error) {
	providerType, ok := raw["type"].(string)
	if !ok {
		return nil, fmt.Errorf("inventory provider missing 'type' field or it is not a string")
	}
	dec, err := util.NewStrictDecoder(raw)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder for inventory provider: %w", err)
	}
	cfg, err := DecodeProviderConfig(ctx, providerType, dec)
	if err != nil {
		return nil, fmt.Errorf("unable to parse inventory provider of type %q: %w", providerType, err)
	}
	return cfg, nil
}
