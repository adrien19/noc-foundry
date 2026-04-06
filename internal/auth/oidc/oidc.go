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

// Package oidc provides an OIDC / JWT authentication service for noc-foundry.
// It validates Bearer tokens issued by any standard OIDC provider — Keycloak,
// Azure AD, Okta, Dex, etc. — via OIDC discovery and JWKS-based signature
// verification.  JWKS key-sets are cached and rotated automatically by the
// underlying go-oidc library.
//
// Minimal YAML configuration for Keycloak:
//
//	kind: authServices
//	name: noc-keycloak
//	type: oidc
//	issuerUrl: https://keycloak.example.com/realms/network-ops
//	clientId: noc-foundry
//
// Optional fields that improve interoperability:
//
//	skipClientIdCheck: true   # for service-account tokens that omit the aud claim
//	caCertFile: /etc/ssl/keycloak-ca.pem  # for Keycloak with a private CA
package oidc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/adrien19/noc-foundry/internal/auth"
	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/goccy/go-yaml"
)

const AuthServiceType = "oidc"

func init() {
	auth.Register(AuthServiceType, func(ctx context.Context, name string, decoder *yaml.Decoder) (auth.AuthServiceConfig, error) {
		cfg := Config{Name: name}
		if err := decoder.DecodeContext(ctx, &cfg); err != nil {
			return nil, err
		}
		return cfg, nil
	})
}

// Config is the YAML configuration for the OIDC auth service.
type Config struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	// IssuerURL is the base URL of the OIDC provider (e.g.
	// "https://keycloak.example.com/realms/network-ops" or
	// "https://login.microsoftonline.com/<tenant-id>/v2.0").
	// noc-foundry performs OIDC discovery by fetching
	// <IssuerURL>/.well-known/openid-configuration at startup.
	IssuerURL string `yaml:"issuerUrl" validate:"required"`
	// ClientID is the OAuth 2.0 client identifier registered with the provider.
	// Used to verify the token's "aud" (audience) claim.
	ClientID string `yaml:"clientId" validate:"required"`
	// SkipClientIDCheck disables the audience check.  Useful for
	// machine-to-machine tokens (Keycloak service accounts, Azure managed
	// identities) that may omit or set a non-matching "aud" value.
	SkipClientIDCheck bool `yaml:"skipClientIdCheck,omitempty"`
	// CACertFile is the path to a PEM-encoded CA certificate that signed the
	// OIDC provider's TLS certificate.  Required only when the provider uses a
	// private CA.  If empty the system trust store is used.
	CACertFile string `yaml:"caCertFile,omitempty"`
	// EndpointAuth is deprecated. Server-scoped endpoint auth policy now lives
	// in --server-config and this field is ignored for enforcement.
	EndpointAuth EndpointAuthConfig `yaml:"endpointAuth,omitempty"`
}

type EndpointAuthConfig struct {
	MCP EndpointSurfaceConfig `yaml:"mcp,omitempty"`
	API EndpointSurfaceConfig `yaml:"api,omitempty"`
}

type EndpointSurfaceConfig struct {
	Enabled  bool   `yaml:"enabled,omitempty"`
	Audience string `yaml:"audience,omitempty"`
}

func (c Config) AuthServiceConfigType() string { return AuthServiceType }

// Initialize contacts the OIDC provider's discovery endpoint, downloads the
// JWKS, and returns a ready-to-use AuthService.  Fails fast if the provider
// is unreachable, so misconfigured issuers are surfaced at startup rather than
// on the first request.
func (c Config) Initialize() (auth.AuthService, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}

	httpClient, err := buildHTTPClient(c.CACertFile)
	if err != nil {
		return nil, fmt.Errorf("oidc: building HTTP client: %w", err)
	}

	// Inject the custom HTTP client into the context so go-oidc uses it for
	// OIDC discovery and JWKS fetches.
	providerCtx := coreoidc.ClientContext(context.Background(), httpClient)
	provider, err := coreoidc.NewProvider(providerCtx, c.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovering issuer %q: %w", c.IssuerURL, err)
	}

	verifier := provider.Verifier(&coreoidc.Config{
		ClientID:          c.ClientID,
		SkipClientIDCheck: c.SkipClientIDCheck,
	})

	endpointVerifier := provider.Verifier(&coreoidc.Config{
		SkipClientIDCheck: true,
	})

	metadata := auth.AuthorizationServerMetadata{
		Issuer:                c.IssuerURL,
		AuthorizationEndpoint: provider.Endpoint().AuthURL,
		TokenEndpoint:         provider.Endpoint().TokenURL,
	}
	var discovery struct {
		Issuer                        string   `json:"issuer"`
		AuthorizationEndpoint         string   `json:"authorization_endpoint"`
		TokenEndpoint                 string   `json:"token_endpoint"`
		EndSessionEndpoint            string   `json:"end_session_endpoint"`
		CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	}
	if err := provider.Claims(&discovery); err == nil {
		if discovery.Issuer != "" {
			metadata.Issuer = discovery.Issuer
		}
		if discovery.AuthorizationEndpoint != "" {
			metadata.AuthorizationEndpoint = discovery.AuthorizationEndpoint
		}
		if discovery.TokenEndpoint != "" {
			metadata.TokenEndpoint = discovery.TokenEndpoint
		}
		metadata.EndSessionEndpoint = discovery.EndSessionEndpoint
		metadata.CodeChallengeMethodsSupported = append([]string(nil), discovery.CodeChallengeMethodsSupported...)
	}
	if len(metadata.CodeChallengeMethodsSupported) == 0 {
		metadata.CodeChallengeMethodsSupported = []string{"S256"}
	}

	return &AuthService{
		config:           c,
		verifier:         verifier,
		endpointVerifier: endpointVerifier,
		httpClient:       httpClient,
		metadata:         metadata,
	}, nil
}

// AuthService validates OIDC Bearer tokens.
type AuthService struct {
	config            Config
	verifier          *coreoidc.IDTokenVerifier
	endpointVerifier  *coreoidc.IDTokenVerifier
	httpClient        *http.Client
	endpointAudiences []string
	metadata          auth.AuthorizationServerMetadata
}

var _ auth.AuthService = (*AuthService)(nil)
var _ auth.IssuerProvider = (*AuthService)(nil)
var _ auth.EndpointAuthService = (*AuthService)(nil)
var _ auth.EndpointAudienceConfigurer = (*AuthService)(nil)
var _ auth.AuthorizationServerMetadataProvider = (*AuthService)(nil)

func (a *AuthService) AuthServiceType() string          { return AuthServiceType }
func (a *AuthService) GetName() string                  { return a.config.Name }
func (a *AuthService) ToConfig() auth.AuthServiceConfig { return a.config }

// OIDCIssuerURL returns the OIDC provider issuer URL, implementing auth.IssuerProvider.
func (a *AuthService) OIDCIssuerURL() string { return a.config.IssuerURL }
func (a *AuthService) AuthorizationServerMetadata() auth.AuthorizationServerMetadata {
	return a.metadata
}

func (a *AuthService) SetEndpointAudiences(audiences []string) {
	a.endpointAudiences = append([]string(nil), audiences...)
}

func (a *AuthService) ValidateEndpointToken(ctx context.Context, h http.Header, audience string) (*auth.EndpointPrincipal, error) {
	rawToken, ok := bearerToken(h)
	if !ok {
		return nil, nil
	}

	iss, err := jwtIssuer(rawToken)
	if err != nil || iss != a.config.IssuerURL {
		return nil, nil
	}

	idToken, err := a.endpointVerifier.Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("oidc: endpoint token verification failed: %w", err)
	}
	if !containsAudience(idToken.Audience, audience) {
		return nil, fmt.Errorf("oidc: endpoint token audience does not include %q", audience)
	}

	claims, err := claimsFromIDToken(idToken)
	if err != nil {
		return nil, err
	}

	sub, _ := claims["sub"].(string)
	return &auth.EndpointPrincipal{
		AuthService: a.config.Name,
		Issuer:      a.config.IssuerURL,
		Subject:     sub,
		Claims:      claims,
	}, nil
}

// GetClaimsFromHeader extracts, validates, and returns the claims from the
// Bearer token in the Authorization header.
//
// Behaviour:
//   - Absent header or non-Bearer value → (nil, nil):  this request does not
//     carry a token for this auth service; other services are still tried.
//   - Token present but issued by a different issuer (iss ≠ IssuerURL) →
//     (nil, nil): silently skip; allows multiple OIDC services to coexist.
//   - Token is for our issuer but fails verification → (nil, error): the
//     caller attempted to authenticate with this provider but the token is
//     bad (expired, wrong signature, revoked audience, …).
//   - Valid token → (claims map, nil).
func (a *AuthService) GetClaimsFromHeader(ctx context.Context, h http.Header) (map[string]any, error) {
	rawToken, ok := bearerToken(h)
	if !ok {
		return nil, nil
	}

	// Pre-check: decode the payload without verification to read the issuer.
	// This short-circuits validation for tokens from unrelated providers and
	// avoids unnecessary JWKS fetches.
	iss, err := jwtIssuer(rawToken)
	if err != nil {
		// Malformed JWT — not for us.
		return nil, nil
	}
	if iss != a.config.IssuerURL {
		return nil, nil
	}

	return a.verifyClaimsForToolUse(ctx, rawToken)
}

// bearerToken extracts the raw JWT from an "Authorization: Bearer <token>"
// header.  Returns ("", false) when the header is absent or not a Bearer.
func bearerToken(h http.Header) (string, bool) {
	v := h.Get("Authorization")
	if v == "" {
		return "", false
	}
	if !strings.HasPrefix(v, "Bearer ") {
		return "", false
	}
	raw := strings.TrimPrefix(v, "Bearer ")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}
	return raw, true
}

// jwtIssuer extracts the "iss" claim from the JWT payload without performing
// any signature verification.  Used purely for routing logic.
func jwtIssuer(rawJWT string) (string, error) {
	// A JWT has three base64url-encoded segments separated by ".".
	parts := strings.SplitN(rawJWT, ".", 3)
	if len(parts) != 3 {
		return "", fmt.Errorf("not a JWT")
	}
	// Add padding if necessary.
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	data, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decoding JWT payload: %w", err)
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(data, &claims); err != nil {
		return "", fmt.Errorf("parsing JWT payload: %w", err)
	}
	return claims.Iss, nil
}

// buildHTTPClient returns an *http.Client configured to trust the given CA
// cert file.  When caCertFile is empty the default system trust store is used.
func buildHTTPClient(caCertFile string) (*http.Client, error) {
	if caCertFile == "" {
		return http.DefaultClient, nil
	}
	pem, err := os.ReadFile(caCertFile) // #nosec G304 — operator-supplied CA cert path
	if err != nil {
		return nil, fmt.Errorf("reading CA cert %q: %w", caCertFile, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no valid certificates found in %q", caCertFile)
	}
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		},
	}, nil
}

func (c Config) validate() error { return nil }

func containsAudience(audiences []string, want string) bool {
	for _, audience := range audiences {
		if audience == want {
			return true
		}
	}
	return false
}

func (a *AuthService) verifyClaimsForToolUse(ctx context.Context, rawToken string) (map[string]any, error) {
	idToken, err := a.verifier.Verify(ctx, rawToken)
	if err == nil {
		return claimsFromIDToken(idToken)
	}

	endpointToken, endpointErr := a.endpointVerifier.Verify(ctx, rawToken)
	if endpointErr != nil || !a.matchesAnyEndpointAudience(endpointToken.Audience) {
		return nil, fmt.Errorf("oidc: token verification failed: %w", err)
	}
	return claimsFromIDToken(endpointToken)
}

func (a *AuthService) matchesAnyEndpointAudience(audiences []string) bool {
	for _, audience := range a.endpointAudiences {
		if containsAudience(audiences, audience) {
			return true
		}
	}
	return false
}

func claimsFromIDToken(idToken *coreoidc.IDToken) (map[string]any, error) {
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc: extracting claims: %w", err)
	}
	return claims, nil
}
