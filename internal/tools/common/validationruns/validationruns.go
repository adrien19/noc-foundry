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

package validationruns

import (
	"context"
	"fmt"
	"net/http"

	yaml "github.com/goccy/go-yaml"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validation"
	"github.com/adrien19/noc-foundry/internal/validationruns"
)

const (
	kindStart  = "validation-run-start"
	kindStatus = "validation-run-status"
	kindResult = "validation-run-result"
	kindCancel = "validation-run-cancel"
)

func init() {
	for _, kind := range []string{kindStart, kindStatus, kindResult, kindCancel} {
		if !tools.Register(kind, newConfig) {
			panic(fmt.Sprintf("tool kind %q already registered", kind))
		}
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

type Config struct {
	Name         string                 `yaml:"name" validate:"required"`
	Type         string                 `yaml:"type" validate:"required"`
	Description  string                 `yaml:"description,omitempty"`
	AuthRequired []string               `yaml:"authRequired"`
	Annotations  *tools.ToolAnnotations `yaml:"annotations"`
}

func (cfg Config) ToolConfigType() string {
	return cfg.Type
}

func (cfg Config) Initialize(_ map[string]sources.Source) (tools.Tool, error) {
	var params parameters.Parameters
	desc := cfg.Description
	annotations := cfg.Annotations

	switch cfg.Type {
	case kindStart:
		if desc == "" {
			desc = "Start a long-running validation run and return a run ID."
		}
		params = parameters.Parameters{
			parameters.NewStringParameter("validation", "Target validation tool name."),
			parameters.NewMapParameter("params", "Validation tool parameters.", ""),
			parameters.NewStringParameterWithRequired("idempotency_key", "Optional key to deduplicate equivalent active submissions.", false),
		}
	case kindStatus:
		if desc == "" {
			desc = "Fetch the current status of a validation run."
		}
		annotations = tools.GetAnnotationsOrDefault(annotations, tools.NewReadOnlyAnnotations)
		params = parameters.Parameters{
			parameters.NewStringParameter("run_id", "Validation run identifier."),
			parameters.NewIntParameterWithDefault("after_sequence", 0, "Only return events after this sequence number."),
			parameters.NewIntParameterWithDefault("limit", 20, "Maximum number of events to return."),
		}
	case kindResult:
		if desc == "" {
			desc = "Fetch the terminal result of a validation run."
		}
		annotations = tools.GetAnnotationsOrDefault(annotations, tools.NewReadOnlyAnnotations)
		params = parameters.Parameters{
			parameters.NewStringParameter("run_id", "Validation run identifier."),
		}
	case kindCancel:
		if desc == "" {
			desc = "Cancel a running validation run."
		}
		params = parameters.Parameters{
			parameters.NewStringParameter("run_id", "Validation run identifier."),
			parameters.NewStringParameterWithRequired("reason", "Optional cancellation reason.", false),
		}
	default:
		return nil, fmt.Errorf("unsupported validation run tool type %q", cfg.Type)
	}

	return Tool{
		Config:      cfg,
		params:      params,
		manifest:    tools.Manifest{Description: desc, Parameters: params.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, params, annotations),
		annotations: annotations,
	}, nil
}

type provider interface {
	validation.RuntimeProvider
	GetValidationRunManager() validationruns.Manager
}

type Tool struct {
	Config
	params      parameters.Parameters
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	annotations *tools.ToolAnnotations
}

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	p, ok := resourceMgr.(provider)
	if !ok {
		return nil, util.NewClientServerError("validation run manager is unavailable", http.StatusInternalServerError, nil)
	}
	manager := p.GetValidationRunManager()
	if manager == nil {
		return nil, util.NewClientServerError("validation run manager is unavailable", http.StatusInternalServerError, nil)
	}

	values := params.AsMap()
	switch t.Type {
	case kindStart:
		toolName, _ := values["validation"].(string)
		target, ok := p.GetTool(toolName)
		if !ok {
			return nil, util.NewClientServerError(fmt.Sprintf("validation tool %q not found", toolName), http.StatusNotFound, nil)
		}
		asyncTool, ok := target.(validation.AsyncRunnable)
		if !ok {
			return nil, util.NewClientServerError(fmt.Sprintf("tool %q does not support async validation runs", toolName), http.StatusBadRequest, nil)
		}
		requiresAuth, err := target.RequiresClientAuthorization(resourceMgr)
		if err != nil {
			return nil, util.NewClientServerError("failed to check target auth requirements", http.StatusInternalServerError, err)
		}
		if requiresAuth {
			return nil, util.NewClientServerError("validation runs do not support tools that require per-request client authorization", http.StatusBadRequest, nil)
		}
		input, _ := values["params"].(map[string]any)
		parsed, err := parameters.ParseParams(target.GetParameters(), input, nil)
		if err != nil {
			return nil, util.NewClientServerError("invalid validation parameters", http.StatusBadRequest, err)
		}
		parsed, err = target.EmbedParams(ctx, parsed, p.GetEmbeddingModelMap())
		if err != nil {
			return nil, util.NewClientServerError("failed to embed validation parameters", http.StatusBadRequest, err)
		}
		compiled, err := asyncTool.CompileValidationRun(ctx, resourceMgr, parsed)
		if err != nil {
			return nil, util.NewClientServerError("failed to compile validation run", http.StatusBadRequest, err)
		}
		handle, err := manager.Submit(ctx, validationruns.SubmitRequest{
			Compiled:       compiled,
			Executor:       asyncTool,
			AccessToken:    accessToken,
			IdempotencyKey: stringValue(values["idempotency_key"]),
		})
		if err != nil {
			return nil, util.NewClientServerError("failed to submit validation run", http.StatusInternalServerError, err)
		}
		return handle, nil
	case kindStatus:
		runID := stringValue(values["run_id"])
		record, err := manager.Get(ctx, runID)
		if err != nil {
			return nil, toClientError("validation run not found", err)
		}
		after, _ := values["after_sequence"].(int)
		limit, _ := values["limit"].(int)
		events, err := manager.ListEvents(ctx, runID, int64(after), limit)
		if err != nil {
			return nil, util.NewClientServerError("failed to fetch validation run events", http.StatusInternalServerError, err)
		}
		return map[string]any{
			"run":    record,
			"events": events,
		}, nil
	case kindResult:
		runID := stringValue(values["run_id"])
		record, err := manager.Get(ctx, runID)
		if err != nil {
			return nil, toClientError("validation run not found", err)
		}
		result, err := manager.GetResult(ctx, runID)
		if err != nil {
			return nil, toClientError("validation run result is not available", err)
		}
		return map[string]any{
			"run":    record,
			"result": result,
		}, nil
	case kindCancel:
		runID := stringValue(values["run_id"])
		if err := manager.Cancel(ctx, runID, stringValue(values["reason"])); err != nil {
			return nil, toClientError("validation run not found", err)
		}
		record, err := manager.Get(ctx, runID)
		if err != nil {
			return nil, toClientError("validation run not found", err)
		}
		return record, nil
	default:
		return nil, util.NewClientServerError("unsupported validation run tool", http.StatusInternalServerError, nil)
	}
}

func toClientError(msg string, err error) util.NOCFoundryError {
	if err == validationruns.ErrRunNotFound {
		return util.NewClientServerError(msg, http.StatusNotFound, err)
	}
	return util.NewClientServerError(msg, http.StatusInternalServerError, err)
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func (t Tool) EmbedParams(ctx context.Context, paramValues parameters.ParamValues, embeddingModels map[string]embeddingmodels.EmbeddingModel) (parameters.ParamValues, error) {
	return parameters.EmbedParams(ctx, t.params, paramValues, embeddingModels, nil)
}

func (t Tool) Manifest() tools.Manifest {
	return t.manifest
}

func (t Tool) McpManifest() tools.McpManifest {
	return t.mcpManifest
}

func (t Tool) Authorized(verifiedAuthServices []string) bool {
	return tools.IsAuthorized(t.AuthRequired, verifiedAuthServices)
}

func (t Tool) RequiresClientAuthorization(resourceMgr tools.SourceProvider) (bool, error) {
	return false, nil
}

func (t Tool) ToConfig() tools.ToolConfig {
	return t.Config
}

func (t Tool) GetAuthTokenHeaderName(resourceMgr tools.SourceProvider) (string, error) {
	return "Authorization", nil
}

func (t Tool) GetParameters() parameters.Parameters {
	return t.params
}
