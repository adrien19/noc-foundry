package internal

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/adrien19/noc-foundry/internal/server"
)

func TestServerConfigParser(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		parser := &ServerConfigParser{}
		cfg, err := parser.ParseServerConfig(context.Background(), []byte(`
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: https://nocfoundry.example.com/api
    mcp:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: https://nocfoundry.example.com/mcp
  ui:
    enabled: false
    authService: noc-keycloak
    clientId: noc-foundry-ui
    scopes: ["openid", "profile"]
    redirectPath: /ui/auth/callback
`))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if !cfg.EndpointAuth.API.Enabled || cfg.EndpointAuth.API.Audience != "https://nocfoundry.example.com/api" {
			t.Fatalf("unexpected API config: %+v", cfg.EndpointAuth.API)
		}
		if cfg.UI.ClientID != "noc-foundry-ui" {
			t.Fatalf("unexpected UI config: %+v", cfg.UI)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		parser := &ServerConfigParser{}
		_, err := parser.ParseServerConfig(context.Background(), []byte(`
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: https://nocfoundry.example.com/api
      unknownField: true
`))
		if err == nil {
			t.Fatal("expected strict unmarshal error")
		}
	})

	t.Run("env expansion", func(t *testing.T) {
		t.Setenv("NOCFOUNDRY_BASE_URL", "https://nocfoundry.example.com")
		parser := &ServerConfigParser{}
		cfg, err := parser.ParseServerConfig(context.Background(), []byte(`
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: ${NOCFOUNDRY_BASE_URL}/api
`))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if cfg.EndpointAuth.API.Audience != "https://nocfoundry.example.com/api" {
			t.Fatalf("unexpected config: %+v", cfg.EndpointAuth.API)
		}
	})

	t.Run("ui auth requires api endpoint auth linkage", func(t *testing.T) {
		parser := &ServerConfigParser{}
		_, err := parser.ParseServerConfig(context.Background(), []byte(`
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: https://nocfoundry.example.com/api
  ui:
    enabled: true
    authService: other-auth
    clientId: noc-foundry-ui
    scopes: ["openid"]
    redirectPath: /ui/auth/callback
`))
		if err == nil {
			t.Fatal("expected UI auth linkage validation error")
		}
	})
}

func TestLoadServerRuntimeConfig(t *testing.T) {
	tmpDir := t.TempDir()
	toolsFilePath := filepath.Join(tmpDir, "tools.yaml")
	if err := os.WriteFile(toolsFilePath, []byte(""), 0o644); err != nil {
		t.Fatalf("write tools file: %v", err)
	}
	serverFilePath := filepath.Join(tmpDir, "server.yaml")
	if err := os.WriteFile(serverFilePath, []byte(`
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: https://nocfoundry.example.com/api
`), 0o644); err != nil {
		t.Fatalf("write server file: %v", err)
	}

	opts := NewNOCFoundryOptions()
	opts.ToolsFile = toolsFilePath
	opts.ServerConfigFile = serverFilePath

	_, err := opts.LoadConfig(context.Background(), &ToolsFileParser{})
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	want := server.ServerAuthConfig{
		EndpointAuth: server.ServerEndpointAuthConfig{
			API: server.EndpointAuthPolicyConfig{
				Enabled:      true,
				AuthServices: []string{"noc-keycloak"},
				Audience:     "https://nocfoundry.example.com/api",
			},
		},
	}
	if !reflect.DeepEqual(opts.Cfg.Auth, want) {
		t.Fatalf("got %+v, want %+v", opts.Cfg.Auth, want)
	}
}

func TestLoadServerRuntimeConfigValidation(t *testing.T) {
	tmpDir := t.TempDir()
	toolsFilePath := filepath.Join(tmpDir, "tools.yaml")
	if err := os.WriteFile(toolsFilePath, []byte(""), 0o644); err != nil {
		t.Fatalf("write tools file: %v", err)
	}
	serverFilePath := filepath.Join(tmpDir, "server.yaml")
	if err := os.WriteFile(serverFilePath, []byte(`
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: []
`), 0o644); err != nil {
		t.Fatalf("write server file: %v", err)
	}

	opts := NewNOCFoundryOptions()
	opts.ToolsFile = toolsFilePath
	opts.ServerConfigFile = serverFilePath

	_, err := opts.LoadConfig(context.Background(), &ToolsFileParser{})
	if err == nil {
		t.Fatal("expected server config validation error")
	}
}
