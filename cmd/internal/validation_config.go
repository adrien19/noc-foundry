// Copyright 2026 Google LLC
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

package internal

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/adrien19/noc-foundry/internal/server"
	yaml "github.com/goccy/go-yaml"
)

type ValidationConfigParser struct {
	EnvVars map[string]string
}

func (p *ValidationConfigParser) ParseValidationConfig(ctx context.Context, raw []byte) (server.ValidationRunConfig, error) {
	var cfg server.ValidationRunConfig
	output, err := parseEnv(string(raw), p.EnvVars)
	if err != nil {
		return cfg, fmt.Errorf("error parsing environment variables: %s", err)
	}
	decoder := yaml.NewDecoder(bytes.NewReader([]byte(output)), yaml.Strict())
	if err := decoder.DecodeContext(ctx, &cfg); err != nil {
		return cfg, fmt.Errorf("unable to decode validation runtime config: %w", err)
	}
	if err := server.ValidateValidationRunConfig(cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (p *ValidationConfigParser) LoadValidationConfigFile(ctx context.Context, path string) (server.ValidationRunConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return server.ValidationRunConfig{}, fmt.Errorf("unable to read validation config file at %q: %w", path, err)
	}
	return p.ParseValidationConfig(ctx, raw)
}
