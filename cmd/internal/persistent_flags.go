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
	"fmt"
	"strings"

	"github.com/adrien19/noc-foundry/internal/prebuiltconfigs"
	"github.com/spf13/cobra"
)

// PersistentFlags sets up flags that are available for all commands and
// subcommands
// It is also used to set up persistent flags during subcommand unit tests
func PersistentFlags(parentCmd *cobra.Command, opts *NOCFoundryOptions) {
	persistentFlags := parentCmd.PersistentFlags()

	persistentFlags.StringVar(&opts.ToolsFile, "tools-file", "", "File path specifying the tool configuration. Cannot be used with --tools-files, or --tools-folder.")
	persistentFlags.StringSliceVar(&opts.ToolsFiles, "tools-files", []string{}, "Multiple file paths specifying tool configurations. Files will be merged. Cannot be used with --tools-file, or --tools-folder.")
	persistentFlags.StringVar(&opts.ToolsFolder, "tools-folder", "", "Directory path containing YAML tool configuration files. All .yaml and .yml files in the directory will be loaded and merged. Cannot be used with --tools-file, or --tools-files.")
	persistentFlags.StringVar(&opts.ServerConfigFile, "server-config", "", "YAML file for server-scoped runtime settings such as HTTP surface endpoint authentication policy.")
	persistentFlags.StringVar(&opts.ValidationConfigFile, "validation-config", "", "YAML file for server-scoped validation runtime settings such as backend, store, SQLite paths, concurrency, and retention.")
	persistentFlags.StringVar(&opts.Cfg.ValidationRuns.ExecutionBackend, "validation-backend", "", "Validation runtime execution backend. Allowed: 'local' or 'durabletask'.")
	persistentFlags.StringVar(&opts.Cfg.ValidationRuns.StoreBackend, "validation-store", "", "Validation runtime state store. Allowed: 'memory' or 'sqlite'.")
	persistentFlags.StringVar(&opts.Cfg.ValidationRuns.SQLitePath, "validation-db", "", "SQLite database path for NOCFoundry validation run metadata, events, and results.")
	persistentFlags.StringVar(&opts.Cfg.ValidationRuns.DurableTaskSQLitePath, "validation-taskhub-db", "", "SQLite database path for durabletask orchestration state. Required when --validation-backend=durabletask.")
	persistentFlags.IntVar(&opts.Cfg.ValidationRuns.MaxConcurrentRuns, "validation-max-runs", 0, "Maximum number of concurrently executing validation runs.")
	persistentFlags.IntVar(&opts.Cfg.ValidationRuns.MaxConcurrentSteps, "validation-max-steps", 0, "Maximum number of concurrent validation step workers.")
	persistentFlags.DurationVar(&opts.Cfg.ValidationRuns.ResultRetention, "validation-result-ttl", 0, "Retention period for completed validation results.")
	persistentFlags.DurationVar(&opts.Cfg.ValidationRuns.EventRetention, "validation-event-ttl", 0, "Retention period for validation run events.")
	persistentFlags.Var(&opts.Cfg.LogLevel, "log-level", "Specify the minimum level logged. Allowed: 'DEBUG', 'INFO', 'WARN', 'ERROR'.")
	persistentFlags.Var(&opts.Cfg.LoggingFormat, "logging-format", "Specify logging format to use. Allowed: 'standard' or 'JSON'.")
	persistentFlags.StringVar(&opts.Cfg.TelemetryOTLP, "telemetry-otlp", "", "Enable exporting using OpenTelemetry Protocol (OTLP) to the specified endpoint (e.g. 'http://127.0.0.1:4318')")
	persistentFlags.StringVar(&opts.Cfg.TelemetryServiceName, "telemetry-service-name", "nocfoundry", "Sets the value of the service.name resource attribute for telemetry data.")
	// Fetch prebuilt tools sources to customize the help description
	prebuiltHelp := fmt.Sprintf(
		"Use a bundled prebuilt tool catalog for generic operational capabilities. Allowed: '%s'. Can be specified multiple times.",
		strings.Join(prebuiltconfigs.GetPrebuiltSources(), "', '"),
	)
	persistentFlags.StringSliceVar(&opts.PrebuiltConfigs, "prebuilt", []string{}, prebuiltHelp)
	persistentFlags.StringSliceVar(&opts.Cfg.UserAgentMetadata, "user-agent-metadata", []string{}, "Appends additional metadata to the User-Agent.")
	persistentFlags.StringVar(&opts.Cfg.SchemaDir, "schema-dir", "", "Directory containing vendor YANG models. Structure: <dir>/<vendor>/<platform>/<version>/*.yang")
	persistentFlags.StringVar(&opts.Cfg.SchemaCacheDir, "schema-cache-dir", "", "Directory used to cache cloned YANG Git repositories. Default: ~/.cache/nocfoundry/yang-repos/")
}
