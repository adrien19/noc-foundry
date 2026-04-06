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
	"testing"

	"github.com/goccy/go-yaml"
)

func TestRegisterProvider(t *testing.T) {
	// Since the global registry may already have entries, test with a unique type
	testType := "test-register-provider-" + t.Name()
	factory := func(ctx context.Context, decoder *yaml.Decoder) (InventoryProviderConfig, error) {
		return nil, nil
	}

	// First registration should succeed
	if !RegisterProvider(testType, factory) {
		t.Fatal("RegisterProvider() returned false for new type")
	}

	// Duplicate registration should fail
	if RegisterProvider(testType, factory) {
		t.Fatal("RegisterProvider() returned true for duplicate type")
	}
}

func TestDecodeProviderConfigUnknownType(t *testing.T) {
	_, err := DecodeProviderConfig(context.Background(), "nonexistent-provider-type", nil)
	if err == nil {
		t.Fatal("expected error for unknown provider type, got nil")
	}
}

func TestDecodeProviderFromMapMissingType(t *testing.T) {
	_, err := decodeProviderFromMap(context.Background(), map[string]any{
		"url": "https://example.com",
	})
	if err == nil {
		t.Fatal("expected error for missing type, got nil")
	}
}

func TestDecodeProviderFromMapInvalidType(t *testing.T) {
	_, err := decodeProviderFromMap(context.Background(), map[string]any{
		"type": 42, // not a string
	})
	if err == nil {
		t.Fatal("expected error for non-string type, got nil")
	}
}
