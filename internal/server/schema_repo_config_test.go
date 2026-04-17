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
	"testing"
)

func TestValidateSchemaRepoConfig_Valid(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "nokia-yang",
		URL:  "https://github.com/nokia/srlinux-yang-models",
		Auth: SchemaRepoAuth{Type: "none"},
		Versions: []SchemaRepoVersion{
			{Ref: "v24.10", Vendor: "nokia", Platform: "srlinux", Version: "v24.10"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateSchemaRepoConfig_MissingURL(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "bad",
		URL:  "",
		Versions: []SchemaRepoVersion{
			{Ref: "main", Vendor: "v", Platform: "p", Version: "1"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err == nil {
		t.Fatal("expected error for missing URL")
	}
}

func TestValidateSchemaRepoConfig_NoVersions(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name:     "bad",
		URL:      "https://example.com/repo",
		Versions: nil,
	}
	if err := ValidateSchemaRepoConfig(cfg); err == nil {
		t.Fatal("expected error for empty versions")
	}
}

func TestValidateSchemaRepoConfig_TokenAuthMissingEnv(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "bad",
		URL:  "https://example.com/repo",
		Auth: SchemaRepoAuth{Type: "token"},
		Versions: []SchemaRepoVersion{
			{Ref: "main", Vendor: "v", Platform: "p", Version: "1"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err == nil {
		t.Fatal("expected error for token auth missing tokenEnv")
	}
}

func TestValidateSchemaRepoConfig_SSHAuthMissingKey(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "bad",
		URL:  "https://example.com/repo",
		Auth: SchemaRepoAuth{Type: "ssh"},
		Versions: []SchemaRepoVersion{
			{Ref: "main", Vendor: "v", Platform: "p", Version: "1"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err == nil {
		t.Fatal("expected error for ssh auth missing sshKeyPath")
	}
}

func TestValidateSchemaRepoConfig_InvalidAuthType(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "bad",
		URL:  "https://example.com/repo",
		Auth: SchemaRepoAuth{Type: "magic"},
		Versions: []SchemaRepoVersion{
			{Ref: "main", Vendor: "v", Platform: "p", Version: "1"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err == nil {
		t.Fatal("expected error for invalid auth type")
	}
}

func TestValidateSchemaRepoConfig_VersionMissingRef(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "bad",
		URL:  "https://example.com/repo",
		Versions: []SchemaRepoVersion{
			{Ref: "", Vendor: "v", Platform: "p", Version: "1"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err == nil {
		t.Fatal("expected error for missing ref")
	}
}

func TestValidateSchemaRepoConfig_ValidTokenAuth(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "private-repo",
		URL:  "https://github.com/corp/yang-models",
		Auth: SchemaRepoAuth{Type: "token", TokenEnv: "GH_TOKEN"},
		Versions: []SchemaRepoVersion{
			{Ref: "main", Vendor: "juniper", Platform: "mx", Version: "v22.4"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestValidateSchemaRepoConfig_ValidSSHAuth(t *testing.T) {
	cfg := &SchemaRepoConfig{
		Name: "ssh-repo",
		URL:  "git@github.com:corp/yang-models.git",
		Auth: SchemaRepoAuth{Type: "ssh", SSHKeyPath: "~/.ssh/id_ed25519"},
		Versions: []SchemaRepoVersion{
			{Ref: "main", Vendor: "juniper", Platform: "mx", Version: "v22.4"},
		},
	}
	if err := ValidateSchemaRepoConfig(cfg); err != nil {
		t.Fatalf("expected valid config, got error: %v", err)
	}
}

func TestUnmarshalYAMLSchemaRepoConfig(t *testing.T) {
	raw := map[string]any{
		"name": "test-repo",
		"url":  "https://github.com/test/repo",
		"auth": map[string]any{
			"type": "none",
		},
		"versions": []any{
			map[string]any{
				"ref":      "v1.0",
				"vendor":   "acme",
				"platform": "router",
				"version":  "v1.0",
				"path":     "yang",
			},
		},
	}
	cfg, err := UnmarshalYAMLSchemaRepoConfig("test-repo", raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Name != "test-repo" {
		t.Errorf("expected name %q, got %q", "test-repo", cfg.Name)
	}
	if cfg.URL != "https://github.com/test/repo" {
		t.Errorf("expected url %q, got %q", "https://github.com/test/repo", cfg.URL)
	}
	if len(cfg.Versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(cfg.Versions))
	}
	if cfg.Versions[0].Path != "yang" {
		t.Errorf("expected path %q, got %q", "yang", cfg.Versions[0].Path)
	}
}
