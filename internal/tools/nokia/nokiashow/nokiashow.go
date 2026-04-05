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

// Package nokiashow implements the nokia-show MCP tool, which executes
// read-only CLI commands on a Nokia device and returns the raw or
// jq-transformed output.
//
// The tool operates in two modes depending on whether a static command
// template is configured:
//
// # Ad-hoc mode (no `command:` in YAML)
//
// The invoker (LLM or operator) supplies the full command string at runtime
// via the required "command" parameter. An optional "jq" parameter allows
// the caller to supply a jq expression that post-processes the raw output.
//
// When the output is valid JSON the jq expression receives the parsed
// object/array directly. For plain-text output it receives {"text": raw}.
// This auto-detection means JSON-producing commands work without any format
// hint. Use `transforms.run_command.format: json` in the YAML only when you
// want strict parsing (error on invalid JSON instead of silent fallback).
//
//	kind: tools
//	name: show_any
//	type: nokia-show
//	source: nokia-srlinux-lab/spine-1/ssh
//
// # Predefined-command mode (`command:` set in YAML)
//
// The command is fixed in the tool configuration. User-defined parameters
// are declared in the `parameters:` block and interpolated into the command
// template using {paramName} placeholders. A static jq transform can be
// configured via the `transforms:` block; the caller may override it at
// runtime with the optional "jq" parameter.
//
// Parameter values are validated against an allowlist before interpolation;
// only alphanumeric characters, '/', '_', '-', '.', ':', '@', and space are
// permitted. This prevents CLI command injection on the target device.
//
//	kind: tools
//	name: show_interface_detail
//	type: nokia-show
//	source: nokia-srlinux-lab/spine-1/ssh
//	description: Show full details for a single interface
//	command: "show interface {interface} detail"
//	parameters:
//	  - name: interface
//	    type: string
//	    description: Interface name (e.g. "ethernet-1/1")
//	transforms:
//	  run_command:
//	    format: json
//	    jq: '{name: .name, oper: ."oper-state"}'
package nokiashow

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/goccy/go-yaml"
)

const kind = "nokia-show"

// placeholderRe matches {paramName} template placeholders.
var placeholderRe = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9_]*)\}`)

// safeParamValueRe is the allowlist for values interpolated into command
// templates. Only characters safe in Nokia device CLI contexts are permitted;
// shell metacharacters, semicolons, and pipes are explicitly excluded to
// prevent command injection on the target device.
var safeParamValueRe = regexp.MustCompile(`^[a-zA-Z0-9/_\-.:@ ]+$`)

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (tools.ToolConfig, error) {
	actual := Config{Name: name}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

// CommandParam declares a user-defined parameter that is interpolated into
// the predefined command template via {name} placeholders.
type CommandParam struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type,omitempty"` // documentational; always treated as string
	Description string `yaml:"description"`
	Required    *bool  `yaml:"required,omitempty"` // defaults to true
}

// isRequired reports whether the parameter is required. Defaults to true.
func (p CommandParam) isRequired() bool {
	return p.Required == nil || *p.Required
}

// Config holds the YAML-decoded configuration for the Nokia show tool.
type Config struct {
	Name           string                 `yaml:"name" validate:"required"`
	Type           string                 `yaml:"type" validate:"required"`
	Source         string                 `yaml:"source,omitempty"`
	SourceSelector *SourceSelector        `yaml:"sourceSelector,omitempty"`
	Description    string                 `yaml:"description"`
	AuthRequired   []string               `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations `yaml:"annotations"`

	// Command is an optional predefined command template.
	// Use {paramName} placeholders where runtime parameter values appear.
	// When set the "command" runtime parameter is NOT exposed; values come
	// from the parameters declared in ExtraParams.
	// All commands (static and ad-hoc) are validated by
	// safety.ValidateReadOnlyCommand before execution.
	Command string `yaml:"command,omitempty"`

	// ExtraParams declares the parameters interpolated into Command.
	// Only valid when Command is set. Each entry's Name must match a
	// {name} placeholder in the command template.
	ExtraParams []CommandParam `yaml:"parameters,omitempty"`

	// Transforms holds optional per-operation jq expressions.
	// Use the key "run_command" (query.OpRunCommand).
	//
	// Example (predefined mode — transform is always appropriate):
	//   transforms:
	//     run_command:
	//       format: json
	//       jq: '.interface[] | {name, oper: ."oper-state"}'
	//
	// In ad-hoc mode, prefer supplying jq at runtime via the "jq" parameter
	// instead of configuring a static transform, as the transform must work
	// for any command the LLM chooses to run.
	//
	// A format-only entry (format: json, no jq) acts as a format hint:
	// it tells the runtime jq parameter to expect parsed JSON instead of
	// the default {"text": raw} wrapper.
	Transforms map[string]query.TransformSpec `yaml:"transforms,omitempty"`
}

// SourceSelector defines label-based device targeting for fleet operations.
type SourceSelector struct {
	MatchLabels    map[string]string `yaml:"matchLabels"`
	MaxConcurrency int               `yaml:"maxConcurrency,omitempty"`
	// Template filters sources to a specific source-template name (e.g. "ssh",
	// "gnmi", "netconf"). When set, only the devices that have a source with
	// this template name are queried. Devices that lack the template are
	// silently skipped — they do not produce an error.
	Template string `yaml:"template,omitempty"`
}

var _ tools.ToolConfig = Config{}

func (cfg Config) ToolConfigType() string {
	return kind
}

// compatibleSource requires CommandRunner for CLI command execution.
type compatibleSource = capabilities.CommandRunner

// Initialize creates a new Tool instance.
func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	if cfg.Source == "" && cfg.SourceSelector == nil {
		return nil, fmt.Errorf("tool %q must specify either 'source' or 'sourceSelector'", cfg.Name)
	}
	if cfg.Source != "" && cfg.SourceSelector != nil {
		return nil, fmt.Errorf("tool %q cannot specify both 'source' and 'sourceSelector'", cfg.Name)
	}
	if len(cfg.ExtraParams) > 0 && cfg.Command == "" {
		return nil, fmt.Errorf("tool %q defines 'parameters:' but no 'command:' template; parameters are only valid in predefined-command mode", cfg.Name)
	}

	// Validate that every {placeholder} in the template has a declared param.
	if cfg.Command != "" {
		declared := make(map[string]bool, len(cfg.ExtraParams))
		for _, ep := range cfg.ExtraParams {
			declared[ep.Name] = true
		}
		for _, match := range placeholderRe.FindAllStringSubmatch(cfg.Command, -1) {
			if !declared[match[1]] {
				return nil, fmt.Errorf("tool %q: command template references {%s} but no matching parameter is declared in 'parameters:'", cfg.Name, match[1])
			}
		}
	}

	// For single-source mode, validate CLI capability if already resolved.
	if cfg.Source != "" {
		if rawS, ok := srcs[cfg.Source]; ok {
			if _, ok := rawS.(compatibleSource); !ok {
				return nil, fmt.Errorf("invalid source for %q tool: source %q does not support CLI command execution", kind, cfg.Source)
			}
		}
	}

	desc := cfg.Description
	if desc == "" {
		if cfg.Command != "" {
			if cfg.SourceSelector != nil {
				desc = fmt.Sprintf("Run '%s' on Nokia devices matching a label selector. Pass 'device' to target a specific device.", cfg.Command)
			} else {
				desc = fmt.Sprintf("Run '%s' on a Nokia device and return the output.", cfg.Command)
			}
		} else {
			if cfg.SourceSelector != nil {
				desc = "Run a read-only CLI command on Nokia devices matching a label selector. " +
					"Pass 'device' to target a specific device, or omit to run on all matching devices. " +
					"Pass 'jq' with a jq expression to transform the output."
			} else {
				desc = "Run a read-only CLI command on a Nokia device and return the output. " +
					"Pass 'jq' with a jq expression to transform the output."
			}
		}
	}

	allParameters := buildParameters(cfg)

	annotations := tools.GetAnnotationsOrDefault(cfg.Annotations, tools.NewReadOnlyAnnotations)
	mcpManifest := tools.GetMcpManifest(cfg.Name, desc, cfg.AuthRequired, allParameters, annotations)

	executor := query.NewExecutor()
	if len(cfg.Transforms) > 0 {
		executor = executor.WithTransforms(query.TransformSet(cfg.Transforms))
	}

	return Tool{
		Config:      cfg,
		executor:    executor,
		manifest:    tools.Manifest{Description: desc, Parameters: allParameters.Manifest(), AuthRequired: cfg.AuthRequired},
		mcpManifest: mcpManifest,
		Parameters:  allParameters,
	}, nil
}

// buildParameters constructs the runtime parameter list for the tool.
//
//   - Predefined-command mode: ExtraParams + optional "jq" + optional "device"
//   - Ad-hoc mode:             required "command" + optional "jq" + optional "device"
func buildParameters(cfg Config) parameters.Parameters {
	jqDesc := "Optional jq expression to transform the output. " +
		"When the command produces JSON output the expression receives the " +
		"parsed object/array directly (e.g. `.hostname`). " +
		"For plain-text output the expression receives {\"text\": \"<raw>\"} " +
		"so you can reference the string via `.text`. " +
		"Configure `transforms.run_command.format: json` in the tool YAML to " +
		"enable strict JSON mode (error on invalid JSON instead of silent fallback)."

	var params parameters.Parameters

	if cfg.Command != "" {
		// Predefined-command mode: expose declared ExtraParams.
		for _, ep := range cfg.ExtraParams {
			params = append(params, parameters.NewStringParameterWithRequired(ep.Name, ep.Description, ep.isRequired()))
		}
	} else {
		// Ad-hoc mode: the caller supplies the command.
		params = append(params, parameters.NewStringParameterWithRequired(
			"command",
			"The read-only CLI command to run (e.g. 'show interface', 'show route table'). State-changing commands are rejected.",
			true,
		))
	}

	// "jq" is optional in both modes.
	params = append(params, parameters.NewStringParameterWithRequired("jq", jqDesc, false))

	if cfg.SourceSelector != nil {
		params = append(params, parameters.NewStringParameterWithRequired(
			"device",
			"Device name to query (e.g. 'spine-1'). Omit to run the command on all matching devices.",
			false,
		))
	}

	return params
}

// Tool implements the generic Nokia CLI command runner.
type Tool struct {
	Config

	executor    *query.Executor
	manifest    tools.Manifest
	mcpManifest tools.McpManifest
	Parameters  parameters.Parameters
}

// Invoke executes the CLI command.
func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	command, err := t.resolveCommand(params)
	if err != nil {
		return nil, util.NewClientServerError(err.Error(), http.StatusBadRequest, err)
	}

	runtimeJQ := extractStringParam(params, "jq")

	if t.Config.Source != "" {
		return t.invokeSingle(ctx, resourceMgr, t.Config.Source, command, runtimeJQ)
	}
	return t.invokeWithSelector(ctx, resourceMgr, params, command, runtimeJQ)
}

// resolveCommand returns the final CLI command for this invocation.
//
// Predefined mode: expands the config template with runtime parameter values.
// Ad-hoc mode:     returns the "command" parameter value directly.
func (t Tool) resolveCommand(params parameters.ParamValues) (string, error) {
	if t.Config.Command != "" {
		vals := make(map[string]string, len(t.Config.ExtraParams))
		for _, ep := range t.Config.ExtraParams {
			val := extractStringParam(params, ep.Name)
			if ep.isRequired() && val == "" {
				return "", fmt.Errorf("missing required parameter %q", ep.Name)
			}
			vals[ep.Name] = val
		}
		return expandCommandTemplate(t.Config.Command, vals)
	}

	cmd := extractStringParam(params, "command")
	if cmd == "" {
		return "", fmt.Errorf("missing required parameter 'command'")
	}
	return cmd, nil
}

// effectiveExecutor returns the executor to use for this invocation.
// When runtimeJQ is non-empty it overrides (but does not mutate) the base
// executor's transform. The format is inherited from the static config so
// that an explicit `format: json` annotation (strict mode) in the YAML is
// also applied to the runtime jq expression.
//
// Without a format hint, applyJQTransform auto-detects JSON: if the raw
// output parses as JSON the jq expression receives the parsed object;
// otherwise it receives {"text": raw}. No format hint is required for
// JSON-producing commands.
func (t Tool) effectiveExecutor(runtimeJQ string) *query.Executor {
	if runtimeJQ == "" {
		return t.executor
	}
	format := "text"
	if spec, ok := t.Config.Transforms[query.OpRunCommand]; ok && spec.Format != "" {
		format = spec.Format
	}
	return t.executor.WithTransforms(query.TransformSet{
		query.OpRunCommand: {JQ: runtimeJQ, Format: format},
	})
}

func (t Tool) invokeSingle(ctx context.Context, resourceMgr tools.SourceProvider, sourceName, command, runtimeJQ string) (any, util.NOCFoundryError) {
	rawSource, ok := resourceMgr.GetSource(sourceName)
	if !ok {
		return nil, util.NewClientServerError("unable to retrieve source", http.StatusInternalServerError, fmt.Errorf("source %q not found", sourceName))
	}

	exec := t.effectiveExecutor(runtimeJQ)
	record, err := exec.ExecuteCommand(ctx, rawSource, command, sourceName)
	if err != nil {
		return nil, util.NewClientServerError("failed to execute command", http.StatusInternalServerError, err)
	}
	return record, nil
}

func (t Tool) invokeWithSelector(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, command, runtimeJQ string) (any, util.NOCFoundryError) {
	sel := t.Config.SourceSelector

	deviceName := extractStringParam(params, "device")

	matchLabels := sel.MatchLabels
	if sel.Template != "" {
		merged := make(map[string]string, len(matchLabels)+1)
		for k, v := range matchLabels {
			merged[k] = v
		}
		merged["template"] = sel.Template
		matchLabels = merged
	}

	srcs, err := resourceMgr.GetSourcesByLabels(ctx, matchLabels)
	if err != nil {
		return nil, util.NewClientServerError("failed to resolve sources by selector", http.StatusInternalServerError, err)
	}
	if len(srcs) == 0 {
		return nil, util.NewClientServerError("no sources matched the selector", http.StatusNotFound, fmt.Errorf("sourceSelector matched 0 devices"))
	}

	// Deduplicate: keep the best-transport source per device so that a selector
	// without an explicit template: filter does not query every transport for
	// each device. Priority: gnmi > netconf > ssh > other.
	srcs = pickBestPerDevice(srcs)

	if deviceName != "" {
		filtered := filterByDevice(srcs, deviceName)
		if len(filtered) == 0 {
			return nil, util.NewClientServerError(
				fmt.Sprintf("device %q not found among matched sources", deviceName),
				http.StatusNotFound,
				fmt.Errorf("device %q not in selector results", deviceName),
			)
		}
		for sn := range filtered {
			return t.invokeSingle(ctx, resourceMgr, sn, command, runtimeJQ)
		}
	}

	sourceNames := make([]string, 0, len(srcs))
	for name := range srcs {
		sourceNames = append(sourceNames, name)
	}

	maxConc := fanout.DefaultMaxConcurrency
	if sel.MaxConcurrency > 0 {
		maxConc = sel.MaxConcurrency
	}

	exec := t.effectiveExecutor(runtimeJQ)
	result := fanout.Execute(ctx, sourceNames, maxConc, func(ctx context.Context, sourceName string) (any, error) {
		rawSource, ok := resourceMgr.GetSource(sourceName)
		if !ok {
			return nil, fmt.Errorf("source %q not found", sourceName)
		}
		return exec.ExecuteCommand(ctx, rawSource, command, sourceName)
	})

	return result, nil
}

// expandCommandTemplate substitutes {paramName} placeholders in the template
// with the corresponding values from vals after sanitization.
//
// Security: each value is validated against safeParamValueRe before use.
// Placeholders that remain after expansion (no matching value) return an error.
func expandCommandTemplate(tmpl string, vals map[string]string) (string, error) {
	result := tmpl
	for name, value := range vals {
		if value == "" {
			continue
		}
		safe, err := sanitizeParamValue(name, value)
		if err != nil {
			return "", err
		}
		result = strings.ReplaceAll(result, "{"+name+"}", safe)
	}
	// Detect unexpanded placeholders, which indicate missing required params
	// or a mismatch between the template and the declared ExtraParams.
	if m := placeholderRe.FindString(result); m != "" {
		return "", fmt.Errorf("command template has unexpanded placeholder %s", m)
	}
	return result, nil
}

// sanitizeParamValue validates that value contains only characters safe for
// injection into a Nokia device CLI command. Returns an error if any forbidden
// character is detected.
func sanitizeParamValue(name, value string) (string, error) {
	if !safeParamValueRe.MatchString(value) {
		return "", fmt.Errorf(
			"parameter %q contains unsafe characters: only alphanumeric, '/', '_', '-', '.', ':', '@', and space are allowed",
			name,
		)
	}
	return value, nil
}

// extractStringParam returns the string value of the named parameter, or
// empty string if the parameter is absent or has a non-string value.
func extractStringParam(params parameters.ParamValues, name string) string {
	for _, p := range params {
		if p.Name == name && p.Value != nil {
			if s, ok := p.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

// filterByDevice returns sources whose device segment matches the given name.
func filterByDevice(srcs map[string]sources.Source, device string) map[string]sources.Source {
	filtered := make(map[string]sources.Source)
	for name, src := range srcs {
		if extractDevice(name) == device {
			filtered[name] = src
		}
	}
	return filtered
}

// extractDevice extracts the device segment from "group/device/template".
func extractDevice(sourceName string) string {
	first := -1
	for i, c := range sourceName {
		if c == '/' {
			if first < 0 {
				first = i
			} else {
				return sourceName[first+1 : i]
			}
		}
	}
	return sourceName
}

// pickBestPerDevice deduplicates a source map so there is at most one source
// per device. When multiple templates match the same device, the source with
// the highest-priority transport is kept (gnmi > netconf > ssh > other).
// Ties are broken alphabetically by source name for deterministic results.
func pickBestPerDevice(srcs map[string]sources.Source) map[string]sources.Source {
	type candidate struct {
		name string
		prio int
	}
	best := make(map[string]candidate)
	for name := range srcs {
		device := extractDevice(name)
		prio := transportPriority(name)
		if c, ok := best[device]; !ok || prio < c.prio || (prio == c.prio && name < c.name) {
			best[device] = candidate{name: name, prio: prio}
		}
	}
	result := make(map[string]sources.Source, len(best))
	for _, c := range best {
		result[c.name] = srcs[c.name]
	}
	return result
}

// transportPriority returns a lower number for preferred transports.
// gnmi=0, netconf=1, ssh=2, other=3.
func transportPriority(sourceName string) int {
	idx := strings.LastIndex(sourceName, "/")
	template := sourceName
	if idx >= 0 {
		template = sourceName[idx+1:]
	}
	switch template {
	case "gnmi":
		return 0
	case "netconf":
		return 1
	case "ssh":
		return 2
	default:
		return 3
	}
}

func (t Tool) EmbedParams(ctx context.Context, paramValues parameters.ParamValues, embeddingModelsMap map[string]embeddingmodels.EmbeddingModel) (parameters.ParamValues, error) {
	return parameters.EmbedParams(ctx, t.Parameters, paramValues, embeddingModelsMap, nil)
}

func (t Tool) Manifest() tools.Manifest {
	return t.manifest
}

func (t Tool) McpManifest() tools.McpManifest {
	return t.mcpManifest
}

func (t Tool) Authorized(services []string) bool {
	return tools.IsAuthorized(t.AuthRequired, services)
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
	return t.Parameters
}

//
// Unlike the operation-specific tools (nokia-show-interfaces,
// nokia-show-version), this tool does not route through the profile/operation
// system. It is intended for ad-hoc queries that do not have a dedicated
// operation — for example, show route tables, EVPN routes, BGP summaries,
// or any vendor-specific diagnostic output.
//
// The invoker (LLM or operator) supplies the CLI command at runtime as the
// required "command" parameter. All commands are validated by
// safety.ValidateReadOnlyCommand before execution; state-changing keywords
// (configure, delete, commit, etc.) are rejected.
//
