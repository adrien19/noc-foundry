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

package server

import (
	"fmt"
	"strings"

	"github.com/adrien19/noc-foundry/internal/util"
	yaml "github.com/goccy/go-yaml"
)

// SchemaRepoConfigs maps a user-assigned name to a schema repo configuration.
type SchemaRepoConfigs map[string]*SchemaRepoConfig

// SchemaRepoConfig describes a remote Git repository containing vendor YANG models.
type SchemaRepoConfig struct {
	Name     string              `yaml:"name" validate:"required"`
	URL      string              `yaml:"url" validate:"required"`
	Auth     SchemaRepoAuth      `yaml:"auth,omitempty"`
	Versions []SchemaRepoVersion `yaml:"versions" validate:"required,min=1"`
}

// SchemaRepoAuth describes how to authenticate when cloning a Git repository.
type SchemaRepoAuth struct {
	// Type is the authentication method: "none" (default), "token", or "ssh".
	Type string `yaml:"type,omitempty"`
	// TokenEnv is the name of the environment variable holding a personal access token.
	// Used when Type is "token".
	TokenEnv string `yaml:"tokenEnv,omitempty"`
	// SSHKeyPath is the path to an SSH private key file.
	// Used when Type is "ssh".
	SSHKeyPath string `yaml:"sshKeyPath,omitempty"`
	// SSHKeyPassphraseEnv is the name of the environment variable holding the
	// passphrase for the SSH key. Optional; used when Type is "ssh".
	SSHKeyPassphraseEnv string `yaml:"sshKeyPassphraseEnv,omitempty"`
}

// SchemaRepoVersion maps a Git ref to a NOCFoundry vendor/platform/version triple.
type SchemaRepoVersion struct {
	// Ref is the Git reference to check out (branch name, tag, or commit SHA).
	Ref string `yaml:"ref" validate:"required"`
	// Vendor is the vendor label for the schema key (e.g., "nokia").
	Vendor string `yaml:"vendor" validate:"required"`
	// Platform is the platform label (e.g., "srlinux").
	Platform string `yaml:"platform" validate:"required"`
	// Version is the NOCFoundry version label (e.g., "v24.10").
	Version string `yaml:"version" validate:"required"`
	// Path is the sub-directory within the repo where .yang files reside.
	// Defaults to "." (repo root).
	Path string `yaml:"path,omitempty"`
	// OpsFile is the path to a local nocfoundry-ops.yaml that defines operation
	// mappings for this version.  When set it takes priority over any sidecar
	// found in the cloned repository and over prebuilt defaults.
	OpsFile string `yaml:"opsFile,omitempty"`
}

// ValidateSchemaRepoConfig checks that a SchemaRepoConfig is well-formed.
func ValidateSchemaRepoConfig(cfg *SchemaRepoConfig) error {
	if strings.TrimSpace(cfg.URL) == "" {
		return fmt.Errorf("schemaRepo %q: url is required", cfg.Name)
	}
	if len(cfg.Versions) == 0 {
		return fmt.Errorf("schemaRepo %q: at least one version entry is required", cfg.Name)
	}

	authType := strings.ToLower(strings.TrimSpace(cfg.Auth.Type))
	if authType == "" {
		authType = "none"
	}
	switch authType {
	case "none":
	case "token":
		if strings.TrimSpace(cfg.Auth.TokenEnv) == "" {
			return fmt.Errorf("schemaRepo %q: auth.tokenEnv is required when auth.type is 'token'", cfg.Name)
		}
	case "ssh":
		if strings.TrimSpace(cfg.Auth.SSHKeyPath) == "" {
			return fmt.Errorf("schemaRepo %q: auth.sshKeyPath is required when auth.type is 'ssh'", cfg.Name)
		}
	default:
		return fmt.Errorf("schemaRepo %q: auth.type must be 'none', 'token', or 'ssh'; got %q", cfg.Name, cfg.Auth.Type)
	}

	for i, v := range cfg.Versions {
		if strings.TrimSpace(v.Ref) == "" {
			return fmt.Errorf("schemaRepo %q: versions[%d].ref is required", cfg.Name, i)
		}
		if strings.TrimSpace(v.Vendor) == "" {
			return fmt.Errorf("schemaRepo %q: versions[%d].vendor is required", cfg.Name, i)
		}
		if strings.TrimSpace(v.Platform) == "" {
			return fmt.Errorf("schemaRepo %q: versions[%d].platform is required", cfg.Name, i)
		}
		if strings.TrimSpace(v.Version) == "" {
			return fmt.Errorf("schemaRepo %q: versions[%d].version is required", cfg.Name, i)
		}
	}
	return nil
}

// UnmarshalYAMLSchemaRepoConfig decodes a schemaRepos YAML document into a SchemaRepoConfig.
func UnmarshalYAMLSchemaRepoConfig(name string, r map[string]any) (*SchemaRepoConfig, error) {
	dec, err := util.NewStrictDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder: %w", err)
	}
	var cfg SchemaRepoConfig
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("error decoding schemaRepo %q: %w", name, err)
	}
	cfg.Name = name
	if err := ValidateSchemaRepoConfig(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// MarshalSchemaRepoConfig serializes a SchemaRepoConfig to YAML bytes.
func MarshalSchemaRepoConfig(cfg *SchemaRepoConfig) ([]byte, error) {
	return yaml.Marshal(cfg)
}
