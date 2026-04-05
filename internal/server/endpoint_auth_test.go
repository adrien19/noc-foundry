package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/auth/oidc"
	"github.com/adrien19/noc-foundry/internal/log"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/server/mcp/jsonrpc"
	"github.com/adrien19/noc-foundry/internal/server/resources"
	"github.com/adrien19/noc-foundry/internal/telemetry"
	"github.com/adrien19/noc-foundry/internal/tools"
)

type endpointRSATestKey struct {
	priv *rsa.PrivateKey
	kid  string
}

func newEndpointRSATestKey(t *testing.T) *endpointRSATestKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	return &endpointRSATestKey{priv: k, kid: "endpoint-key-1"}
}

func endpointJWKSFromKey(key *endpointRSATestKey) map[string]any {
	pub := key.priv.Public().(*rsa.PublicKey)
	return map[string]any{
		"keys": []map[string]any{
			{
				"kty": "RSA",
				"use": "sig",
				"kid": key.kid,
				"alg": "RS256",
				"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			},
		},
	}
}

func mockEndpointOIDCServer(t *testing.T, key *endpointRSATestKey) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			doc := map[string]any{
				"issuer":                                srv.URL,
				"jwks_uri":                              srv.URL + "/keys",
				"authorization_endpoint":                srv.URL + "/auth",
				"token_endpoint":                        srv.URL + "/token",
				"response_types_supported":              []string{"code"},
				"subject_types_supported":               []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(doc)
		case "/keys":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(endpointJWKSFromKey(key))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func mintEndpointJWT(t *testing.T, key *endpointRSATestKey, claims map[string]any) string {
	t.Helper()

	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": key.kid}
	encodeJSON := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}

	signingInput := encodeJSON(header) + "." + encodeJSON(claims)
	digest := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key.priv, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15: %v", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func newEndpointAuthHeader(token string) map[string]string {
	return map[string]string{
		"Authorization": "Bearer " + token,
	}
}

func setUpServerWithAuthServices(t *testing.T, router string, authCfg ServerAuthConfig, toolsMap map[string]tools.Tool, toolsets map[string]tools.Toolset, promptsMap map[string]prompts.Prompt, promptsets map[string]prompts.Promptset, authServices map[string]auth.AuthService) (chiRouter http.Handler, shutdown func()) {
	ctx, cancel := context.WithCancel(context.Background())

	testLogger, err := log.NewStdLogger(io.Discard, io.Discard, "info")
	if err != nil {
		t.Fatalf("unable to initialize logger: %s", err)
	}

	otelShutdown, err := telemetry.SetupOTel(ctx, fakeVersionString, "", "nocfoundry")
	if err != nil {
		t.Fatalf("unable to setup otel: %s", err)
	}

	instrumentation, err := telemetry.CreateTelemetryInstrumentation(fakeVersionString)
	if err != nil {
		t.Fatalf("unable to create custom metrics: %s", err)
	}

	sseManager := newSseManager(ctx)
	resourceManager := resources.NewResourceManager(nil, authServices, nil, toolsMap, toolsets, promptsMap, promptsets)
	if err := ValidateAndApplyEndpointAuthConfig(authCfg, authServices); err != nil {
		t.Fatalf("ValidateAndApplyEndpointAuthConfig() error: %v", err)
	}
	server := Server{
		version:         fakeVersionString,
		logger:          testLogger,
		instrumentation: instrumentation,
		sseManager:      sseManager,
		authConfig:      authCfg,
		ResourceMgr:     resourceManager,
	}

	var handler http.Handler
	switch router {
	case "api":
		handler, err = apiRouter(&server)
	case "mcp":
		handler, err = mcpRouter(&server)
	default:
		t.Fatalf("unknown router: %s", router)
	}
	if err != nil {
		t.Fatalf("unable to initialize router: %s", err)
	}

	shutdown = func() {
		cancel()
		if err := otelShutdown(ctx); err != nil {
			t.Fatalf("error shutting down OpenTelemetry: %s", err)
		}
	}
	return handler, shutdown
}

func runSSERequestWithHeaders(ts *httptest.Server, path string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		return nil, fmt.Errorf("unable to create request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("unable to send request: %w", err)
	}
	return resp, nil
}

func TestUnmarshalYAMLAuthServiceConfig_OIDCDeprecatedEndpointAuth(t *testing.T) {
	cfg, err := UnmarshalYAMLAuthServiceConfig(context.Background(), "noc-oidc", map[string]any{
		"type":      "oidc",
		"issuerUrl": "https://issuer.example.com/realms/network-ops",
		"clientId":  "noc-foundry",
		"endpointAuth": map[string]any{
			"mcp": map[string]any{
				"enabled":  true,
				"audience": "https://nocfoundry.example.com/mcp",
			},
			"api": map[string]any{
				"enabled":  true,
				"audience": "https://nocfoundry.example.com/api",
			},
		},
	})
	if err != nil {
		t.Fatalf("UnmarshalYAMLAuthServiceConfig() error: %v", err)
	}

	oidcCfg, ok := cfg.(oidc.Config)
	if !ok {
		t.Fatalf("unexpected config type %T", cfg)
	}
	if !oidcCfg.EndpointAuth.MCP.Enabled || !oidcCfg.EndpointAuth.API.Enabled {
		t.Fatalf("expected deprecated endpointAuth fields to remain parseable: %+v", oidcCfg.EndpointAuth)
	}
}

func TestProtectedResourceMetadataHandler_SurfaceSpecific(t *testing.T) {
	mcpKey := newEndpointRSATestKey(t)
	mcpIssuer := mockEndpointOIDCServer(t, mcpKey)
	apiKey := newEndpointRSATestKey(t)
	apiIssuer := mockEndpointOIDCServer(t, apiKey)

	mcpSvc, err := oidc.Config{Name: "mcp-auth", IssuerURL: mcpIssuer.URL, ClientID: "noc-foundry"}.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
	apiSvc, err := oidc.Config{Name: "api-auth", IssuerURL: apiIssuer.URL, ClientID: "noc-foundry"}.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}

	s := &Server{
		authConfig: ServerAuthConfig{
			EndpointAuth: ServerEndpointAuthConfig{
				MCP: EndpointAuthPolicyConfig{
					Enabled:      true,
					AuthServices: []string{"mcp-auth"},
					Audience:     "https://nocfoundry.example.com/mcp",
				},
				API: EndpointAuthPolicyConfig{
					Enabled:      true,
					AuthServices: []string{"api-auth"},
					Audience:     "https://nocfoundry.example.com/api",
				},
			},
		},
		ResourceMgr: resources.NewResourceManager(nil, map[string]auth.AuthService{
			"mcp-auth": mcpSvc,
			"api-auth": apiSvc,
		}, nil, nil, nil, nil, nil),
	}

	for _, tc := range []struct {
		surface  auth.EndpointSurface
		wantURL  string
		wantAuth string
	}{
		{surface: auth.EndpointSurfaceMCP, wantURL: "https://nocfoundry.example.com/mcp", wantAuth: mcpIssuer.URL},
		{surface: auth.EndpointSurfaceAPI, wantURL: "https://nocfoundry.example.com/api", wantAuth: apiIssuer.URL},
	} {
		req := httptest.NewRequest(http.MethodGet, "http://nocfoundry.example.com/.well-known/oauth-protected-resource/"+string(tc.surface), nil)
		req.Host = "nocfoundry.example.com"
		req.Header.Set("X-Forwarded-Proto", "https")
		rr := httptest.NewRecorder()

		protectedResourceMetadataHandler(s, tc.surface, rr, req)

		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("json.Unmarshal() error: %v", err)
		}
		if got["resource"] != tc.wantURL {
			t.Fatalf("resource = %v, want %s", got["resource"], tc.wantURL)
		}
		authServers, ok := got["authorization_servers"].([]any)
		if !ok || len(authServers) != 1 || authServers[0] != tc.wantAuth {
			t.Fatalf("authorization_servers = %v, want [%s]", got["authorization_servers"], tc.wantAuth)
		}
	}
}

func TestAPIEndpointAuth(t *testing.T) {
	apiKey := newEndpointRSATestKey(t)
	apiIssuer := mockEndpointOIDCServer(t, apiKey)
	otherKey := newEndpointRSATestKey(t)
	otherIssuer := mockEndpointOIDCServer(t, otherKey)

	apiSvc, err := oidc.Config{Name: "api-auth", IssuerURL: apiIssuer.URL, ClientID: "noc-foundry"}.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}
	otherSvc, err := oidc.Config{Name: "other-auth", IssuerURL: otherIssuer.URL, ClientID: "noc-foundry"}.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}

	mockTools := []MockTool{tool1}
	toolsMap, toolsets, _, _ := setUpResources(t, mockTools, nil)
	r, shutdown := setUpServerWithAuthServices(t, "api", ServerAuthConfig{
		EndpointAuth: ServerEndpointAuthConfig{
			API: EndpointAuthPolicyConfig{
				Enabled:      true,
				AuthServices: []string{"api-auth"},
				Audience:     "https://nocfoundry.example.com/api",
			},
		},
	}, toolsMap, toolsets, nil, nil, map[string]auth.AuthService{
		"api-auth":   apiSvc,
		"other-auth": otherSvc,
	})
	defer shutdown()
	ts := httptest.NewServer(r)
	defer ts.Close()

	resp, _, err := runRequest(ts, http.MethodGet, "/tools", nil, nil)
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if got := resp.Header.Get("WWW-Authenticate"); got != `Bearer resource_metadata="`+ts.URL+`/.well-known/oauth-protected-resource/api"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}

	now := time.Now()
	validToken := mintEndpointJWT(t, apiKey, map[string]any{
		"iss": apiIssuer.URL,
		"aud": "https://nocfoundry.example.com/api",
		"sub": "noc-operator",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	resp, _, err = runRequest(ts, http.MethodGet, "/tools", nil, newEndpointAuthHeader(validToken))
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	wrongAudienceToken := mintEndpointJWT(t, apiKey, map[string]any{
		"iss": apiIssuer.URL,
		"aud": "https://nocfoundry.example.com/mcp",
		"sub": "noc-operator",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	resp, _, err = runRequest(ts, http.MethodGet, "/tools", nil, newEndpointAuthHeader(wrongAudienceToken))
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	otherToken := mintEndpointJWT(t, otherKey, map[string]any{
		"iss": otherIssuer.URL,
		"aud": "https://nocfoundry.example.com/api",
		"sub": "noc-operator",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	resp, _, err = runRequest(ts, http.MethodGet, "/tools", nil, newEndpointAuthHeader(otherToken))
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestMCPEndpointAuthAndSessionBinding(t *testing.T) {
	key := newEndpointRSATestKey(t)
	issuer := mockEndpointOIDCServer(t, key)

	svc, err := oidc.Config{Name: "mcp-auth", IssuerURL: issuer.URL, ClientID: "noc-foundry"}.Initialize()
	if err != nil {
		t.Fatalf("Initialize() error: %v", err)
	}

	mockTools := []MockTool{tool1}
	mockPrompts := []MockPrompt{prompt1}
	toolsMap, toolsets, promptsMap, promptsets := setUpResources(t, mockTools, mockPrompts)
	r, shutdown := setUpServerWithAuthServices(t, "mcp", ServerAuthConfig{
		EndpointAuth: ServerEndpointAuthConfig{
			MCP: EndpointAuthPolicyConfig{
				Enabled:      true,
				AuthServices: []string{"mcp-auth"},
				Audience:     "https://nocfoundry.example.com/mcp",
			},
		},
	}, toolsMap, toolsets, promptsMap, promptsets, map[string]auth.AuthService{
		"mcp-auth": svc,
	})
	defer shutdown()
	ts := httptest.NewServer(r)
	defer ts.Close()

	resp, _, err := runRequest(ts, http.MethodPost, "/", bytes.NewBufferString(`{"jsonrpc":"2.0","id":"ping","method":"ping"}`), nil)
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}

	sseResp, err := runSSERequestWithHeaders(ts, "/sse", nil)
	if err != nil {
		t.Fatalf("runSSERequestWithHeaders() error: %v", err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", sseResp.StatusCode)
	}

	now := time.Now()
	tokenA := mintEndpointJWT(t, key, map[string]any{
		"iss": issuer.URL,
		"aud": "https://nocfoundry.example.com/mcp",
		"sub": "operator-a",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	})
	tokenB := mintEndpointJWT(t, key, map[string]any{
		"iss": issuer.URL,
		"aud": "https://nocfoundry.example.com/mcp",
		"sub": "operator-b",
		"iat": now.Unix(),
		"exp": now.Add(5 * time.Minute).Unix(),
	})

	sseResp, err = runSSERequestWithHeaders(ts, "/sse", newEndpointAuthHeader(tokenA))
	if err != nil {
		t.Fatalf("runSSERequestWithHeaders() error: %v", err)
	}
	defer sseResp.Body.Close()
	if sseResp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", sseResp.StatusCode)
	}

	buffer := make([]byte, 1024)
	n, err := sseResp.Body.Read(buffer)
	if err != nil {
		t.Fatalf("unable to read SSE response: %v", err)
	}
	endpointEvent := string(buffer[:n])
	parts := strings.SplitN(endpointEvent, "sessionId=", 2)
	if len(parts) != 2 {
		t.Fatalf("unable to parse session id from %q", endpointEvent)
	}
	sessionID := strings.TrimSpace(parts[1])

	reqBody := jsonrpc.JSONRPCRequest{
		Jsonrpc: "2.0",
		Id:      "ping",
		Request: jsonrpc.Request{Method: "ping"},
	}
	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("json.Marshal() error: %v", err)
	}

	resp, _, err = runRequest(ts, http.MethodPost, "/?sessionId="+sessionID, bytes.NewBuffer(reqBytes), newEndpointAuthHeader(tokenB))
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}

	resp, _, err = runRequest(ts, http.MethodPost, "/", bytes.NewBuffer(reqBytes), newEndpointAuthHeader(tokenA))
	if err != nil {
		t.Fatalf("runRequest() error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
