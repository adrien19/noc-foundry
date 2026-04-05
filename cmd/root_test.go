// Copyright 2024 Google LLC
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

package cmd

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/adrien19/noc-foundry/cmd/internal"
	"github.com/adrien19/noc-foundry/internal/log"
	"github.com/adrien19/noc-foundry/internal/prebuiltconfigs"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/telemetry"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/spf13/cobra"
)

func withDefaults(c server.ServerConfig) server.ServerConfig {
	data, _ := os.ReadFile("version.txt")
	version := strings.TrimSpace(string(data)) // Preserving 'data', new var for clarity
	c.Version = version + "+" + strings.Join([]string{"dev", runtime.GOOS, runtime.GOARCH}, ".")

	if c.Address == "" {
		c.Address = "127.0.0.1"
	}
	if c.Port == 0 {
		c.Port = 5000
	}
	if c.TelemetryServiceName == "" {
		c.TelemetryServiceName = "nocfoundry"
	}
	if c.AllowedOrigins == nil {
		c.AllowedOrigins = []string{"*"}
	}
	if c.AllowedHosts == nil {
		c.AllowedHosts = []string{"*"}
	}
	if c.UserAgentMetadata == nil {
		c.UserAgentMetadata = []string{}
	}
	return c
}

func invokeCommand(args []string) (*cobra.Command, *internal.NOCFoundryOptions, string, error) {
	buf := new(bytes.Buffer)
	opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
	c := NewCommand(opts)

	// Keep the test output quiet
	c.SilenceUsage = true
	c.SilenceErrors = true

	// Capture output
	c.SetOut(buf)
	c.SetErr(buf)
	c.SetArgs(args)

	// Disable execute behavior
	c.RunE = func(*cobra.Command, []string) error {
		return nil
	}

	err := c.Execute()

	return c, opts, buf.String(), err
}

// invokeCommandWithContext executes the command with a context and returns the captured output.
func invokeCommandWithContext(ctx context.Context, args []string) (*cobra.Command, *internal.NOCFoundryOptions, string, error) {
	buf := new(bytes.Buffer)
	opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
	c := NewCommand(opts)

	// Capture output using a buffer
	c.SetArgs(args)
	c.SilenceUsage = true
	c.SilenceErrors = true
	c.SetContext(ctx)

	err := c.Execute()
	return c, opts, buf.String(), err
}

func TestVersion(t *testing.T) {
	data, err := os.ReadFile("version.txt")
	if err != nil {
		t.Fatalf("failed to read version.txt: %v", err)
	}
	want := strings.TrimSpace(string(data))

	_, _, got, err := invokeCommand([]string{"--version"})
	if err != nil {
		t.Fatalf("error invoking command: %s", err)
	}

	if !strings.Contains(got, want) {
		t.Errorf("cli did not return correct version: want %q, got %q", want, got)
	}
}

func TestServerConfigFlags(t *testing.T) {
	tcs := []struct {
		desc string
		args []string
		want server.ServerConfig
	}{
		{
			desc: "default values",
			args: []string{},
			want: withDefaults(server.ServerConfig{}),
		},
		{
			desc: "address short",
			args: []string{"-a", "127.0.1.1"},
			want: withDefaults(server.ServerConfig{
				Address: "127.0.1.1",
			}),
		},
		{
			desc: "address long",
			args: []string{"--address", "0.0.0.0"},
			want: withDefaults(server.ServerConfig{
				Address: "0.0.0.0",
			}),
		},
		{
			desc: "port short",
			args: []string{"-p", "5052"},
			want: withDefaults(server.ServerConfig{
				Port: 5052,
			}),
		},
		{
			desc: "port long",
			args: []string{"--port", "5050"},
			want: withDefaults(server.ServerConfig{
				Port: 5050,
			}),
		},
		{
			desc: "logging format",
			args: []string{"--logging-format", "JSON"},
			want: withDefaults(server.ServerConfig{
				LoggingFormat: "JSON",
			}),
		},
		{
			desc: "debug logs",
			args: []string{"--log-level", "WARN"},
			want: withDefaults(server.ServerConfig{
				LogLevel: "WARN",
			}),
		},
		{
			desc: "telemetry otlp",
			args: []string{"--telemetry-otlp", "http://127.0.0.1:4553"},
			want: withDefaults(server.ServerConfig{
				TelemetryOTLP: "http://127.0.0.1:4553",
			}),
		},
		{
			desc: "telemetry service name",
			args: []string{"--telemetry-service-name", "nocfoundry-custom"},
			want: withDefaults(server.ServerConfig{
				TelemetryServiceName: "nocfoundry-custom",
			}),
		},
		{
			desc: "stdio",
			args: []string{"--stdio"},
			want: withDefaults(server.ServerConfig{
				Stdio: true,
			}),
		},
		{
			desc: "disable reload",
			args: []string{"--disable-reload"},
			want: withDefaults(server.ServerConfig{
				DisableReload: true,
			}),
		},
		{
			desc: "allowed origin",
			args: []string{"--allowed-origins", "http://foo.com,http://bar.com"},
			want: withDefaults(server.ServerConfig{
				AllowedOrigins: []string{"http://foo.com", "http://bar.com"},
			}),
		},
		{
			desc: "allowed hosts",
			args: []string{"--allowed-hosts", "http://foo.com,http://bar.com"},
			want: withDefaults(server.ServerConfig{
				AllowedHosts: []string{"http://foo.com", "http://bar.com"},
			}),
		},
		{
			desc: "user agent metadata",
			args: []string{"--user-agent-metadata", "foo,bar"},
			want: withDefaults(server.ServerConfig{
				UserAgentMetadata: []string{"foo", "bar"},
			}),
		},
		{
			desc: "validation runtime flags",
			args: []string{
				"--validation-backend", "durabletask",
				"--validation-store", "sqlite",
				"--validation-db", "/tmp/validation-runs.sqlite",
				"--validation-taskhub-db", "/tmp/validation-taskhub.sqlite",
				"--validation-max-runs", "4",
				"--validation-max-steps", "8",
				"--validation-result-ttl", "24h",
				"--validation-event-ttl", "12h",
			},
			want: withDefaults(server.ServerConfig{
				ValidationRuns: server.ValidationRunConfig{
					ExecutionBackend:      "durabletask",
					StoreBackend:          "sqlite",
					SQLitePath:            "/tmp/validation-runs.sqlite",
					DurableTaskSQLitePath: "/tmp/validation-taskhub.sqlite",
					MaxConcurrentRuns:     4,
					MaxConcurrentSteps:    8,
					ResultRetention:       24 * time.Hour,
					EventRetention:        12 * time.Hour,
				},
			}),
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, opts, _, err := invokeCommand(tc.args)
			if err != nil {
				t.Fatalf("unexpected error invoking command: %s", err)
			}

			if !cmp.Equal(opts.Cfg, tc.want) {
				t.Fatalf("got %v, want %v", opts.Cfg, tc.want)
			}
		})
	}
}

func TestValidationConfigFlag(t *testing.T) {
	_, opts, _, err := invokeCommand([]string{"--validation-config", "validation-runtime.yaml"})
	if err != nil {
		t.Fatalf("unexpected error invoking command: %s", err)
	}
	if opts.ValidationConfigFile != "validation-runtime.yaml" {
		t.Fatalf("got %q, want %q", opts.ValidationConfigFile, "validation-runtime.yaml")
	}
}

func TestServerConfigFlag(t *testing.T) {
	_, opts, _, err := invokeCommand([]string{"--server-config", "server-runtime.yaml"})
	if err != nil {
		t.Fatalf("unexpected error invoking command: %s", err)
	}
	if opts.ServerConfigFile != "server-runtime.yaml" {
		t.Fatalf("got %q, want %q", opts.ServerConfigFile, "server-runtime.yaml")
	}
}

func TestToolFileFlag(t *testing.T) {
	tcs := []struct {
		desc string
		args []string
		want string
	}{
		{
			desc: "default value",
			args: []string{},
			want: "",
		},
		{
			desc: "foo file",
			args: []string{"--tools-file", "foo.yaml"},
			want: "foo.yaml",
		},
		{
			desc: "address long",
			args: []string{"--tools-file", "bar.yaml"},
			want: "bar.yaml",
		},
		{
			desc: "deprecated flag",
			args: []string{"--tools_file", "foo.yaml"},
			want: "foo.yaml",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, opts, _, err := invokeCommand(tc.args)
			if err != nil {
				t.Fatalf("unexpected error invoking command: %s", err)
			}
			if opts.ToolsFile != tc.want {
				t.Fatalf("got %v, want %v", opts.Cfg, tc.want)
			}
		})
	}
}

func TestToolsFilesFlag(t *testing.T) {
	tcs := []struct {
		desc string
		args []string
		want []string
	}{
		{
			desc: "no value",
			args: []string{},
			want: []string{},
		},
		{
			desc: "single file",
			args: []string{"--tools-files", "foo.yaml"},
			want: []string{"foo.yaml"},
		},
		{
			desc: "multiple files",
			args: []string{"--tools-files", "foo.yaml,bar.yaml"},
			want: []string{"foo.yaml", "bar.yaml"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, opts, _, err := invokeCommand(tc.args)
			if err != nil {
				t.Fatalf("unexpected error invoking command: %s", err)
			}
			if diff := cmp.Diff(opts.ToolsFiles, tc.want); diff != "" {
				t.Fatalf("got %v, want %v", opts.ToolsFiles, tc.want)
			}
		})
	}
}

func TestToolsFolderFlag(t *testing.T) {
	tcs := []struct {
		desc string
		args []string
		want string
	}{
		{
			desc: "no value",
			args: []string{},
			want: "",
		},
		{
			desc: "folder set",
			args: []string{"--tools-folder", "test-folder"},
			want: "test-folder",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, opts, _, err := invokeCommand(tc.args)
			if err != nil {
				t.Fatalf("unexpected error invoking command: %s", err)
			}
			if opts.ToolsFolder != tc.want {
				t.Fatalf("got %v, want %v", opts.ToolsFolder, tc.want)
			}
		})
	}
}

func TestPrebuiltFlag(t *testing.T) {
	tcs := []struct {
		desc string
		args []string
		want []string
	}{
		{
			desc: "default value",
			args: []string{},
			want: []string{},
		},
		{
			desc: "single prebuilt flag",
			args: []string{"--prebuilt", "validation-runs"},
			want: []string{"validation-runs"},
		},
		{
			desc: "multiple prebuilt flags",
			args: []string{"--prebuilt", "validation-runs", "--prebuilt", "validation-runs"},
			want: []string{"validation-runs", "validation-runs"},
		},
		{
			desc: "comma separated prebuilt flags",
			args: []string{"--prebuilt", "validation-runs,validation-runs"},
			want: []string{"validation-runs", "validation-runs"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, opts, _, err := invokeCommand(tc.args)
			if err != nil {
				t.Fatalf("unexpected error invoking command: %s", err)
			}
			if diff := cmp.Diff(opts.PrebuiltConfigs, tc.want); diff != "" {
				t.Fatalf("got %v, want %v, diff %s", opts.PrebuiltConfigs, tc.want, diff)
			}
		})
	}
}

func TestFailServerConfigFlags(t *testing.T) {
	tcs := []struct {
		desc string
		args []string
	}{
		{
			desc: "logging format",
			args: []string{"--logging-format", "fail"},
		},
		{
			desc: "debug logs",
			args: []string{"--log-level", "fail"},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, err := invokeCommand(tc.args)
			if err == nil {
				t.Fatalf("expected an error, but got nil")
			}
		})
	}
}

func TestDefaultLoggingFormat(t *testing.T) {
	_, opts, _, err := invokeCommand([]string{})
	if err != nil {
		t.Fatalf("unexpected error invoking command: %s", err)
	}
	got := opts.Cfg.LoggingFormat.String()
	want := "standard"
	if got != want {
		t.Fatalf("unexpected default logging format flag: got %v, want %v", got, want)
	}
}

func TestDefaultLogLevel(t *testing.T) {
	_, opts, _, err := invokeCommand([]string{})
	if err != nil {
		t.Fatalf("unexpected error invoking command: %s", err)
	}
	got := opts.Cfg.LogLevel.String()
	want := "info"
	if got != want {
		t.Fatalf("unexpected default log level flag: got %v, want %v", got, want)
	}
}

// normalizeFilepaths is a helper function to allow same filepath formats for Mac and Windows.
// this prevents needing multiple "want" cases for TestResolveWatcherInputs
func normalizeFilepaths(m map[string]bool) map[string]bool {
	newMap := make(map[string]bool)
	for k, v := range m {
		newMap[filepath.ToSlash(k)] = v
	}
	return newMap
}

func TestResolveWatcherInputs(t *testing.T) {
	tcs := []struct {
		description      string
		toolsFile        string
		toolsFiles       []string
		toolsFolder      string
		wantWatchDirs    map[string]bool
		wantWatchedFiles map[string]bool
	}{
		{
			description:      "single tools file",
			toolsFile:        "tools_folder/example_tools.yaml",
			toolsFiles:       []string{},
			toolsFolder:      "",
			wantWatchDirs:    map[string]bool{"tools_folder": true},
			wantWatchedFiles: map[string]bool{"tools_folder/example_tools.yaml": true},
		},
		{
			description:      "default tools file (root dir)",
			toolsFile:        "tools.yaml",
			toolsFiles:       []string{},
			toolsFolder:      "",
			wantWatchDirs:    map[string]bool{".": true},
			wantWatchedFiles: map[string]bool{"tools.yaml": true},
		},
		{
			description:   "multiple files in different folders",
			toolsFile:     "",
			toolsFiles:    []string{"tools_folder/example_tools.yaml", "tools_folder2/example_tools.yaml"},
			toolsFolder:   "",
			wantWatchDirs: map[string]bool{"tools_folder": true, "tools_folder2": true},
			wantWatchedFiles: map[string]bool{
				"tools_folder/example_tools.yaml":  true,
				"tools_folder2/example_tools.yaml": true,
			},
		},
		{
			description:   "multiple files in same folder",
			toolsFile:     "",
			toolsFiles:    []string{"tools_folder/example_tools.yaml", "tools_folder/example_tools2.yaml"},
			toolsFolder:   "",
			wantWatchDirs: map[string]bool{"tools_folder": true},
			wantWatchedFiles: map[string]bool{
				"tools_folder/example_tools.yaml":  true,
				"tools_folder/example_tools2.yaml": true,
			},
		},
		{
			description: "multiple files in different levels",
			toolsFile:   "",
			toolsFiles: []string{
				"tools_folder/example_tools.yaml",
				"tools_folder/special_tools/example_tools2.yaml"},
			toolsFolder:   "",
			wantWatchDirs: map[string]bool{"tools_folder": true, "tools_folder/special_tools": true},
			wantWatchedFiles: map[string]bool{
				"tools_folder/example_tools.yaml":                true,
				"tools_folder/special_tools/example_tools2.yaml": true,
			},
		},
		{
			description:      "tools folder",
			toolsFile:        "",
			toolsFiles:       []string{},
			toolsFolder:      "tools_folder",
			wantWatchDirs:    map[string]bool{"tools_folder": true},
			wantWatchedFiles: map[string]bool{},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			gotWatchDirs, gotWatchedFiles := resolveWatcherInputs(tc.toolsFile, tc.toolsFiles, tc.toolsFolder)

			normalizedGotWatchDirs := normalizeFilepaths(gotWatchDirs)
			normalizedGotWatchedFiles := normalizeFilepaths(gotWatchedFiles)

			if diff := cmp.Diff(tc.wantWatchDirs, normalizedGotWatchDirs); diff != "" {
				t.Errorf("incorrect watchDirs: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantWatchedFiles, normalizedGotWatchedFiles); diff != "" {
				t.Errorf("incorrect watchedFiles: diff %v", diff)
			}

		})
	}
}

// helper function for testing file detection in dynamic reloading
func tmpFileWithCleanup(content []byte) (string, func(), error) {
	f, err := os.CreateTemp("", "*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.Remove(f.Name()) }

	if _, err := f.Write(content); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return f.Name(), cleanup, err
}

func TestSingleEdit(t *testing.T) {
	ctx, cancelCtx := context.WithTimeout(context.Background(), time.Minute)
	defer cancelCtx()

	pr, pw := io.Pipe()
	defer pw.Close()
	defer pr.Close()

	fileToWatch, cleanup, err := tmpFileWithCleanup([]byte("initial content"))
	if err != nil {
		t.Fatalf("error editing tools file %s", err)
	}
	defer cleanup()

	logger, err := log.NewStdLogger(pw, pw, "DEBUG")
	if err != nil {
		t.Fatalf("failed to setup logger %s", err)
	}
	ctx = util.WithLogger(ctx, logger)

	instrumentation, err := telemetry.CreateTelemetryInstrumentation(versionString)
	if err != nil {
		t.Fatalf("failed to setup instrumentation %s", err)
	}
	ctx = util.WithInstrumentation(ctx, instrumentation)

	mockServer := &server.Server{}

	cleanFileToWatch := filepath.Clean(fileToWatch)
	watchDir := filepath.Dir(cleanFileToWatch)

	watchedFiles := map[string]bool{cleanFileToWatch: true}
	watchDirs := map[string]bool{watchDir: true}

	go watchChanges(ctx, watchDirs, watchedFiles, mockServer, 0, server.ServerAuthConfig{})

	// escape backslash so regex doesn't fail on windows filepaths
	regexEscapedPathFile := strings.ReplaceAll(cleanFileToWatch, `\`, `\\\\*\\`)
	regexEscapedPathFile = path.Clean(regexEscapedPathFile)

	regexEscapedPathDir := strings.ReplaceAll(watchDir, `\`, `\\\\*\\`)
	regexEscapedPathDir = path.Clean(regexEscapedPathDir)

	begunWatchingDir := regexp.MustCompile(fmt.Sprintf(`DEBUG "Added directory %s to watcher."`, regexEscapedPathDir))
	_, err = testutils.WaitForString(ctx, begunWatchingDir, pr)
	if err != nil {
		t.Fatalf("timeout or error waiting for watcher to start: %s", err)
	}

	err = os.WriteFile(fileToWatch, []byte("modification"), 0777)
	if err != nil {
		t.Fatalf("error writing to file: %v", err)
	}

	// only check substring of DEBUG message due to some OS/editors firing different operations
	detectedFileChange := regexp.MustCompile(fmt.Sprintf(`event detected in %s"`, regexEscapedPathFile))
	_, err = testutils.WaitForString(ctx, detectedFileChange, pr)
	if err != nil {
		t.Fatalf("timeout or error waiting for file to detect write: %s", err)
	}
}

func TestMutuallyExclusiveFlags(t *testing.T) {
	testCases := []struct {
		desc      string
		args      []string
		errString string
	}{
		{
			desc:      "--tools-file and --tools-files",
			args:      []string{"--tools-file", "my.yaml", "--tools-files", "a.yaml,b.yaml"},
			errString: "--tools-file, --tools-files, and --tools-folder flags cannot be used simultaneously",
		},
		{
			desc:      "--tools-folder and --tools-files",
			args:      []string{"--tools-folder", "./", "--tools-files", "a.yaml,b.yaml"},
			errString: "--tools-file, --tools-files, and --tools-folder flags cannot be used simultaneously",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			buf := new(bytes.Buffer)
			opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
			cmd := NewCommand(opts)
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected an error but got none")
			}
			if !strings.Contains(err.Error(), tc.errString) {
				t.Errorf("expected error message to contain %q, but got %q", tc.errString, err.Error())
			}
		})
	}
}

func TestFileLoadingErrors(t *testing.T) {
	t.Run("non-existent tools-file", func(t *testing.T) {
		buf := new(bytes.Buffer)
		opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
		cmd := NewCommand(opts)
		// Use a file that is guaranteed not to exist
		nonExistentFile := filepath.Join(t.TempDir(), "non-existent-tools.yaml")
		cmd.SetArgs([]string{"--tools-file", nonExistentFile})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected an error for non-existent file but got none")
		}
		if !strings.Contains(err.Error(), "unable to read tool file") {
			t.Errorf("expected error about reading file, but got: %v", err)
		}
	})

	t.Run("non-existent tools-folder", func(t *testing.T) {
		buf := new(bytes.Buffer)
		opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
		cmd := NewCommand(opts)
		nonExistentFolder := filepath.Join(t.TempDir(), "non-existent-folder")
		cmd.SetArgs([]string{"--tools-folder", nonExistentFolder})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("expected an error for non-existent folder but got none")
		}
		if !strings.Contains(err.Error(), "unable to access tools folder") {
			t.Errorf("expected error about accessing folder, but got: %v", err)
		}
	})
}

func TestPrebuiltAndCustomTools(t *testing.T) {
	t.Setenv("SQLITE_DATABASE", "test.db")
	if len(prebuiltconfigs.GetPrebuiltSources()) == 0 {
		t.Skip("no bundled prebuilt configs in this repository variant")
	}

	// Setup custom tools file
	customContent := `
kind: tools
name: custom_tool
type: http
source: my-http
method: GET
path: /
description: "A custom tool for testing"
---
kind: sources
name: my-http
type: http
baseUrl: http://example.com
`
	customFile := filepath.Join(t.TempDir(), "custom.yaml")
	if err := os.WriteFile(customFile, []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Tool Conflict File
	// Validation-runs prebuilt has a tool named 'start_validation_run'
	toolConflictContent := `
kind: tools
name: start_validation_run
type: http
source: my-http
method: GET
path: /
description: "Conflicting tool"
---
kind: sources
name: my-http
type: http
baseUrl: http://example.com
`
	toolConflictFile := filepath.Join(t.TempDir(), "tool_conflict.yaml")
	if err := os.WriteFile(toolConflictFile, []byte(toolConflictContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Toolset Conflict File
	// Validation-runs prebuilt has a toolset named 'validation-run-tools'
	toolsetConflictContent := `
kind: sources
name: dummy-src
type: http
baseUrl: http://example.com
---
kind: tools
name: dummy_tool
type: http
source: dummy-src
method: GET
path: /
description: "Dummy"
---
kind: toolsets
name: validation-run-tools
tools:
- dummy_tool
`
	toolsetConflictFile := filepath.Join(t.TempDir(), "toolset_conflict.yaml")
	if err := os.WriteFile(toolsetConflictFile, []byte(toolsetConflictContent), 0644); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		desc      string
		args      []string
		wantErr   bool
		errString string
		cfgCheck  func(server.ServerConfig) error
	}{
		{
			desc:    "success mixed",
			args:    []string{"--stdio", "--prebuilt", "validation-runs", "--tools-file", customFile},
			wantErr: false,
			cfgCheck: func(cfg server.ServerConfig) error {
				if _, ok := cfg.ToolConfigs["custom_tool"]; !ok {
					return fmt.Errorf("custom tool not found")
				}
				if _, ok := cfg.ToolConfigs["start_validation_run"]; !ok {
					return fmt.Errorf("prebuilt tool 'start_validation_run' not found")
				}
				if _, ok := cfg.ToolsetConfigs["validation-run-tools"]; !ok {
					return fmt.Errorf("prebuilt toolset 'validation-run-tools' not found")
				}
				return nil
			},
		},
		{
			desc:      "validation-runs called twice error",
			args:      []string{"--prebuilt", "validation-runs", "--prebuilt", "validation-runs"},
			wantErr:   true,
			errString: "resource conflicts detected",
		},
		{
			desc:      "tool conflict error",
			args:      []string{"--prebuilt", "validation-runs", "--tools-file", toolConflictFile},
			wantErr:   true,
			errString: "resource conflicts detected",
		},
		{
			desc:      "toolset conflict error",
			args:      []string{"--prebuilt", "validation-runs", "--tools-file", toolsetConflictFile},
			wantErr:   true,
			errString: "resource conflicts detected",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()

			_, opts, output, err := invokeCommandWithContext(ctx, tc.args)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error but got none")
				}
				if !strings.Contains(err.Error(), tc.errString) {
					t.Errorf("expected error message to contain %q, but got %q", tc.errString, err.Error())
				}
			} else {
				if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
					t.Fatalf("unexpected error: %v", err)
				}
				if !strings.Contains(output, "Initialized") {
					t.Errorf("server did not initialize successfully (no initialization log found). Output:\n%s", output)
				}
				if tc.cfgCheck != nil {
					if err := tc.cfgCheck(opts.Cfg); err != nil {
						t.Errorf("config check failed: %v", err)
					}
				}
			}
		})
	}
}

func TestDefaultToolsFileBehavior(t *testing.T) {
	t.Setenv("SQLITE_DATABASE", "test.db")
	noPrebuiltConfigs := len(prebuiltconfigs.GetPrebuiltSources()) == 0
	testCases := []struct {
		desc      string
		args      []string
		expectRun bool
		errString string
	}{
		{
			desc:      "no flags (defaults to tools.yaml)",
			args:      []string{},
			expectRun: false,
			errString: "tools.yaml", // Expect error because tools.yaml doesn't exist in test env
		},
		{
			desc:      "prebuilt only (skips tools.yaml)",
			args:      []string{"--stdio", "--prebuilt", "validation-runs"},
			expectRun: !noPrebuiltConfigs,
			errString: "no prebuilt configurations found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			_, _, output, err := invokeCommandWithContext(ctx, tc.args)

			if tc.expectRun {
				if err != nil && err != context.DeadlineExceeded && err != context.Canceled {
					t.Fatalf("expected server start, got error: %v", err)
				}
				// Verify it actually initialized
				if !strings.Contains(output, "Initialized") {
					t.Errorf("server did not initialize successfully (no initialization log found). Output:\n%s", output)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error reading default file, got nil")
				}
				if !strings.Contains(err.Error(), tc.errString) {
					t.Errorf("expected error message to contain %q, but got %q", tc.errString, err.Error())
				}
			}
		})
	}
}

func TestSubcommandWiring(t *testing.T) {
	buf := new(bytes.Buffer)
	opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
	baseCmd := NewCommand(opts)

	tests := []struct {
		args         []string
		expectedName string
	}{
		{[]string{"invoke"}, "invoke"},
		{[]string{"skills-generate"}, "skills-generate"},
	}

	for _, tc := range tests {
		// Find returns the Command struct and the remaining args
		cmd, _, err := baseCmd.Find(tc.args)

		if err != nil {
			t.Fatalf("Failed to find command %v: %v", tc.args, err)
		}

		if cmd.Name() != tc.expectedName {
			t.Errorf("Expected command name %q, got %q", tc.expectedName, cmd.Name())
		}
	}
}
