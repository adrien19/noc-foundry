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
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/adrien19/noc-foundry/internal/log"
	"github.com/adrien19/noc-foundry/internal/prebuiltconfigs"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/telemetry"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/spf13/cobra"
)

type IOStreams struct {
	In     io.Reader
	Out    io.Writer
	ErrOut io.Writer
}

// NOCFoundryOptions holds dependencies shared by all commands.
type NOCFoundryOptions struct {
	IOStreams               IOStreams
	Logger                  log.Logger
	Cfg                     server.ServerConfig
	ToolsFile               string
	ToolsFiles              []string
	ToolsFolder             string
	ServerConfigFile        string
	ValidationConfigFile    string
	ValidationFlagValues    server.ValidationRunConfig
	ValidationFlagOverrides validationFlagOverrides
	PrebuiltConfigs         []string
}

type validationFlagOverrides struct {
	ExecutionBackend      bool
	StoreBackend          bool
	SQLitePath            bool
	DurableTaskSQLitePath bool
	MaxConcurrentRuns     bool
	MaxConcurrentSteps    bool
	ResultRetention       bool
	EventRetention        bool
}

// Option defines a function that modifies the NOCFoundryOptions struct.
type Option func(*NOCFoundryOptions)

// NewNOCFoundryOptions creates a new instance with defaults, then applies any
// provided options.
func NewNOCFoundryOptions(opts ...Option) *NOCFoundryOptions {
	o := &NOCFoundryOptions{
		IOStreams: IOStreams{
			In:     os.Stdin,
			Out:    os.Stdout,
			ErrOut: os.Stderr,
		},
	}

	for _, opt := range opts {
		opt(o)
	}
	return o
}

// Apply allows you to update an EXISTING NOCFoundryOptions instance.
// This is useful for "late binding".
func (o *NOCFoundryOptions) Apply(opts ...Option) {
	for _, opt := range opts {
		opt(o)
	}
}

// WithIOStreams updates the IO streams.
func WithIOStreams(out, err io.Writer) Option {
	return func(o *NOCFoundryOptions) {
		o.IOStreams.Out = out
		o.IOStreams.ErrOut = err
	}
}

// Setup create logger and telemetry instrumentations.
func (opts *NOCFoundryOptions) Setup(ctx context.Context) (context.Context, func(context.Context) error, error) {
	// If stdio, set logger's out stream (usually DEBUG and INFO logs) to
	// errStream
	loggerOut := opts.IOStreams.Out
	if opts.Cfg.Stdio {
		loggerOut = opts.IOStreams.ErrOut
	}

	// Handle logger separately from config
	logger, err := log.NewLogger(opts.Cfg.LoggingFormat.String(), opts.Cfg.LogLevel.String(), loggerOut, opts.IOStreams.ErrOut)
	if err != nil {
		return ctx, nil, fmt.Errorf("unable to initialize logger: %w", err)
	}

	ctx = util.WithLogger(ctx, logger)
	opts.Logger = logger

	// Set up OpenTelemetry
	otelShutdown, err := telemetry.SetupOTel(ctx, opts.Cfg.Version, opts.Cfg.TelemetryOTLP, opts.Cfg.TelemetryServiceName)
	if err != nil {
		errMsg := fmt.Errorf("error setting up OpenTelemetry: %w", err)
		logger.ErrorContext(ctx, errMsg.Error())
		return ctx, nil, errMsg
	}

	shutdownFunc := func(ctx context.Context) error {
		err := otelShutdown(ctx)
		if err != nil {
			errMsg := fmt.Errorf("error shutting down OpenTelemetry: %w", err)
			logger.ErrorContext(ctx, errMsg.Error())
			return err
		}
		return nil
	}

	instrumentation, err := telemetry.CreateTelemetryInstrumentation(opts.Cfg.Version)
	if err != nil {
		errMsg := fmt.Errorf("unable to create telemetry instrumentation: %w", err)
		logger.ErrorContext(ctx, errMsg.Error())
		return ctx, shutdownFunc, errMsg
	}

	ctx = util.WithInstrumentation(ctx, instrumentation)

	return ctx, shutdownFunc, nil
}

// LoadConfig checks and merge files that should be loaded into the server
func (opts *NOCFoundryOptions) LoadConfig(ctx context.Context, parser *ToolsFileParser) (bool, error) {
	// Determine if Custom Files should be loaded
	// Check for explicit custom flags
	isCustomConfigured := opts.ToolsFile != "" || len(opts.ToolsFiles) > 0 || opts.ToolsFolder != ""

	// Determine if default 'tools.yaml' should be used (No prebuilt AND No custom flags)
	useDefaultToolsFile := len(opts.PrebuiltConfigs) == 0 && !isCustomConfigured

	if useDefaultToolsFile {
		opts.ToolsFile = "tools.yaml"
		isCustomConfigured = true
	}

	logger, loggerErr := util.LoggerFromContext(ctx)

	var allToolsFiles []ToolsFile

	// Load Prebuilt Configuration

	if len(opts.PrebuiltConfigs) > 0 {
		slices.Sort(opts.PrebuiltConfigs)
		sourcesList := strings.Join(opts.PrebuiltConfigs, ", ")
		logMsg := fmt.Sprintf("Using prebuilt tool configurations for: %s", sourcesList)
		if loggerErr == nil {
			logger.InfoContext(ctx, logMsg)
		}

		for _, configName := range opts.PrebuiltConfigs {
			buf, err := prebuiltconfigs.Get(configName)
			if err != nil {
				if loggerErr == nil {
					logger.ErrorContext(ctx, err.Error())
				}
				return isCustomConfigured, err
			}

			// Parse into ToolsFile struct
			parsed, err := parser.ParseToolsFile(ctx, buf)
			if err != nil {
				errMsg := fmt.Errorf("unable to parse prebuilt tool configuration for '%s': %w", configName, err)
				if loggerErr == nil {
					logger.ErrorContext(ctx, errMsg.Error())
				}
				return isCustomConfigured, errMsg
			}
			allToolsFiles = append(allToolsFiles, parsed)
		}
	}

	// Load Custom Configurations
	if isCustomConfigured {
		// Enforce exclusivity among custom flags (tools-file vs tools-files vs tools-folder)
		if (opts.ToolsFile != "" && len(opts.ToolsFiles) > 0) ||
			(opts.ToolsFile != "" && opts.ToolsFolder != "") ||
			(len(opts.ToolsFiles) > 0 && opts.ToolsFolder != "") {
			errMsg := fmt.Errorf("--tools-file, --tools-files, and --tools-folder flags cannot be used simultaneously")
			if loggerErr == nil {
				logger.ErrorContext(ctx, errMsg.Error())
			}
			return isCustomConfigured, errMsg
		}

		var customTools ToolsFile
		var err error

		if len(opts.ToolsFiles) > 0 {
			// Use tools-files
			if loggerErr == nil {
				logger.InfoContext(ctx, fmt.Sprintf("Loading and merging %d tool configuration files", len(opts.ToolsFiles)))
			}
			customTools, err = parser.LoadAndMergeToolsFiles(ctx, opts.ToolsFiles)
		} else if opts.ToolsFolder != "" {
			// Use tools-folder
			if loggerErr == nil {
				logger.InfoContext(ctx, fmt.Sprintf("Loading and merging all YAML files from directory: %s", opts.ToolsFolder))
			}
			customTools, err = parser.LoadAndMergeToolsFolder(ctx, opts.ToolsFolder)
		} else {
			// Use single file (tools-file or default `tools.yaml`)
			buf, readFileErr := os.ReadFile(opts.ToolsFile)
			if readFileErr != nil {
				errMsg := fmt.Errorf("unable to read tool file at %q: %w", opts.ToolsFile, readFileErr)
				if loggerErr == nil {
					logger.ErrorContext(ctx, errMsg.Error())
				}
				return isCustomConfigured, errMsg
			}
			customTools, err = parser.ParseToolsFile(ctx, buf)
			if err != nil {
				err = fmt.Errorf("unable to parse tool file at %q: %w", opts.ToolsFile, err)
			}
		}

		if err != nil {
			if loggerErr == nil {
				logger.ErrorContext(ctx, err.Error())
			}
			return isCustomConfigured, err
		}
		allToolsFiles = append(allToolsFiles, customTools)
	}

	// Modify version string based on loaded configurations
	if len(opts.PrebuiltConfigs) > 0 {
		tag := "prebuilt"
		if isCustomConfigured {
			tag = "custom"
		}
		// prebuiltConfigs is already sorted above
		for _, configName := range opts.PrebuiltConfigs {
			opts.Cfg.Version += fmt.Sprintf("+%s.%s", tag, configName)
		}
	}

	// Merge Everything
	// This will error if custom tools collide with prebuilt tools
	finalToolsFile, err := mergeToolsFiles(allToolsFiles...)
	if err != nil {
		if loggerErr == nil {
			logger.ErrorContext(ctx, err.Error())
		}
		return isCustomConfigured, err
	}

	opts.Cfg.SourceConfigs = finalToolsFile.Sources
	opts.Cfg.AuthServiceConfigs = finalToolsFile.AuthServices
	opts.Cfg.EmbeddingModelConfigs = finalToolsFile.EmbeddingModels
	opts.Cfg.ToolConfigs = finalToolsFile.Tools
	opts.Cfg.ToolsetConfigs = finalToolsFile.Toolsets
	opts.Cfg.PromptConfigs = finalToolsFile.Prompts
	opts.Cfg.PromptsetConfigs = finalToolsFile.Promptsets
	opts.Cfg.DeviceGroupConfigs = finalToolsFile.DeviceGroups

	if err := opts.LoadServerRuntimeConfig(ctx, &ServerConfigParser{}); err != nil {
		if loggerErr == nil {
			logger.ErrorContext(ctx, err.Error())
		}
		return isCustomConfigured, err
	}

	if err := opts.LoadValidationRuntimeConfig(ctx, &ValidationConfigParser{}); err != nil {
		if loggerErr == nil {
			logger.ErrorContext(ctx, err.Error())
		}
		return isCustomConfigured, err
	}

	return isCustomConfigured, nil
}

func (opts *NOCFoundryOptions) LoadServerRuntimeConfig(ctx context.Context, parser *ServerConfigParser) error {
	if opts.ServerConfigFile == "" {
		return nil
	}
	cfg, err := parser.LoadServerConfigFile(ctx, opts.ServerConfigFile)
	if err != nil {
		return err
	}
	opts.Cfg.Auth = cfg
	return nil
}

func (opts *NOCFoundryOptions) CaptureValidationFlagOverrides(cmd *cobra.Command) {
	if f := cmd.Flag("validation-backend"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.ExecutionBackend = true
	}
	if f := cmd.Flag("validation-store"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.StoreBackend = true
	}
	if f := cmd.Flag("validation-db"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.SQLitePath = true
	}
	if f := cmd.Flag("validation-taskhub-db"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.DurableTaskSQLitePath = true
	}
	if f := cmd.Flag("validation-max-runs"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.MaxConcurrentRuns = true
	}
	if f := cmd.Flag("validation-max-steps"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.MaxConcurrentSteps = true
	}
	if f := cmd.Flag("validation-result-ttl"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.ResultRetention = true
	}
	if f := cmd.Flag("validation-event-ttl"); f != nil && f.Changed {
		opts.ValidationFlagOverrides.EventRetention = true
	}
	opts.ValidationFlagValues = opts.Cfg.ValidationRuns
}

func (opts *NOCFoundryOptions) LoadValidationRuntimeConfig(ctx context.Context, parser *ValidationConfigParser) error {
	explicit := opts.ValidationConfigFile != "" || opts.hasValidationFlagOverrides()
	if !explicit {
		return nil
	}

	var cfg server.ValidationRunConfig
	if opts.ValidationConfigFile != "" {
		loaded, err := parser.LoadValidationConfigFile(ctx, opts.ValidationConfigFile)
		if err != nil {
			return err
		}
		cfg = loaded
	}
	if opts.ValidationFlagOverrides.ExecutionBackend {
		cfg.ExecutionBackend = opts.ValidationFlagValues.ExecutionBackend
	}
	if opts.ValidationFlagOverrides.StoreBackend {
		cfg.StoreBackend = opts.ValidationFlagValues.StoreBackend
	}
	if opts.ValidationFlagOverrides.SQLitePath {
		cfg.SQLitePath = opts.ValidationFlagValues.SQLitePath
	}
	if opts.ValidationFlagOverrides.DurableTaskSQLitePath {
		cfg.DurableTaskSQLitePath = opts.ValidationFlagValues.DurableTaskSQLitePath
	}
	if opts.ValidationFlagOverrides.MaxConcurrentRuns {
		cfg.MaxConcurrentRuns = opts.ValidationFlagValues.MaxConcurrentRuns
	}
	if opts.ValidationFlagOverrides.MaxConcurrentSteps {
		cfg.MaxConcurrentSteps = opts.ValidationFlagValues.MaxConcurrentSteps
	}
	if opts.ValidationFlagOverrides.ResultRetention {
		cfg.ResultRetention = opts.ValidationFlagValues.ResultRetention
	}
	if opts.ValidationFlagOverrides.EventRetention {
		cfg.EventRetention = opts.ValidationFlagValues.EventRetention
	}
	if err := server.ValidateValidationRunConfig(cfg); err != nil {
		return err
	}
	opts.Cfg.ValidationRuns = cfg
	return nil
}

func (opts *NOCFoundryOptions) hasValidationFlagOverrides() bool {
	return opts.ValidationFlagOverrides.ExecutionBackend ||
		opts.ValidationFlagOverrides.StoreBackend ||
		opts.ValidationFlagOverrides.SQLitePath ||
		opts.ValidationFlagOverrides.DurableTaskSQLitePath ||
		opts.ValidationFlagOverrides.MaxConcurrentRuns ||
		opts.ValidationFlagOverrides.MaxConcurrentSteps ||
		opts.ValidationFlagOverrides.ResultRetention ||
		opts.ValidationFlagOverrides.EventRetention
}
