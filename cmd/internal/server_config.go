package internal

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/adrien19/noc-foundry/internal/server"
	yaml "github.com/goccy/go-yaml"
)

type ServerConfigParser struct {
	EnvVars map[string]string
}

type serverRuntimeConfigFile struct {
	Auth server.ServerAuthConfig `yaml:"auth,omitempty"`
}

func (p *ServerConfigParser) ParseServerConfig(ctx context.Context, raw []byte) (server.ServerAuthConfig, error) {
	var cfg serverRuntimeConfigFile
	output, err := parseEnv(string(raw), p.EnvVars)
	if err != nil {
		return server.ServerAuthConfig{}, fmt.Errorf("error parsing environment variables: %s", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(output)), yaml.Strict())
	if err := decoder.DecodeContext(ctx, &cfg); err != nil {
		return server.ServerAuthConfig{}, fmt.Errorf("unable to decode server runtime config: %w", err)
	}
	if err := server.ValidateServerAuthConfig(cfg.Auth); err != nil {
		return server.ServerAuthConfig{}, err
	}
	return cfg.Auth, nil
}

func (p *ServerConfigParser) LoadServerConfigFile(ctx context.Context, path string) (server.ServerAuthConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return server.ServerAuthConfig{}, fmt.Errorf("unable to read server config file at %q: %w", path, err)
	}
	return p.ParseServerConfig(ctx, raw)
}
