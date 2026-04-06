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

package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/log"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/telemetry"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/trace"
)

type mockSourceConfig struct {
	typ string
}

func (c mockSourceConfig) SourceConfigType() string {
	return c.typ
}

func (c mockSourceConfig) Initialize(_ context.Context, _ trace.Tracer) (sources.Source, error) {
	return &mockSource{}, nil
}

type mockSource struct {
}

func (s *mockSource) SourceType() string {
	return "mock-source"
}

func (s *mockSource) ToConfig() sources.SourceConfig {
	return mockSourceConfig{typ: "mock-source"}
}

func TestServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addr, port := "127.0.0.1", 5000
	cfg := server.ServerConfig{
		Version:      "0.0.0",
		Address:      addr,
		Port:         port,
		AllowedHosts: []string{"*"},
	}

	otelShutdown, err := telemetry.SetupOTel(ctx, "0.0.0", "", "nocfoundry")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	defer func() {
		err := otelShutdown(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
	}()

	testLogger, err := log.NewStdLogger(os.Stdout, os.Stderr, "info")
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	ctx = util.WithLogger(ctx, testLogger)

	instrumentation, err := telemetry.CreateTelemetryInstrumentation(cfg.Version)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	ctx = util.WithInstrumentation(ctx, instrumentation)

	s, err := server.NewServer(ctx, cfg)
	if err != nil {
		t.Fatalf("unable to initialize server: %v", err)
	}

	err = s.Listen(ctx)
	if err != nil {
		t.Fatalf("unable to start server: %v", err)
	}

	// start server in background
	errCh := make(chan error)
	go func() {
		defer close(errCh)

		err = s.Serve(ctx)
		if err != nil {
			errCh <- err
		}
	}()

	url := fmt.Sprintf("http://%s:%d/", addr, port)
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("error when sending a request: %s", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		t.Fatalf("response status code is not 200")
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("error reading from request body: %s", err)
	}
	if got := string(raw); strings.Contains(got, "0.0.0") {
		t.Fatalf("version missing from output: %q", got)
	}
}

func TestUpdateServer(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("error setting up logger: %s", err)
	}

	addr, port := "127.0.0.1", 5000
	cfg := server.ServerConfig{
		Version: "0.0.0",
		Address: addr,
		Port:    port,
	}

	instrumentation, err := telemetry.CreateTelemetryInstrumentation(cfg.Version)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	ctx = util.WithInstrumentation(ctx, instrumentation)

	s, err := server.NewServer(ctx, cfg)
	if err != nil {
		t.Fatalf("error setting up server: %s", err)
	}

	newSources := map[string]sources.Source{
		"example-source": &mockSource{},
	}
	newAuth := map[string]auth.AuthService{"example-auth": nil}
	newEmbeddingModels := map[string]embeddingmodels.EmbeddingModel{"example-model": nil}
	newTools := map[string]tools.Tool{"example-tool": nil}
	newToolsets := map[string]tools.Toolset{
		"example-toolset": {
			ToolsetConfig: tools.ToolsetConfig{
				Name: "example-toolset",
			},
			Tools: []*tools.Tool{},
		},
	}
	newPrompts := map[string]prompts.Prompt{"example-prompt": nil}
	newPromptsets := map[string]prompts.Promptset{
		"example-promptset": {
			PromptsetConfig: prompts.PromptsetConfig{
				Name: "example-promptset",
			},
			Prompts: []*prompts.Prompt{},
		},
	}
	s.ResourceMgr.SetResources(newSources, newAuth, newEmbeddingModels, newTools, newToolsets, newPrompts, newPromptsets)
	if err != nil {
		t.Errorf("error updating server: %s", err)
	}

	gotSource, _ := s.ResourceMgr.GetSource("example-source")
	if diff := cmp.Diff(gotSource, newSources["example-source"]); diff != "" {
		t.Errorf("error updating server, sources (-want +got):\n%s", diff)
	}

	gotAuthService, _ := s.ResourceMgr.GetAuthService("example-auth")
	if diff := cmp.Diff(gotAuthService, newAuth["example-auth"]); diff != "" {
		t.Errorf("error updating server, authServices (-want +got):\n%s", diff)
	}

	gotTool, _ := s.ResourceMgr.GetTool("example-tool")
	if diff := cmp.Diff(gotTool, newTools["example-tool"]); diff != "" {
		t.Errorf("error updating server, tools (-want +got):\n%s", diff)
	}

	gotToolset, _ := s.ResourceMgr.GetToolset("example-toolset")
	if diff := cmp.Diff(gotToolset, newToolsets["example-toolset"]); diff != "" {
		t.Errorf("error updating server, toolset (-want +got):\n%s", diff)
	}

	gotPrompt, _ := s.ResourceMgr.GetPrompt("example-prompt")
	if diff := cmp.Diff(gotPrompt, newPrompts["example-prompt"]); diff != "" {
		t.Errorf("error updating server, prompts (-want +got):\n%s", diff)
	}

	gotPromptset, _ := s.ResourceMgr.GetPromptset("example-promptset")
	if diff := cmp.Diff(gotPromptset, newPromptsets["example-promptset"]); diff != "" {
		t.Errorf("error updating server, promptset (-want +got):\n%s", diff)
	}
}

func TestNameValidation(t *testing.T) {
	testCases := []struct {
		desc         string
		resourceName string
		errStr       string
	}{
		{
			desc:         "names with 0 length",
			resourceName: "",
			errStr:       "resource name SHOULD be between 1 and 128 characters in length (inclusive)",
		},
		{
			desc:         "names with allowed length",
			resourceName: "foo",
		},
		{
			desc:         "names with 128 length",
			resourceName: strings.Repeat("a", 128),
		},
		{
			desc:         "names with more than 128 length",
			resourceName: strings.Repeat("a", 129),
			errStr:       "resource name SHOULD be between 1 and 128 characters in length (inclusive)",
		},
		{
			desc:         "names with space",
			resourceName: "foo bar",
			errStr:       "invalid character for resource name; only uppercase and lowercase ASCII letters (A-Z, a-z), digits (0-9), underscore (_), hyphen (-), and dot (.) is allowed",
		},
		{
			desc:         "names with commas",
			resourceName: "foo,bar",
			errStr:       "invalid character for resource name; only uppercase and lowercase ASCII letters (A-Z, a-z), digits (0-9), underscore (_), hyphen (-), and dot (.) is allowed",
		},
		{
			desc:         "names with other special character",
			resourceName: "foo!",
			errStr:       "invalid character for resource name; only uppercase and lowercase ASCII letters (A-Z, a-z), digits (0-9), underscore (_), hyphen (-), and dot (.) is allowed",
		},
		{
			desc:         "names with allowed special character",
			resourceName: "foo_.-bar6",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			err := server.NameValidation(tc.resourceName)
			if err != nil {
				if tc.errStr != err.Error() {
					t.Fatalf("unexpected error: %s", err)
				}
			}
			if err == nil && tc.errStr != "" {
				t.Fatalf("expect error: %s", tc.errStr)
			}
		})
	}
}
