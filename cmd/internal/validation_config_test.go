package internal

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/server"
)

func TestValidationConfigParser(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		parser := &ValidationConfigParser{}
		cfg, err := parser.ParseValidationConfig(context.Background(), []byte(`
executionBackend: durabletask
storeBackend: sqlite
sqlitePath: /tmp/runs.sqlite
durableTaskSQLitePath: /tmp/taskhub.sqlite
maxConcurrentRuns: 4
maxConcurrentSteps: 8
resultRetention: 24h
eventRetention: 12h
`))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if cfg.ExecutionBackend != "durabletask" || cfg.StoreBackend != "sqlite" || cfg.SQLitePath != "/tmp/runs.sqlite" {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})

	t.Run("unknown field", func(t *testing.T) {
		parser := &ValidationConfigParser{}
		_, err := parser.ParseValidationConfig(context.Background(), []byte(`
executionBackend: local
unknownField: true
`))
		if err == nil {
			t.Fatalf("expected strict unmarshal error")
		}
	})

	t.Run("env expansion", func(t *testing.T) {
		t.Setenv("VALIDATION_DB", "/tmp/env-runs.sqlite")
		parser := &ValidationConfigParser{}
		cfg, err := parser.ParseValidationConfig(context.Background(), []byte(`
storeBackend: sqlite
sqlitePath: ${VALIDATION_DB}
`))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if cfg.SQLitePath != "/tmp/env-runs.sqlite" {
			t.Fatalf("unexpected config: %+v", cfg)
		}
	})
}

func TestLoadValidationRuntimeConfigMergePrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	toolsFilePath := filepath.Join(tmpDir, "tools.yaml")
	if err := os.WriteFile(toolsFilePath, []byte(""), 0o644); err != nil {
		t.Fatalf("write tools file: %v", err)
	}
	validationFilePath := filepath.Join(tmpDir, "validation.yaml")
	if err := os.WriteFile(validationFilePath, []byte(`
executionBackend: durabletask
storeBackend: sqlite
sqlitePath: /tmp/from-file-runs.sqlite
durableTaskSQLitePath: /tmp/from-file-taskhub.sqlite
maxConcurrentRuns: 4
resultRetention: 24h
`), 0o644); err != nil {
		t.Fatalf("write validation file: %v", err)
	}

	opts := NewNOCFoundryOptions()
	opts.ToolsFile = toolsFilePath
	opts.ValidationConfigFile = validationFilePath
	opts.ValidationFlagValues = server.ValidationRunConfig{
		ExecutionBackend:      "durabletask",
		StoreBackend:          "sqlite",
		SQLitePath:            "/tmp/from-flag-runs.sqlite",
		DurableTaskSQLitePath: "/tmp/from-flag-taskhub.sqlite",
		MaxConcurrentRuns:     9,
		EventRetention:        time.Hour,
	}
	opts.ValidationFlagOverrides = validationFlagOverrides{
		SQLitePath:            true,
		DurableTaskSQLitePath: true,
		MaxConcurrentRuns:     true,
		EventRetention:        true,
	}

	_, err := opts.LoadConfig(context.Background(), &ToolsFileParser{})
	if err != nil {
		t.Fatalf("load config failed: %v", err)
	}

	got := opts.Cfg.ValidationRuns
	if got.ExecutionBackend != "durabletask" || got.StoreBackend != "sqlite" {
		t.Fatalf("file config not loaded: %+v", got)
	}
	if got.SQLitePath != "/tmp/from-flag-runs.sqlite" || got.DurableTaskSQLitePath != "/tmp/from-flag-taskhub.sqlite" {
		t.Fatalf("flag override not applied: %+v", got)
	}
	if got.MaxConcurrentRuns != 9 || got.ResultRetention != 24*time.Hour || got.EventRetention != time.Hour {
		t.Fatalf("unexpected merged config: %+v", got)
	}
}

func TestLoadValidationRuntimeConfigValidation(t *testing.T) {
	tmpDir := t.TempDir()
	toolsFilePath := filepath.Join(tmpDir, "tools.yaml")
	if err := os.WriteFile(toolsFilePath, []byte(""), 0o644); err != nil {
		t.Fatalf("write tools file: %v", err)
	}

	opts := NewNOCFoundryOptions()
	opts.ToolsFile = toolsFilePath
	opts.ValidationFlagValues = server.ValidationRunConfig{
		ExecutionBackend: "durabletask",
		StoreBackend:     "memory",
	}
	opts.ValidationFlagOverrides = validationFlagOverrides{
		ExecutionBackend: true,
		StoreBackend:     true,
	}

	_, err := opts.LoadConfig(context.Background(), &ToolsFileParser{})
	if err == nil {
		t.Fatalf("expected validation runtime config error")
	}
}
