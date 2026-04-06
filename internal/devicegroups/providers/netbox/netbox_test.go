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

package netbox

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adrien19/noc-foundry/internal/devicegroups"
	"github.com/goccy/go-yaml"
)

func TestProviderType(t *testing.T) {
	p := &Provider{}
	if got := p.ProviderType(); got != ProviderType {
		t.Errorf("ProviderType() = %q, want %q", got, ProviderType)
	}
}

func TestConfigProviderConfigType(t *testing.T) {
	c := &Config{}
	if got := c.ProviderConfigType(); got != ProviderType {
		t.Errorf("ProviderConfigType() = %q, want %q", got, ProviderType)
	}
}

func TestConfigInitializeValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "missing url",
			cfg:     Config{Token: "abc"},
			wantErr: "'url' is required",
		},
		{
			name:    "missing token",
			cfg:     Config{URL: "https://nb.example.com"},
			wantErr: "'token' is required",
		},
		{
			name:    "invalid scheme",
			cfg:     Config{URL: "ftp://nb.example.com", Token: "abc"},
			wantErr: "url scheme must be http or https",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.cfg.Initialize(context.Background())
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if got := err.Error(); !contains(got, tc.wantErr) {
				t.Errorf("error = %q, want to contain %q", got, tc.wantErr)
			}
		})
	}
}

func TestConfigInitializeSuccess(t *testing.T) {
	cfg := Config{
		URL:     "https://nb.example.com",
		Token:   "test-token",
		Timeout: 10,
	}
	provider, err := cfg.Initialize(context.Background())
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
	if provider.ProviderType() != ProviderType {
		t.Errorf("ProviderType() = %q, want %q", provider.ProviderType(), ProviderType)
	}
}

func TestDiscoverBasic(t *testing.T) {
	// Create a mock NetBox API server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/api/dcim/devices/" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		resp := apiResponse{
			Count: 2,
			Next:  nil,
			Results: []apiDevice{
				{
					ID:        1,
					Name:      "spine-1",
					PrimaryIP: &apiIP{Address: "10.0.0.1/24"},
					Platform:  &apiRef{Name: "SR Linux", Slug: "srlinux"},
					Role:      &apiRef{Name: "Spine", Slug: "spine"},
					Site:      &apiRef{Name: "DC1", Slug: "dc1"},
					Tags:      []apiTag{{Name: "Production", Slug: "production"}},
				},
				{
					ID:        2,
					Name:      "leaf-1",
					PrimaryIP: &apiIP{Address: "10.0.0.2/24"},
					Platform:  &apiRef{Name: "SR Linux", Slug: "srlinux"},
					Role:      &apiRef{Name: "Leaf", Slug: "leaf"},
					Site:      &apiRef{Name: "DC1", Slug: "dc1"},
					Tenant:    &apiRef{Name: "Acme", Slug: "acme"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := &Provider{
		baseURL: srv.URL,
		token:   "test-token",
		client:  srv.Client(),
		labels:  map[string]string{"env": "test"},
	}

	entries, err := provider.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Discover() returned %d entries, want 2", len(entries))
	}

	// Verify spine-1
	spine := entries[0]
	if spine.Name != "spine-1" {
		t.Errorf("entries[0].Name = %q, want %q", spine.Name, "spine-1")
	}
	if spine.Host != "10.0.0.1" {
		t.Errorf("entries[0].Host = %q, want %q", spine.Host, "10.0.0.1")
	}
	if spine.Labels["platform"] != "srlinux" {
		t.Errorf("entries[0].Labels[platform] = %q, want %q", spine.Labels["platform"], "srlinux")
	}
	if spine.Labels["role"] != "spine" {
		t.Errorf("entries[0].Labels[role] = %q, want %q", spine.Labels["role"], "spine")
	}
	if spine.Labels["site"] != "dc1" {
		t.Errorf("entries[0].Labels[site] = %q, want %q", spine.Labels["site"], "dc1")
	}
	if spine.Labels["env"] != "test" {
		t.Errorf("entries[0].Labels[env] = %q, want %q", spine.Labels["env"], "test")
	}
	if spine.Labels["tag/production"] != "true" {
		t.Errorf("entries[0].Labels[tag/production] = %q, want %q", spine.Labels["tag/production"], "true")
	}
	if spine.Labels["netbox_id"] != "1" {
		t.Errorf("entries[0].Labels[netbox_id] = %q, want %q", spine.Labels["netbox_id"], "1")
	}

	// Verify leaf-1 has tenant label
	leaf := entries[1]
	if leaf.Labels["tenant"] != "acme" {
		t.Errorf("entries[1].Labels[tenant] = %q, want %q", leaf.Labels["tenant"], "acme")
	}
}

func TestDiscoverPagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 0:
			page++
			nextURL := "http://" + r.Host + "/api/dcim/devices/?offset=1"
			resp := apiResponse{
				Count: 2,
				Next:  &nextURL,
				Results: []apiDevice{
					{ID: 1, Name: "r1", PrimaryIP: &apiIP{Address: "10.0.0.1"}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case 1:
			resp := apiResponse{
				Count: 2,
				Next:  nil,
				Results: []apiDevice{
					{ID: 2, Name: "r2", PrimaryIP: &apiIP{Address: "10.0.0.2"}},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}
	}))
	defer srv.Close()

	provider := &Provider{
		baseURL: srv.URL,
		token:   "tok",
		client:  srv.Client(),
	}

	entries, err := provider.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("Discover() returned %d entries, want 2", len(entries))
	}
	if entries[0].Name != "r1" || entries[1].Name != "r2" {
		t.Errorf("unexpected entries: %v, %v", entries[0].Name, entries[1].Name)
	}
}

func TestDiscoverSkipsDevicesWithoutIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := apiResponse{
			Count: 3,
			Results: []apiDevice{
				{ID: 1, Name: "r1", PrimaryIP: &apiIP{Address: "10.0.0.1/32"}},
				{ID: 2, Name: "r2", PrimaryIP: nil},                       // no IP, should be skipped
				{ID: 3, Name: "", PrimaryIP: &apiIP{Address: "10.0.0.3"}}, // no name, skipped
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	provider := &Provider{
		baseURL: srv.URL,
		token:   "tok",
		client:  srv.Client(),
	}

	entries, err := provider.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Discover() returned %d entries, want 1", len(entries))
	}
	if entries[0].Name != "r1" {
		t.Errorf("entries[0].Name = %q, want %q", entries[0].Name, "r1")
	}
}

func TestDiscoverHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()

	provider := &Provider{
		baseURL: srv.URL,
		token:   "tok",
		client:  srv.Client(),
	}

	_, err := provider.Discover(context.Background())
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
}

func TestDiscoverWithFilters(t *testing.T) {
	var receivedParams string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedParams = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(apiResponse{Count: 0, Results: []apiDevice{}})
	}))
	defer srv.Close()

	provider := &Provider{
		baseURL: srv.URL,
		token:   "tok",
		filters: map[string]string{"site": "dc1", "role": "router"},
		client:  srv.Client(),
	}

	_, err := provider.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Verify filters were passed as query params
	if !contains(receivedParams, "site=dc1") {
		t.Errorf("query params %q missing site=dc1", receivedParams)
	}
	if !contains(receivedParams, "role=router") {
		t.Errorf("query params %q missing role=router", receivedParams)
	}
}

func TestToDeviceEntryStripsCIDR(t *testing.T) {
	p := &Provider{}
	entry, err := p.toDeviceEntry(apiDevice{
		ID:        1,
		Name:      "r1",
		PrimaryIP: &apiIP{Address: "192.168.1.1/32"},
	})
	if err != nil {
		t.Fatalf("toDeviceEntry() error: %v", err)
	}
	if entry.Host != "192.168.1.1" {
		t.Errorf("Host = %q, want %q", entry.Host, "192.168.1.1")
	}
}

func TestToDeviceEntryNoSlash(t *testing.T) {
	p := &Provider{}
	entry, err := p.toDeviceEntry(apiDevice{
		ID:        1,
		Name:      "r1",
		PrimaryIP: &apiIP{Address: "192.168.1.1"},
	})
	if err != nil {
		t.Fatalf("toDeviceEntry() error: %v", err)
	}
	if entry.Host != "192.168.1.1" {
		t.Errorf("Host = %q, want %q", entry.Host, "192.168.1.1")
	}
}

func TestRegisteredInRegistry(t *testing.T) {
	// Verify init() registration worked by checking that the provider type
	// doesn't produce an "unknown provider type" error. We pass a valid
	// decoder with minimal config.
	rawMap := map[string]any{
		"type":  ProviderType,
		"url":   "https://example.com",
		"token": "test",
	}
	dec, err := yaml.Marshal(rawMap)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(dec))
	cfg, err := devicegroups.DecodeProviderConfig(context.Background(), ProviderType, decoder)
	if err != nil {
		t.Fatalf("DecodeProviderConfig() error: %v", err)
	}
	if cfg.ProviderConfigType() != ProviderType {
		t.Errorf("ProviderConfigType() = %q, want %q", cfg.ProviderConfigType(), ProviderType)
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && searchSubstring(s, sub)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
