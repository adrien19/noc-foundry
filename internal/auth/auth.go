// Copyright 2024 Google LLC
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

package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/goccy/go-yaml"
)

// AuthServiceConfig is the interface for configuring authentication services.
type AuthServiceConfig interface {
	AuthServiceConfigType() string
	Initialize() (AuthService, error)
}

// AuthService is the interface for authentication services.
type AuthService interface {
	AuthServiceType() string
	GetName() string
	GetClaimsFromHeader(context.Context, http.Header) (map[string]any, error)
	ToConfig() AuthServiceConfig
}

// IssuerProvider is an optional interface implemented by auth services that are
// backed by an OIDC provider. Callers can type-assert an AuthService to this
// interface to retrieve the issuer URL for RFC 9728 protected resource metadata
// without importing the concrete oidc package.
type IssuerProvider interface {
	OIDCIssuerURL() string
}

// EndpointSurface identifies an HTTP surface that can be protected by
// endpoint-level authentication.
type EndpointSurface string

const (
	EndpointSurfaceMCP EndpointSurface = "mcp"
	EndpointSurfaceAPI EndpointSurface = "api"
)

// EndpointPrincipal represents an authenticated principal at the HTTP surface
// boundary.
type EndpointPrincipal struct {
	AuthService string
	Issuer      string
	Subject     string
	Claims      map[string]any
}

// EndpointAuthService is an optional interface implemented by auth services
// that can authenticate HTTP surfaces such as /mcp and /api.
type EndpointAuthService interface {
	ValidateEndpointToken(context.Context, http.Header, string) (*EndpointPrincipal, error)
}

// EndpointAudienceConfigurer is an optional interface implemented by auth
// services that need the server's configured endpoint audiences to treat the
// same bearer token as valid for both endpoint access and downstream tool auth.
type EndpointAudienceConfigurer interface {
	SetEndpointAudiences([]string)
}

// AuthorizationServerMetadata contains the minimal OIDC discovery details a
// browser client needs to complete an authorization-code + PKCE flow.
type AuthorizationServerMetadata struct {
	Issuer                        string
	AuthorizationEndpoint         string
	TokenEndpoint                 string
	EndSessionEndpoint            string
	CodeChallengeMethodsSupported []string
}

// AuthorizationServerMetadataProvider is an optional interface implemented by
// OIDC-backed auth services that can surface discovery metadata for UI login.
type AuthorizationServerMetadataProvider interface {
	AuthorizationServerMetadata() AuthorizationServerMetadata
}

// AuthServiceConfigFactory creates an AuthServiceConfig from a YAML decoder.
type AuthServiceConfigFactory func(ctx context.Context, name string, decoder *yaml.Decoder) (AuthServiceConfig, error)

var authServiceRegistry = make(map[string]AuthServiceConfigFactory)

// Register registers a new auth service type with its factory.
// It returns false if the type is already registered (idempotent for init() safety).
func Register(serviceType string, factory AuthServiceConfigFactory) bool {
	if _, exists := authServiceRegistry[serviceType]; exists {
		return false
	}
	authServiceRegistry[serviceType] = factory
	return true
}

// DecodeConfig decodes an auth service configuration from the registered factory.
func DecodeConfig(ctx context.Context, serviceType string, name string, decoder *yaml.Decoder) (AuthServiceConfig, error) {
	factory, found := authServiceRegistry[serviceType]
	if !found {
		return nil, fmt.Errorf("unknown auth service type: %q", serviceType)
	}
	cfg, err := factory(ctx, name, decoder)
	if err != nil {
		return nil, fmt.Errorf("unable to parse auth service %q as %q: %w", name, serviceType, err)
	}
	return cfg, nil
}
