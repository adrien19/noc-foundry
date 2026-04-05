package server

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/auth/oidc"
)

type stubNonOIDCAuthService struct{}

func (s stubNonOIDCAuthService) AuthServiceType() string { return "stub" }
func (s stubNonOIDCAuthService) GetName() string         { return "stub" }
func (s stubNonOIDCAuthService) GetClaimsFromHeader(context.Context, http.Header) (map[string]any, error) {
	return nil, nil
}
func (s stubNonOIDCAuthService) ToConfig() auth.AuthServiceConfig { return nil }

type stubEndpointOIDCWithoutMetadataAuthService struct{}

func (s stubEndpointOIDCWithoutMetadataAuthService) AuthServiceType() string { return "stub-oidc" }
func (s stubEndpointOIDCWithoutMetadataAuthService) GetName() string         { return "stub" }
func (s stubEndpointOIDCWithoutMetadataAuthService) GetClaimsFromHeader(context.Context, http.Header) (map[string]any, error) {
	return nil, nil
}
func (s stubEndpointOIDCWithoutMetadataAuthService) ToConfig() auth.AuthServiceConfig {
	return nil
}
func (s stubEndpointOIDCWithoutMetadataAuthService) OIDCIssuerURL() string {
	return "https://issuer.example.com/realms/network-ops"
}
func (s stubEndpointOIDCWithoutMetadataAuthService) ValidateEndpointToken(context.Context, http.Header, string) (*auth.EndpointPrincipal, error) {
	return nil, nil
}

func TestValidateAndApplyEndpointAuthConfig_MissingAuthService(t *testing.T) {
	err := ValidateAndApplyEndpointAuthConfig(ServerAuthConfig{
		EndpointAuth: ServerEndpointAuthConfig{
			API: EndpointAuthPolicyConfig{
				Enabled:      true,
				AuthServices: []string{"missing"},
				Audience:     "https://nocfoundry.example.com/api",
			},
		},
	}, map[string]auth.AuthService{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "unknown auth service") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAndApplyEndpointAuthConfig_ConfiguresEndpointAudiences(t *testing.T) {
	key := newEndpointRSATestKey(t)
	issuer := mockEndpointOIDCServer(t, key)

	svc, err := oidc.Config{Name: "noc-keycloak", IssuerURL: issuer.URL, ClientID: "noc-foundry"}.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
	authServices := map[string]auth.AuthService{"noc-keycloak": svc}

	err = ValidateAndApplyEndpointAuthConfig(ServerAuthConfig{
		EndpointAuth: ServerEndpointAuthConfig{
			API: EndpointAuthPolicyConfig{
				Enabled:      true,
				AuthServices: []string{"noc-keycloak"},
				Audience:     "https://nocfoundry.example.com/api",
			},
		},
	}, authServices)
	if err != nil {
		t.Fatalf("ValidateAndApplyEndpointAuthConfig() error: %v", err)
	}

	nowToken := mintEndpointJWT(t, key, map[string]any{
		"iss": issuer.URL,
		"aud": "https://nocfoundry.example.com/api",
		"sub": "noc-operator",
		"iat": 1,
		"exp": 4102444800,
	})

	header := http.Header{}
	header.Set("Authorization", "Bearer "+nowToken)
	claims, err := svc.GetClaimsFromHeader(context.Background(), header)
	if err != nil {
		t.Fatalf("GetClaimsFromHeader() error: %v", err)
	}
	if claims == nil {
		t.Fatal("expected claims, got nil")
	}
}

func TestValidateAndApplyEndpointAuthConfig_UIAuthRequiresOIDCMetadata(t *testing.T) {
	err := ValidateAndApplyEndpointAuthConfig(ServerAuthConfig{
		EndpointAuth: ServerEndpointAuthConfig{
			API: EndpointAuthPolicyConfig{
				Enabled:      true,
				AuthServices: []string{"stub"},
				Audience:     "https://nocfoundry.example.com/api",
			},
		},
		UI: ServerUIAuthConfig{
			Enabled:      true,
			AuthService:  "stub",
			ClientID:     "noc-foundry-ui",
			Scopes:       []string{"openid"},
			RedirectPath: "/ui/auth/callback",
		},
	}, map[string]auth.AuthService{
		"stub": stubEndpointOIDCWithoutMetadataAuthService{},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "metadata") {
		t.Fatalf("unexpected error: %v", err)
	}
}
