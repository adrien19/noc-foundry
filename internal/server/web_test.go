// Copyright 2025 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/server/resources"
	"github.com/go-chi/chi/v5"
)

type stubUIAuthService struct {
	name     string
	issuer   string
	metadata auth.AuthorizationServerMetadata
}

func (s stubUIAuthService) AuthServiceType() string { return "stub-oidc" }
func (s stubUIAuthService) GetName() string         { return s.name }
func (s stubUIAuthService) GetClaimsFromHeader(context.Context, http.Header) (map[string]any, error) {
	return nil, nil
}
func (s stubUIAuthService) ToConfig() auth.AuthServiceConfig { return nil }
func (s stubUIAuthService) OIDCIssuerURL() string            { return s.issuer }
func (s stubUIAuthService) AuthorizationServerMetadata() auth.AuthorizationServerMetadata {
	return s.metadata
}
func (s stubUIAuthService) ValidateEndpointToken(context.Context, http.Header, string) (*auth.EndpointPrincipal, error) {
	return nil, nil
}
func (s stubUIAuthService) SetEndpointAudiences([]string) {}

func TestRegisterWebUI(t *testing.T) {
	t.Run("static routes", func(t *testing.T) {
		r := chi.NewRouter()
		s := &Server{}
		if err := RegisterWebUI(r, s); err != nil {
			t.Fatalf("RegisterWebUI() error = %v", err)
		}

		tests := []struct {
			name string
			path string
		}{
			{name: "overview", path: "/ui/"},
			{name: "tools", path: "/ui/tools"},
			{name: "toolsets", path: "/ui/toolsets"},
			{name: "callback", path: "/ui/auth/callback"},
			{name: "css", path: "/ui/static/css/app.css"},
			{name: "js", path: "/ui/static/js/app.js"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, tt.path, nil)
				rr := httptest.NewRecorder()
				r.ServeHTTP(rr, req)

				if rr.Code != http.StatusOK {
					t.Fatalf("expected 200, got %d for %s", rr.Code, tt.path)
				}
			})
		}
	})

	t.Run("auth config disabled", func(t *testing.T) {
		r := chi.NewRouter()
		s := &Server{}
		if err := RegisterWebUI(r, s); err != nil {
			t.Fatalf("RegisterWebUI() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "http://nocfoundry.example.com/ui/auth/config", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		var got uiAuthConfigResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if got.Enabled {
			t.Fatalf("expected disabled response, got %+v", got)
		}
	})

	t.Run("auth config enabled", func(t *testing.T) {
		r := chi.NewRouter()
		authSvc := stubUIAuthService{
			name:   "noc-keycloak",
			issuer: "https://issuer.example.com/realms/network-ops",
			metadata: auth.AuthorizationServerMetadata{
				Issuer:                        "https://issuer.example.com/realms/network-ops",
				AuthorizationEndpoint:         "https://issuer.example.com/realms/network-ops/protocol/openid-connect/auth",
				TokenEndpoint:                 "https://issuer.example.com/realms/network-ops/protocol/openid-connect/token",
				EndSessionEndpoint:            "https://issuer.example.com/realms/network-ops/protocol/openid-connect/logout",
				CodeChallengeMethodsSupported: []string{"S256"},
			},
		}
		s := &Server{
			authConfig: ServerAuthConfig{
				EndpointAuth: ServerEndpointAuthConfig{
					API: EndpointAuthPolicyConfig{
						Enabled:      true,
						AuthServices: []string{"noc-keycloak"},
						Audience:     "https://nocfoundry.example.com/api",
					},
				},
				UI: ServerUIAuthConfig{
					Enabled:      true,
					AuthService:  "noc-keycloak",
					ClientID:     "noc-foundry-ui",
					Scopes:       []string{"openid", "profile", "email"},
					RedirectPath: "/ui/auth/callback",
				},
			},
			ResourceMgr: resources.NewResourceManager(nil, map[string]auth.AuthService{
				"noc-keycloak": authSvc,
			}, nil, nil, nil, nil, nil),
		}
		if err := RegisterWebUI(r, s); err != nil {
			t.Fatalf("RegisterWebUI() error = %v", err)
		}

		req := httptest.NewRequest(http.MethodGet, "http://nocfoundry.example.com/ui/auth/config", nil)
		req.Host = "nocfoundry.example.com"
		req.Header.Set("X-Forwarded-Proto", "https")
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}

		var got uiAuthConfigResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if !got.Enabled {
			t.Fatalf("expected enabled response, got %+v", got)
		}
		if got.RedirectURI != "https://nocfoundry.example.com/ui/auth/callback" {
			t.Fatalf("unexpected redirectUri: %q", got.RedirectURI)
		}
		if got.APIAudience != "https://nocfoundry.example.com/api" {
			t.Fatalf("unexpected apiAudience: %q", got.APIAudience)
		}
		if got.AuthorizationEndpoint == "" || got.TokenEndpoint == "" {
			t.Fatalf("expected discovery metadata, got %+v", got)
		}
	})
}
