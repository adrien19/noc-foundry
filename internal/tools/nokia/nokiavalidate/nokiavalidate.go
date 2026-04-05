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

// Package nokiavalidate implements the nokia-validate MCP tool, a read-only
// declarative validation engine for Nokia devices and blast-radius checks.
package nokiavalidate

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/adrien19/noc-foundry/internal/embeddingmodels"
	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/adrien19/noc-foundry/internal/validation"
	"github.com/goccy/go-yaml"
	"github.com/itchyny/gojq"
)

const kind = "nokia-validate"

func init() {
	if !tools.Register(kind, newConfig) {
		panic(fmt.Sprintf("tool kind %q already registered", kind))
	}
	if !validation.RegisterCompiledRuntime(kind, func(compiled validation.CompiledRun) (validation.CompiledRuntime, error) {
		return Tool{}, nil
	}) {
		panic(fmt.Sprintf("compiled runtime for %q already registered", kind))
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
	Name           string                       `yaml:"name" validate:"required"`
	Type           string                       `yaml:"type" validate:"required"`
	Source         string                       `yaml:"source,omitempty"`
	SourceSelector *SourceSelector              `yaml:"sourceSelector,omitempty"`
	Description    string                       `yaml:"description"`
	AuthRequired   []string                     `yaml:"authRequired"`
	Annotations    *tools.ToolAnnotations       `yaml:"annotations"`
	Defaults       *Defaults                    `yaml:"defaults,omitempty"`
	Groups         map[string]TargetGroup       `yaml:"groups,omitempty"`
	UseProfile     string                       `yaml:"useProfile,omitempty"`
	Profiles       map[string]ValidationProfile `yaml:"profiles,omitempty"`
	Templates      map[string]StepTemplate      `yaml:"templates,omitempty"`
	Phases         []Phase                      `yaml:"phases,omitempty"`
}

type SourceSelector struct {
	MatchLabels    map[string]string `yaml:"matchLabels"`
	MaxConcurrency int               `yaml:"maxConcurrency,omitempty"`
	Template       string            `yaml:"template,omitempty"`
}

type Defaults struct {
	MaxConcurrency int    `yaml:"maxConcurrency,omitempty"`
	FailFast       *bool  `yaml:"failFast,omitempty"`
	Interval       string `yaml:"interval,omitempty"`
	Timeout        string `yaml:"timeout,omitempty"`
}

type ValidationProfile struct {
	Description string          `yaml:"description,omitempty"`
	Defaults    *Defaults       `yaml:"defaults,omitempty"`
	Phases      []PhaseTemplate `yaml:"phases" validate:"required"`
}

type PhaseTemplate struct {
	Name  string         `yaml:"name" validate:"required"`
	Steps []StepTemplate `yaml:"steps" validate:"required"`
}

type TargetGroup struct {
	Devices     []string          `yaml:"devices,omitempty"`
	MatchLabels map[string]string `yaml:"matchLabels,omitempty"`
	Template    string            `yaml:"template,omitempty"`
}

type Phase struct {
	Name        string   `yaml:"name" validate:"required"`
	Description string   `yaml:"description,omitempty"`
	Use         []string `yaml:"use,omitempty"`
	Steps       []Step   `yaml:"steps,omitempty"`
}

type Step struct {
	Name        string          `yaml:"name" validate:"required"`
	Description string          `yaml:"description,omitempty"`
	Collect     *CollectSpec    `yaml:"collect,omitempty"`
	Assert      *AssertSpec     `yaml:"assert,omitempty"`
	Retry       *RetryPolicy    `yaml:"retry,omitempty"` // deprecated alias for simple convergence
	Converge    *ConvergePolicy `yaml:"converge,omitempty"`
	FailFast    *bool           `yaml:"failFast,omitempty"`
}

type StepTemplate struct {
	Name        string          `yaml:"name" validate:"required"`
	Description string          `yaml:"description,omitempty"`
	Collect     *CollectSpec    `yaml:"collect,omitempty"`
	Assert      *AssertSpec     `yaml:"assert,omitempty"`
	Retry       *RetryPolicy    `yaml:"retry,omitempty"`
	Converge    *ConvergePolicy `yaml:"converge,omitempty"`
	FailFast    *bool           `yaml:"failFast,omitempty"`
}

type CollectSpec struct {
	Into       string                         `yaml:"into" validate:"required"`
	Targets    []string                       `yaml:"targets,omitempty"`
	Operation  string                         `yaml:"operation,omitempty"`
	Command    string                         `yaml:"command,omitempty"`
	Transforms map[string]query.TransformSpec `yaml:"transforms,omitempty"`
	Transport  *TransportPolicy               `yaml:"transport,omitempty"`
	Retry      *RetryPolicy                   `yaml:"retry,omitempty"`
}

type TransportPolicy struct {
	Prefer   []string `yaml:"prefer,omitempty"`
	Require  []string `yaml:"require,omitempty"`
	Fallback bool     `yaml:"fallback,omitempty"`
}

type AssertSpec struct {
	Name        string                  `yaml:"name,omitempty"`
	From        []string                `yaml:"from,omitempty"`
	Compare     []EvidenceComparisonRef `yaml:"compare,omitempty"`
	Inputs      map[string]EvidenceRef  `yaml:"inputs,omitempty"`
	Scope       AssertionScope          `yaml:"scope,omitempty"`
	Expr        string                  `yaml:"expr,omitempty"`
	Primitive   *AssertionCheck         `yaml:"check,omitempty"`
	Expect      string                  `yaml:"expect,omitempty"`
	MinPassRate *float64                `yaml:"minPassRate,omitempty"`
	Severity    ResultSeverity          `yaml:"severity,omitempty"`
	Message     string                  `yaml:"message,omitempty"`
}

type RetryPolicy struct {
	Interval    string `yaml:"interval,omitempty"`
	Timeout     string `yaml:"timeout,omitempty"`
	MaxAttempts int    `yaml:"maxAttempts,omitempty"`
}

type ConvergePolicy struct {
	Until        ConvergeCondition `yaml:"until"`
	Interval     string            `yaml:"interval,omitempty"`
	Timeout      string            `yaml:"timeout,omitempty"`
	MaxAttempts  int               `yaml:"maxAttempts,omitempty"`
	Backoff      string            `yaml:"backoff,omitempty"`
	StabilizeFor string            `yaml:"stabilizeFor,omitempty"`
	MinPasses    int               `yaml:"minPasses,omitempty"`
}

type ConvergeCondition string

const (
	ConvergeStepPass      ConvergeCondition = "step_pass"
	ConvergeAssertionPass ConvergeCondition = "assertions_pass"
)

type EvidenceComparisonRef struct {
	Left  EvidenceRef `yaml:"left"`
	Right EvidenceRef `yaml:"right"`
	Name  string      `yaml:"name,omitempty"`
}

type EvidenceRef struct {
	Evidence string `yaml:"evidence,omitempty"`
	Group    string `yaml:"group,omitempty"`
	Alias    string `yaml:"alias,omitempty"`
}

type AssertionScope string

const (
	ScopePerRecord AssertionScope = "per_record"
	ScopePerDevice AssertionScope = "per_device"
	ScopeAggregate AssertionScope = "aggregate"
)

type AssertionType string

const (
	AssertionEquals      AssertionType = "equals"
	AssertionNotEquals   AssertionType = "not_equals"
	AssertionExists      AssertionType = "exists"
	AssertionCountEQ     AssertionType = "count_eq"
	AssertionCountGTE    AssertionType = "count_gte"
	AssertionCountLTE    AssertionType = "count_lte"
	AssertionAllMatch    AssertionType = "all_match"
	AssertionAnyMatch    AssertionType = "any_match"
	AssertionNoneMatch   AssertionType = "none_match"
	AssertionSameAcross  AssertionType = "same_across"
	AssertionDeltaWithin AssertionType = "delta_within"
)

type AssertionReduce string

const (
	ReduceFirst AssertionReduce = "first"
	ReduceCount AssertionReduce = "count"
	ReduceSum   AssertionReduce = "sum"
)

type AssertionCheck struct {
	Type      AssertionType   `yaml:"type"`
	Path      string          `yaml:"path,omitempty"`
	Value     any             `yaml:"value,omitempty"`
	Values    []any           `yaml:"values,omitempty"`
	Count     *int            `yaml:"count,omitempty"`
	Filter    string          `yaml:"filter,omitempty"`
	Tolerance *float64        `yaml:"tolerance,omitempty"`
	MatchBy   string          `yaml:"matchBy,omitempty"`
	Reduce    AssertionReduce `yaml:"reduce,omitempty"`
}

type Result struct {
	Status          ValidationStatus  `json:"status"`
	Outcome         ValidationOutcome `json:"outcome"`
	Blocking        bool              `json:"blocking"`
	ToolName        string            `json:"tool_name"`
	Phase           string            `json:"phase,omitempty"`
	Summary         string            `json:"summary"`
	Recommendations []string          `json:"recommendations,omitempty"`
	Diagnostics     ResultDiagnostics `json:"diagnostics,omitempty"`
	Scope           ResultScope       `json:"scope"`
	Steps           []StepResult      `json:"steps"`
	Evidence        []Evidence        `json:"evidence,omitempty"`
	StartedAt       time.Time         `json:"started_at"`
	CompletedAt     time.Time         `json:"completed_at"`
}

type ValidationStatus string

const (
	StatusPass    ValidationStatus = "pass"
	StatusFail    ValidationStatus = "fail"
	StatusPartial ValidationStatus = "partial"
)

type ValidationOutcome string

const (
	OutcomeHealthy    ValidationOutcome = "healthy"
	OutcomeWarning    ValidationOutcome = "warning"
	OutcomeBlocked    ValidationOutcome = "blocked"
	OutcomeIncomplete ValidationOutcome = "incomplete"
	OutcomeConverging ValidationOutcome = "converging"
)

type ResultSeverity string

const (
	SeverityError   ResultSeverity = "error"
	SeverityWarning ResultSeverity = "warning"
	SeverityInfo    ResultSeverity = "info"
)

type ResultDiagnostics struct {
	TotalSteps       int `json:"total_steps"`
	PassedSteps      int `json:"passed_steps"`
	FailedSteps      int `json:"failed_steps"`
	WarningSteps     int `json:"warning_steps"`
	CollectedRecords int `json:"collected_records"`
	CollectionErrors int `json:"collection_errors"`
}

type ResultScope struct {
	Source   string            `json:"source,omitempty"`
	Selector map[string]string `json:"selector,omitempty"`
	Devices  []string          `json:"devices,omitempty"`
}

type StepKind string

const (
	StepKindCollect StepKind = "collect"
	StepKindAssert  StepKind = "assert"
)

type StepResult struct {
	Name           string             `json:"name"`
	Kind           StepKind           `json:"kind"`
	Status         ValidationStatus   `json:"status"`
	Code           string             `json:"code,omitempty"`
	Summary        string             `json:"summary"`
	Groups         []string           `json:"groups,omitempty"`
	Recommendation string             `json:"recommendation,omitempty"`
	Assertions     []AssertionResult  `json:"assertions,omitempty"`
	Attempts       int                `json:"attempts,omitempty"`
	Convergence    *ConvergenceResult `json:"convergence,omitempty"`
}

type AssertionResult struct {
	Name           string             `json:"name"`
	Status         ValidationStatus   `json:"status"`
	Severity       ResultSeverity     `json:"severity,omitempty"`
	Code           string             `json:"code,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	Scope          AssertionScope     `json:"scope"`
	Expression     string             `json:"expression"`
	Expected       string             `json:"expected,omitempty"`
	Actual         any                `json:"actual,omitempty"`
	Passed         int                `json:"passed,omitempty"`
	Failed         int                `json:"failed,omitempty"`
	Recommendation string             `json:"recommendation,omitempty"`
	Failures       []AssertionFailure `json:"failures,omitempty"`
}

type AssertionFailure struct {
	DeviceID string `json:"device_id,omitempty"`
	Evidence string `json:"evidence,omitempty"`
	Message  string `json:"message"`
}

type ConvergenceResult struct {
	Met              bool       `json:"met"`
	Attempts         int        `json:"attempts"`
	PassStreak       int        `json:"pass_streak"`
	ObservedFailures int        `json:"observed_failures"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      time.Time  `json:"completed_at"`
	LastPassedAt     *time.Time `json:"last_passed_at,omitempty"`
	LastActual       any        `json:"last_actual,omitempty"`
	TimeoutReached   bool       `json:"timeout_reached,omitempty"`
}

type Evidence struct {
	Name    string           `json:"name"`
	Records []EvidenceRecord `json:"records"`
	Summary EvidenceSummary  `json:"summary,omitempty"`
}

type EvidenceRecord struct {
	DeviceID      string            `json:"device_id"`
	SourceName    string            `json:"source_name"`
	Labels        map[string]string `json:"labels,omitempty"`
	Groups        []string          `json:"groups,omitempty"`
	Transport     string            `json:"transport,omitempty"`
	SelectedBy    string            `json:"selected_by,omitempty"`
	SelectionNote string            `json:"selection_note,omitempty"`
	Record        *models.Record    `json:"record,omitempty"`
	Error         string            `json:"error,omitempty"`
}

type EvidenceSummary struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
}

type Tool struct {
	Config

	compiled     compiledConfig
	manifest     tools.Manifest
	mcpManifest  tools.McpManifest
	Parameters   parameters.Parameters
	baseExecutor *query.Executor
}

type compiledRunPayload struct {
	PhaseName string            `json:"phase_name"`
	Scope     ResultScope       `json:"scope"`
	Steps     []compiledRunStep `json:"steps"`
}

type compiledRunStep struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description,omitempty"`
	Kind        StepKind                `json:"kind"`
	FailFast    bool                    `json:"fail_fast"`
	Collect     *compiledRunCollectStep `json:"collect,omitempty"`
	Assert      *compiledRunAssertStep  `json:"assert,omitempty"`
	Converge    *compiledRunConverge    `json:"converge,omitempty"`
}

type compiledRunCollectStep struct {
	Spec  CollectSpec         `json:"spec"`
	Mode  collectionMode      `json:"mode"`
	Plans []compiledPlanEntry `json:"plans"`
	Retry *compiledRunRetry   `json:"retry,omitempty"`
}

type compiledRunAssertStep struct {
	Spec AssertSpec `json:"spec"`
}

type compiledRunConverge struct {
	Until        ConvergeCondition `json:"until"`
	Interval     string            `json:"interval,omitempty"`
	Timeout      string            `json:"timeout,omitempty"`
	MaxAttempts  int               `json:"max_attempts,omitempty"`
	Backoff      string            `json:"backoff,omitempty"`
	StabilizeFor string            `json:"stabilize_for,omitempty"`
	MinPasses    int               `json:"min_passes,omitempty"`
}

type compiledConfig struct {
	phases map[string]compiledPhase
	order  []string
}

type compiledPhase struct {
	name        string
	description string
	steps       []compiledStep
}

type compiledStep struct {
	Step
	mode     collectionMode
	collect  *compiledCollect
	assert   *compiledAssert
	converge *compiledConverge
	failFast bool
}

type collectionMode string

const (
	modeOperation collectionMode = "operation"
	modeCommand   collectionMode = "command"
)

type compiledCollect struct {
	spec      CollectSpec
	mode      collectionMode
	transport TransportPolicy
	retry     *compiledRetry
}

type compiledRetry struct {
	interval    time.Duration
	timeout     time.Duration
	maxAttempts int
}

type compiledRunRetry struct {
	Interval    string `json:"interval,omitempty"`
	Timeout     string `json:"timeout,omitempty"`
	MaxAttempts int    `json:"max_attempts,omitempty"`
}

type collectAttemptResult struct {
	Evidence        Evidence
	Status          ValidationStatus
	Code            string
	Recommendation  string
	FailureCategory collectFailureCategory
}

type collectFailureCategory string

const (
	collectFailureNone         collectFailureCategory = ""
	collectFailureRetryable    collectFailureCategory = "retryable"
	collectFailureNonRetryable collectFailureCategory = "non_retryable"
)

type compiledAssert struct {
	spec AssertSpec
}

type compiledConverge struct {
	until        ConvergeCondition
	interval     time.Duration
	timeout      time.Duration
	maxAttempts  int
	backoff      string
	stabilizeFor time.Duration
	minPasses    int
}

type ResolvedTarget struct {
	DeviceID     string
	SourceName   string
	Labels       map[string]string
	GroupNames   []string
	Transport    string
	Capabilities capabilities.SourceCapabilities
}

type transportPlan struct {
	target        ResolvedTarget
	selectedBy    string
	selectionNote string
}

type compiledPlanEntry struct {
	Target        ResolvedTarget `json:"target"`
	SelectedBy    string         `json:"selected_by,omitempty"`
	SelectionNote string         `json:"selection_note,omitempty"`
}

type EvidenceStore struct {
	byName map[string]Evidence
	order  []string
}

func newEvidenceStore() *EvidenceStore {
	return &EvidenceStore{byName: make(map[string]Evidence)}
}

func (s *EvidenceStore) Put(name string, evidence Evidence) {
	if _, ok := s.byName[name]; !ok {
		s.order = append(s.order, name)
	}
	s.byName[name] = evidence
}

func (s *EvidenceStore) Get(name string) (Evidence, bool) {
	ev, ok := s.byName[name]
	return ev, ok
}

func (s *EvidenceStore) MustGetMany(names []string) ([]Evidence, error) {
	out := make([]Evidence, 0, len(names))
	for _, name := range names {
		ev, ok := s.byName[name]
		if !ok {
			return nil, fmt.Errorf("evidence %q not found", name)
		}
		if len(ev.Records) == 0 {
			return nil, fmt.Errorf("evidence %q is empty", name)
		}
		out = append(out, ev)
	}
	return out, nil
}

func (s *EvidenceStore) All() []Evidence {
	out := make([]Evidence, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.byName[name])
	}
	return out
}

var _ tools.ToolConfig = Config{}

func (cfg Config) ToolConfigType() string {
	return kind
}

func (cfg Config) Initialize(srcs map[string]sources.Source) (tools.Tool, error) {
	resolved, err := cfg.expandProfilesAndTemplates()
	if err != nil {
		return nil, err
	}
	if resolved.Source == "" && resolved.SourceSelector == nil {
		return nil, fmt.Errorf("tool %q must specify either 'source' or 'sourceSelector'", resolved.Name)
	}
	if resolved.Source != "" && resolved.SourceSelector != nil {
		return nil, fmt.Errorf("tool %q cannot specify both 'source' and 'sourceSelector'", resolved.Name)
	}
	if len(resolved.Phases) == 0 {
		return nil, fmt.Errorf("tool %q must define at least one phase", resolved.Name)
	}

	compiled, err := resolved.compileConfig(srcs)
	if err != nil {
		return nil, err
	}

	desc := resolved.Description
	if desc == "" {
		desc = "Validate Nokia device or blast-radius state across one or more collection and assertion phases."
	}
	allParameters := buildParameters(resolved)
	annotations := tools.GetAnnotationsOrDefault(resolved.Annotations, tools.NewReadOnlyAnnotations)
	mcpManifest := tools.GetMcpManifest(resolved.Name, desc, resolved.AuthRequired, allParameters, annotations)

	return Tool{
		Config:       resolved,
		compiled:     compiled,
		manifest:     tools.Manifest{Description: desc, Parameters: allParameters.Manifest(), AuthRequired: resolved.AuthRequired},
		mcpManifest:  mcpManifest,
		Parameters:   allParameters,
		baseExecutor: query.NewExecutor(),
	}, nil
}

func buildParameters(cfg Config) parameters.Parameters {
	var params parameters.Parameters
	if len(cfg.Phases) > 1 {
		params = append(params, parameters.NewStringParameterWithRequired(
			"phase",
			"Validation phase to execute. Required when the tool defines multiple phases.",
			true,
		))
	}
	if cfg.SourceSelector != nil {
		params = append(params, parameters.NewStringParameterWithRequired(
			"device",
			"Device name to narrow selector scope to a single device.",
			false,
		))
	}
	return params
}

func (cfg Config) expandProfilesAndTemplates() (Config, error) {
	resolved := cfg
	if cfg.UseProfile != "" && len(cfg.Phases) > 0 {
		return Config{}, fmt.Errorf("tool %q cannot define both useProfile and phases", cfg.Name)
	}
	if cfg.UseProfile != "" {
		profile, ok := cfg.Profiles[cfg.UseProfile]
		if !ok {
			return Config{}, fmt.Errorf("tool %q references unknown profile %q", cfg.Name, cfg.UseProfile)
		}
		if resolved.Defaults == nil && profile.Defaults != nil {
			resolved.Defaults = profile.Defaults
		}
		resolved.Phases = make([]Phase, 0, len(profile.Phases))
		for _, pt := range profile.Phases {
			phase := Phase{Name: pt.Name}
			for _, st := range pt.Steps {
				phase.Steps = append(phase.Steps, stepFromTemplate(st))
			}
			resolved.Phases = append(resolved.Phases, phase)
		}
	}

	if len(resolved.Phases) == 0 {
		return resolved, nil
	}
	expanded := make([]Phase, 0, len(resolved.Phases))
	for _, phase := range resolved.Phases {
		next := Phase{Name: phase.Name, Description: phase.Description}
		for _, name := range phase.Use {
			template, ok := cfg.Templates[name]
			if !ok {
				return Config{}, fmt.Errorf("phase %q references unknown template %q", phase.Name, name)
			}
			next.Steps = append(next.Steps, stepFromTemplate(template))
		}
		next.Steps = append(next.Steps, phase.Steps...)
		expanded = append(expanded, next)
	}
	resolved.Phases = expanded
	return resolved, nil
}

func stepFromTemplate(st StepTemplate) Step {
	return Step{
		Name:        st.Name,
		Description: st.Description,
		Collect:     st.Collect,
		Assert:      st.Assert,
		Retry:       st.Retry,
		Converge:    st.Converge,
		FailFast:    st.FailFast,
	}
}

func (cfg Config) compileConfig(srcs map[string]sources.Source) (compiledConfig, error) {
	seenPhases := map[string]bool{}
	out := compiledConfig{phases: map[string]compiledPhase{}}

	for _, phase := range cfg.Phases {
		if phase.Name == "" {
			return compiledConfig{}, fmt.Errorf("phase name is required")
		}
		if seenPhases[phase.Name] {
			return compiledConfig{}, fmt.Errorf("duplicate phase %q", phase.Name)
		}
		seenPhases[phase.Name] = true
		if len(phase.Steps) == 0 {
			return compiledConfig{}, fmt.Errorf("phase %q must define at least one step", phase.Name)
		}

		cp := compiledPhase{name: phase.Name, description: phase.Description}
		seenEvidence := map[string]bool{}
		for _, step := range phase.Steps {
			cs, err := cfg.compileStep(step, seenEvidence, srcs)
			if err != nil {
				return compiledConfig{}, fmt.Errorf("phase %q step %q: %w", phase.Name, step.Name, err)
			}
			cp.steps = append(cp.steps, cs)
		}
		out.order = append(out.order, phase.Name)
		out.phases[phase.Name] = cp
	}
	return out, nil
}

func (cfg Config) compileStep(step Step, seenEvidence map[string]bool, srcs map[string]sources.Source) (compiledStep, error) {
	if step.Name == "" {
		return compiledStep{}, fmt.Errorf("step name is required")
	}
	if step.Retry != nil && step.Converge != nil {
		return compiledStep{}, fmt.Errorf("cannot define both retry and converge")
	}
	if (step.Collect == nil) == (step.Assert == nil) {
		return compiledStep{}, fmt.Errorf("must define exactly one of collect or assert")
	}

	out := compiledStep{Step: step, failFast: cfg.defaultFailFast(step)}

	if step.Collect != nil {
		cc, err := cfg.compileCollect(*step.Collect, srcs)
		if err != nil {
			return compiledStep{}, err
		}
		if seenEvidence[cc.spec.Into] {
			return compiledStep{}, fmt.Errorf("duplicate evidence name %q", cc.spec.Into)
		}
		seenEvidence[cc.spec.Into] = true
		out.mode = cc.mode
		out.collect = &cc
	}

	if step.Assert != nil {
		if step.Converge != nil && step.Collect != nil {
			return compiledStep{}, fmt.Errorf("converge is only supported on assert steps")
		}
		ca, err := cfg.compileAssert(*step.Assert)
		if err != nil {
			return compiledStep{}, err
		}
		out.assert = &ca
	}

	conv, err := cfg.compileConverge(step.Retry, step.Converge)
	if err != nil {
		return compiledStep{}, err
	}
	out.converge = conv
	return out, nil
}

func (cfg Config) compileCollect(spec CollectSpec, srcs map[string]sources.Source) (compiledCollect, error) {
	if spec.Into == "" {
		return compiledCollect{}, fmt.Errorf("collect.into is required")
	}
	if spec.Operation == "" && spec.Command == "" {
		return compiledCollect{}, fmt.Errorf("collect must define operation or command")
	}
	if spec.Operation != "" && spec.Command != "" {
		return compiledCollect{}, fmt.Errorf("collect cannot define both operation and command")
	}

	mode := modeOperation
	if spec.Command != "" {
		mode = modeCommand
	}
	policy := cfg.defaultTransportPolicy(mode, spec.Transport)
	if cfg.Source != "" && len(policy.Require) > 0 && !slices.Contains(policy.Require, templateOf(cfg.Source)) {
		return compiledCollect{}, fmt.Errorf("fixed source %q does not satisfy required transports %v", cfg.Source, policy.Require)
	}
	if cfg.Source != "" && mode == modeCommand {
		if src, ok := srcs[cfg.Source]; ok {
			if _, ok := src.(capabilities.CommandRunner); !ok {
				return compiledCollect{}, fmt.Errorf("fixed source %q does not support CLI command execution", cfg.Source)
			}
		}
	}
	retry, err := cfg.compileCollectRetry(spec.Retry)
	if err != nil {
		return compiledCollect{}, err
	}

	return compiledCollect{
		spec:      spec,
		mode:      mode,
		transport: policy,
		retry:     retry,
	}, nil
}

func (cfg Config) compileCollectRetry(retry *RetryPolicy) (*compiledRetry, error) {
	if retry == nil {
		return nil, nil
	}
	maxAttempts := retry.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 1
	}
	if maxAttempts < 1 {
		return nil, fmt.Errorf("collect.retry.maxAttempts must be greater than 0")
	}
	var interval time.Duration
	var err error
	if maxAttempts > 1 {
		if strings.TrimSpace(retry.Interval) == "" {
			return nil, fmt.Errorf("collect.retry.interval is required when collect.retry.maxAttempts > 1")
		}
		interval, err = time.ParseDuration(retry.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid collect.retry.interval %q: %w", retry.Interval, err)
		}
		if interval < 0 {
			return nil, fmt.Errorf("collect.retry.interval must be greater than or equal to 0")
		}
	}
	var timeout time.Duration
	if strings.TrimSpace(retry.Timeout) != "" {
		timeout, err = time.ParseDuration(retry.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid collect.retry.timeout %q: %w", retry.Timeout, err)
		}
		if timeout <= 0 {
			return nil, fmt.Errorf("collect.retry.timeout must be greater than 0")
		}
	}
	return &compiledRetry{
		interval:    interval,
		timeout:     timeout,
		maxAttempts: maxAttempts,
	}, nil
}

func (cfg Config) defaultTransportPolicy(mode collectionMode, in *TransportPolicy) TransportPolicy {
	if in != nil {
		return *in
	}
	switch mode {
	case modeCommand:
		return TransportPolicy{
			Prefer:   []string{"ssh"},
			Require:  []string{"ssh"},
			Fallback: false,
		}
	default:
		return TransportPolicy{
			Prefer:   []string{"netconf", "gnmi", "ssh"},
			Fallback: true,
		}
	}
}

func (cfg Config) compileAssert(spec AssertSpec) (compiledAssert, error) {
	if len(spec.From) == 0 && len(spec.Inputs) == 0 && len(spec.Compare) == 0 {
		return compiledAssert{}, fmt.Errorf("assert must reference at least one evidence input")
	}
	if spec.Expr != "" && spec.Primitive != nil {
		return compiledAssert{}, fmt.Errorf("assert cannot define both expr and check")
	}
	if spec.Expr == "" && spec.Primitive == nil {
		return compiledAssert{}, fmt.Errorf("assert requires expr or check")
	}
	scope := spec.effectiveScope()
	if scope != ScopePerRecord && scope != ScopePerDevice && scope != ScopeAggregate {
		return compiledAssert{}, fmt.Errorf("invalid assert.scope %q", scope)
	}
	if spec.Primitive != nil {
		if err := validatePrimitive(spec.Primitive); err != nil {
			return compiledAssert{}, err
		}
	}
	aliases := map[string]bool{}
	for name, ref := range spec.Inputs {
		alias := ref.effectiveAlias(name)
		if aliases[alias] {
			return compiledAssert{}, fmt.Errorf("duplicate assert input alias %q", alias)
		}
		aliases[alias] = true
	}
	for _, cmp := range spec.Compare {
		if cmp.Left.Evidence == "" || cmp.Right.Evidence == "" {
			return compiledAssert{}, fmt.Errorf("compare requires left.evidence and right.evidence")
		}
	}
	return compiledAssert{spec: spec}, nil
}

func validatePrimitive(check *AssertionCheck) error {
	if check == nil {
		return nil
	}
	switch check.Type {
	case AssertionEquals, AssertionNotEquals, AssertionExists,
		AssertionCountEQ, AssertionCountGTE, AssertionCountLTE,
		AssertionAllMatch, AssertionAnyMatch, AssertionNoneMatch,
		AssertionSameAcross, AssertionDeltaWithin:
	default:
		return fmt.Errorf("unsupported check.type %q", check.Type)
	}
	switch check.Type {
	case AssertionEquals, AssertionNotEquals, AssertionExists, AssertionSameAcross:
		if check.Path == "" {
			return fmt.Errorf("path is required")
		}
	case AssertionCountEQ, AssertionCountGTE, AssertionCountLTE:
		if check.Path == "" || check.Count == nil {
			return fmt.Errorf("path and count are required")
		}
	case AssertionAllMatch, AssertionAnyMatch, AssertionNoneMatch:
		if check.Path == "" || check.Filter == "" {
			return fmt.Errorf("path and filter are required")
		}
	case AssertionDeltaWithin:
		if check.Tolerance == nil {
			return fmt.Errorf("tolerance is required")
		}
		if check.Reduce == "" {
			return fmt.Errorf("reduce is required")
		}
	}
	return nil
}

func (cfg Config) compileConverge(retry *RetryPolicy, converge *ConvergePolicy) (*compiledConverge, error) {
	if retry == nil && converge == nil {
		return nil, nil
	}
	if retry != nil && converge != nil {
		return nil, fmt.Errorf("cannot define both retry and converge")
	}

	if retry != nil {
		converge = &ConvergePolicy{
			Until:       ConvergeAssertionPass,
			Interval:    retry.Interval,
			Timeout:     retry.Timeout,
			MaxAttempts: retry.MaxAttempts,
			Backoff:     "fixed",
			MinPasses:   1,
		}
	}

	interval, err := cfg.parseDurationWithDefaults(converge.Interval, "interval")
	if err != nil {
		return nil, err
	}
	timeout, err := cfg.parseDurationWithDefaults(converge.Timeout, "timeout")
	if err != nil {
		return nil, err
	}
	stabilizeFor, err := cfg.parseDurationWithDefaults(converge.StabilizeFor, "stabilizeFor")
	if err != nil {
		return nil, err
	}
	if interval <= 0 {
		interval = time.Second
	}
	maxAttempts := converge.MaxAttempts
	if maxAttempts <= 0 {
		if timeout > 0 {
			maxAttempts = int(timeout/interval) + 1
		} else {
			maxAttempts = 1
		}
	}
	minPasses := converge.MinPasses
	if minPasses <= 0 {
		minPasses = 1
	}
	until := converge.Until
	if until == "" {
		until = ConvergeAssertionPass
	}

	return &compiledConverge{
		until:        until,
		interval:     interval,
		timeout:      timeout,
		maxAttempts:  maxAttempts,
		backoff:      converge.Backoff,
		stabilizeFor: stabilizeFor,
		minPasses:    minPasses,
	}, nil
}

func (cfg Config) defaultFailFast(step Step) bool {
	if step.FailFast != nil {
		return *step.FailFast
	}
	if cfg.Defaults != nil && cfg.Defaults.FailFast != nil {
		return *cfg.Defaults.FailFast
	}
	return step.Assert != nil
}

func (cfg Config) parseDurationWithDefaults(raw, field string) (time.Duration, error) {
	if raw == "" && cfg.Defaults != nil {
		switch field {
		case "interval":
			raw = cfg.Defaults.Interval
		case "timeout":
			raw = cfg.Defaults.Timeout
		}
	}
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration %q: %w", field, raw, err)
	}
	return d, nil
}

func (t Tool) Invoke(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	compiled, err := t.CompileValidationRun(ctx, resourceMgr, params)
	if err != nil {
		return nil, util.NewClientServerError("failed to compile validation run", http.StatusBadRequest, err)
	}
	return t.ExecuteCompiledValidation(ctx, resourceMgr, compiled, accessToken)
}

func (t Tool) CompileValidationRun(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues) (validation.CompiledRun, error) {
	phase, err := t.resolvePhase(params)
	if err != nil {
		return validation.CompiledRun{}, err
	}
	scope, groups, err := t.resolveScope(ctx, resourceMgr, params)
	if err != nil {
		return validation.CompiledRun{}, err
	}

	payload := compiledRunPayload{
		PhaseName: phase.name,
		Scope:     scope,
		Steps:     make([]compiledRunStep, 0, len(phase.steps)),
	}
	for _, step := range phase.steps {
		next := compiledRunStep{
			Name:        step.Name,
			Description: step.Description,
			FailFast:    step.failFast,
		}
		if step.collect != nil {
			targets, err := selectTargets(step.collect.spec.Targets, groups)
			if err != nil {
				return validation.CompiledRun{}, err
			}
			next.Kind = StepKindCollect
			var retry *compiledRunRetry
			if step.collect.retry != nil {
				retry = &compiledRunRetry{
					Interval:    step.collect.retry.interval.String(),
					Timeout:     step.collect.retry.timeout.String(),
					MaxAttempts: step.collect.retry.maxAttempts,
				}
			}
			next.Collect = &compiledRunCollectStep{
				Spec:  step.collect.spec,
				Mode:  step.collect.mode,
				Plans: serializePlans(buildTransportPlans(targets, step.collect)),
				Retry: retry,
			}
		} else {
			next.Kind = StepKindAssert
			next.Assert = &compiledRunAssertStep{Spec: step.assert.spec}
			if step.converge != nil {
				next.Converge = &compiledRunConverge{
					Until:        step.converge.until,
					Interval:     step.converge.interval.String(),
					Timeout:      step.converge.timeout.String(),
					MaxAttempts:  step.converge.maxAttempts,
					Backoff:      step.converge.backoff,
					StabilizeFor: step.converge.stabilizeFor.String(),
					MinPasses:    step.converge.minPasses,
				}
			}
		}
		payload.Steps = append(payload.Steps, next)
	}

	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return validation.CompiledRun{}, fmt.Errorf("failed to encode compiled validation payload: %w", err)
	}
	configRaw, err := json.Marshal(t.ToConfig())
	if err != nil {
		return validation.CompiledRun{}, fmt.Errorf("failed to encode validation config snapshot: %w", err)
	}
	return validation.CompiledRun{
		RunType:           "validation",
		ToolName:          t.Name,
		ToolType:          t.Type,
		Phase:             payload.PhaseName,
		Steps:             compiledStepRefs(payload),
		SubmittedParams:   params.AsMap(),
		ResourceVersion:   resourceVersionFrom(resourceMgr),
		ConfigFingerprint: fingerprint(configRaw),
		PlanFingerprint:   fingerprint(payloadRaw),
		PayloadVersion:    "nokia-validate/v1",
		Payload:           payloadRaw,
		CreatedAt:         time.Now(),
	}, nil
}

func compiledStepRefs(payload compiledRunPayload) []validation.StepRef {
	refs := make([]validation.StepRef, 0, len(payload.Steps))
	for index, step := range payload.Steps {
		refs = append(refs, validation.StepRef{
			Phase: payload.PhaseName,
			Index: index,
			Name:  step.Name,
			Kind:  string(step.Kind),
		})
	}
	return refs
}

func (t Tool) ExecuteCompiledValidation(ctx context.Context, resourceMgr tools.SourceProvider, compiled validation.CompiledRun, accessToken tools.AccessToken) (any, util.NOCFoundryError) {
	_ = accessToken
	payload, err := decodeCompiledRunPayload(compiled)
	if err != nil {
		return nil, util.NewClientServerError("failed to decode compiled validation payload", http.StatusInternalServerError, err)
	}
	startedAt := time.Now()
	store := newEvidenceStore()
	stepResults := make([]StepResult, 0, len(payload.Steps))
	for index, step := range payload.Steps {
		ref := validation.StepRef{
			Phase: payload.PhaseName,
			Index: index,
			Name:  step.Name,
			Kind:  string(step.Kind),
		}
		input := validation.StepExecutionInput{
			Attempt:  1,
			Evidence: mapEvidenceStore(store),
		}
		for {
			output, stepErr := t.ExecuteCompiledStep(ctx, resourceMgr, compiled, ref, input, accessToken)
			if stepErr != nil {
				return nil, stepErr
			}
			if err := applyEvidenceDelta(store, output.EvidenceDelta); err != nil {
				return nil, util.NewClientServerError("failed to apply step evidence", http.StatusInternalServerError, err)
			}
			if output.StepCompleted {
				var stepResult StepResult
				if err := json.Unmarshal(output.StepResult, &stepResult); err != nil {
					return nil, util.NewClientServerError("failed to decode step result", http.StatusInternalServerError, err)
				}
				stepResults = append(stepResults, stepResult)
				if output.FailFastStop || output.Terminal {
					break
				}
				goto nextStep
			}
			wait, err := time.ParseDuration(output.RetryAfter)
			if err != nil {
				return nil, util.NewClientServerError("failed to parse retry interval", http.StatusInternalServerError, err)
			}
			if !sleepWithContext(ctx, wait) {
				break
			}
			input.Attempt++
			input.Evidence = mapEvidenceStore(store)
			input.ConvergenceState = output.ConvergenceState
		}
		break
	nextStep:
	}
	result, resultErr := t.assembleCompiledValidationResult(payload, store, stepResults, startedAt, time.Now())
	if resultErr != nil {
		return nil, util.NewClientServerError("failed to assemble validation result", http.StatusInternalServerError, resultErr)
	}
	return result, nil
}

func (t Tool) ExecuteCompiledStep(ctx context.Context, resourceMgr tools.SourceProvider, compiled validation.CompiledRun, ref validation.StepRef, input validation.StepExecutionInput, accessToken tools.AccessToken) (validation.StepExecutionOutput, util.NOCFoundryError) {
	_ = accessToken
	payload, err := decodeCompiledRunPayload(compiled)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("failed to decode compiled validation payload", http.StatusInternalServerError, err)
	}
	if ref.Index < 0 || ref.Index >= len(payload.Steps) {
		return validation.StepExecutionOutput{}, util.NewClientServerError("compiled step index out of range", http.StatusBadRequest, nil)
	}
	step := payload.Steps[ref.Index]
	store, err := evidenceStoreFromMap(input.Evidence)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("failed to decode compiled evidence", http.StatusBadRequest, err)
	}
	if step.Collect != nil {
		attemptCtx, cancel := withCollectAttemptTimeout(ctx, step.Collect.Retry)
		defer cancel()
		stepResult, delta, failureClass, err := t.runCompiledPlanStep(attemptCtx, resourceMgr, step, store)
		if err != nil {
			return validation.StepExecutionOutput{}, util.NewClientServerError("validation step failed", http.StatusBadRequest, err)
		}
		stepRaw, err := json.Marshal(stepResult)
		if err != nil {
			return validation.StepExecutionOutput{}, util.NewClientServerError("failed to encode step result", http.StatusInternalServerError, err)
		}
		stepFailed := stepResult.Status == StatusFail || stepResult.Code == "collection_failed"
		retryable := stepFailed && failureClass == collectFailureRetryable && collectAttemptRemaining(step.Collect.Retry, input.Attempt)
		if retryable {
			return validation.StepExecutionOutput{
				StepResult:    stepRaw,
				EvidenceDelta: nil,
				FailFastStop:  false,
				Terminal:      false,
				StepCompleted: false,
				RetryAfter:    collectRetryInterval(step.Collect.Retry),
			}, nil
		}
		return validation.StepExecutionOutput{
			StepResult:    stepRaw,
			EvidenceDelta: delta,
			FailFastStop:  stepFailed || (step.FailFast && stepFailed),
			Terminal:      stepFailed,
			StepCompleted: true,
		}, nil
	}
	stepResult, convState, retryAfter, completed, err := t.runCompiledAssertAttempt(step, store, input)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("validation step failed", http.StatusBadRequest, err)
	}
	stepRaw, err := json.Marshal(stepResult)
	if err != nil {
		return validation.StepExecutionOutput{}, util.NewClientServerError("failed to encode step result", http.StatusInternalServerError, err)
	}
	return validation.StepExecutionOutput{
		StepResult:       stepRaw,
		FailFastStop:     completed && step.FailFast && (stepResult.Status == StatusFail || stepResult.Code == "collection_failed"),
		Terminal:         false,
		StepCompleted:    completed,
		RetryAfter:       retryAfter,
		ConvergenceState: convState,
	}, nil
}

func (t Tool) AssembleCompiledValidationResult(ctx context.Context, resourceMgr tools.SourceProvider, compiled validation.CompiledRun, evidence map[string]json.RawMessage, stepResults []json.RawMessage) (any, util.NOCFoundryError) {
	_ = ctx
	_ = resourceMgr
	payload, err := decodeCompiledRunPayload(compiled)
	if err != nil {
		return nil, util.NewClientServerError("failed to decode compiled validation payload", http.StatusInternalServerError, err)
	}
	store, err := evidenceStoreFromMap(evidence)
	if err != nil {
		return nil, util.NewClientServerError("failed to decode compiled evidence", http.StatusBadRequest, err)
	}
	decodedSteps := make([]StepResult, 0, len(stepResults))
	for _, raw := range stepResults {
		var step StepResult
		if err := json.Unmarshal(raw, &step); err != nil {
			return nil, util.NewClientServerError("failed to decode step result", http.StatusInternalServerError, err)
		}
		decodedSteps = append(decodedSteps, step)
	}
	result, err := t.assembleCompiledValidationResult(payload, store, decodedSteps, compiled.CreatedAt, time.Now())
	if err != nil {
		return nil, util.NewClientServerError("failed to assemble validation result", http.StatusInternalServerError, err)
	}
	return result, nil
}

func decodeCompiledRunPayload(compiled validation.CompiledRun) (compiledRunPayload, error) {
	var payload compiledRunPayload
	if err := json.Unmarshal(compiled.Payload, &payload); err != nil {
		return compiledRunPayload{}, err
	}
	return payload, nil
}

func evidenceStoreFromMap(raw map[string]json.RawMessage) (*EvidenceStore, error) {
	store := newEvidenceStore()
	for name, data := range raw {
		var evidence Evidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			return nil, err
		}
		store.Put(name, evidence)
	}
	return store, nil
}

func mapEvidenceStore(store *EvidenceStore) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(store.byName))
	for name, evidence := range store.byName {
		raw, err := json.Marshal(evidence)
		if err != nil {
			continue
		}
		out[name] = raw
	}
	return out
}

func applyEvidenceDelta(store *EvidenceStore, delta map[string]json.RawMessage) error {
	for name, raw := range delta {
		var evidence Evidence
		if err := json.Unmarshal(raw, &evidence); err != nil {
			return err
		}
		store.Put(name, evidence)
	}
	return nil
}

func (t Tool) assembleCompiledValidationResult(payload compiledRunPayload, store *EvidenceStore, stepResults []StepResult, startedAt, completedAt time.Time) (Result, error) {
	result := Result{
		ToolName:    t.Name,
		Phase:       payload.PhaseName,
		Scope:       payload.Scope,
		Steps:       stepResults,
		Evidence:    store.All(),
		StartedAt:   startedAt,
		CompletedAt: completedAt,
	}
	result.Status, result.Outcome, result.Blocking = classifyResult(stepResults)
	result.Diagnostics = buildDiagnostics(stepResults, result.Evidence)
	result.Recommendations = collectRecommendations(stepResults)
	result.Summary = fmt.Sprintf("phase %q completed with status %s", payload.PhaseName, result.Status)
	return result, nil
}

func (t Tool) resolvePhase(params parameters.ParamValues) (compiledPhase, error) {
	phaseName := extractStringParam(params, "phase")
	if len(t.compiled.order) == 1 {
		name := t.compiled.order[0]
		if phaseName == "" || phaseName == name {
			return t.compiled.phases[name], nil
		}
		return compiledPhase{}, fmt.Errorf("unknown phase %q", phaseName)
	}
	if phaseName == "" {
		return compiledPhase{}, fmt.Errorf("missing required parameter 'phase'")
	}
	phase, ok := t.compiled.phases[phaseName]
	if !ok {
		return compiledPhase{}, fmt.Errorf("unknown phase %q", phaseName)
	}
	return phase, nil
}

func (t Tool) resolveScope(ctx context.Context, resourceMgr tools.SourceProvider, params parameters.ParamValues) (ResultScope, map[string][]ResolvedTarget, error) {
	if t.Source != "" {
		src, ok := resourceMgr.GetSource(t.Source)
		if !ok {
			return ResultScope{}, nil, fmt.Errorf("source %q not found", t.Source)
		}
		target := ResolvedTarget{
			DeviceID:     extractDevice(t.Source),
			SourceName:   t.Source,
			Labels:       nil,
			GroupNames:   []string{"all"},
			Transport:    templateOf(t.Source),
			Capabilities: getCapabilities(src),
		}
		groups := map[string][]ResolvedTarget{
			"":    {target},
			"all": {target},
		}
		return ResultScope{Source: t.Source, Devices: []string{target.DeviceID}}, groups, nil
	}

	matchLabels := cloneMap(t.SourceSelector.MatchLabels)
	if t.SourceSelector.Template != "" {
		matchLabels["template"] = t.SourceSelector.Template
	}
	srcs, err := resourceMgr.GetSourcesByLabels(ctx, matchLabels)
	if err != nil {
		return ResultScope{}, nil, err
	}
	if len(srcs) == 0 {
		return ResultScope{}, nil, fmt.Errorf("sourceSelector matched 0 devices")
	}

	deviceParam := extractStringParam(params, "device")
	poolLabels := resourceMgr.GetDevicePoolLabels()
	base := make([]ResolvedTarget, 0, len(srcs))
	for name, src := range srcs {
		deviceID := extractDevice(name)
		if deviceParam != "" && deviceID != deviceParam {
			continue
		}
		base = append(base, ResolvedTarget{
			DeviceID:     deviceID,
			SourceName:   name,
			Labels:       cloneMap(poolLabels[name]),
			Transport:    templateOf(name),
			Capabilities: getCapabilities(src),
		})
	}
	if len(base) == 0 {
		return ResultScope{}, nil, fmt.Errorf("device %q not found among matched sources", deviceParam)
	}
	sort.Slice(base, func(i, j int) bool { return base[i].SourceName < base[j].SourceName })
	groups := t.resolveGroups(base)
	return ResultScope{
		Selector: cloneMap(t.SourceSelector.MatchLabels),
		Devices:  uniqueDevices(base),
	}, groups, nil
}

func (t Tool) resolveGroups(base []ResolvedTarget) map[string][]ResolvedTarget {
	groups := map[string][]ResolvedTarget{
		"":    slices.Clone(base),
		"all": slices.Clone(base),
	}
	for name, group := range t.Groups {
		filtered := make([]ResolvedTarget, 0)
		for _, target := range base {
			if !matchesGroupTarget(target, group) {
				continue
			}
			next := target
			next.GroupNames = appendUnique(next.GroupNames, name)
			filtered = append(filtered, next)
		}
		groups[name] = filtered
	}
	return groups
}

func matchesGroupTarget(target ResolvedTarget, group TargetGroup) bool {
	if len(group.Devices) > 0 && !slices.Contains(group.Devices, target.DeviceID) {
		return false
	}
	if group.Template != "" && group.Template != target.Transport {
		return false
	}
	if len(group.MatchLabels) > 0 && !matchesAllLabels(target.Labels, group.MatchLabels) {
		return false
	}
	return true
}

func appendUnique(in []string, val string) []string {
	if slices.Contains(in, val) {
		return in
	}
	return append(in, val)
}

func getCapabilities(source sources.Source) capabilities.SourceCapabilities {
	if cp, ok := source.(capabilities.CapabilityProvider); ok {
		return cp.Capabilities()
	}
	return capabilities.SourceCapabilities{}
}

func (t Tool) runStep(ctx context.Context, resourceMgr tools.SourceProvider, step compiledStep, groups map[string][]ResolvedTarget, store *EvidenceStore) (StepResult, error) {
	if step.collect != nil {
		return t.runCollectStep(ctx, resourceMgr, step, groups, store)
	}
	return t.runAssertStep(ctx, step, store)
}

func (t Tool) runCompiledPlanStep(ctx context.Context, resourceMgr tools.SourceProvider, step compiledRunStep, store *EvidenceStore) (StepResult, map[string]json.RawMessage, collectFailureCategory, error) {
	if step.Collect != nil {
		attempt, err := t.collectEvidenceFromPlans(ctx, resourceMgr, step.Collect)
		if err != nil {
			return StepResult{}, nil, collectFailureNone, err
		}
		delta := map[string]json.RawMessage{}
		raw, err := json.Marshal(attempt.Evidence)
		if err != nil {
			return StepResult{}, nil, collectFailureNone, err
		}
		delta[step.Collect.Spec.Into] = raw
		return StepResult{
			Name:           step.Name,
			Kind:           StepKindCollect,
			Status:         attempt.Status,
			Code:           attempt.Code,
			Summary:        fmt.Sprintf("collected %d records into %q", attempt.Evidence.Summary.Total, step.Collect.Spec.Into),
			Groups:         slices.Clone(step.Collect.Spec.Targets),
			Recommendation: attempt.Recommendation,
			Attempts:       1,
		}, delta, attempt.FailureCategory, nil
	}
	result, err := t.runCompiledAssertStep(ctx, step, store)
	return result, nil, collectFailureNone, err
}

func (t Tool) runCollectStep(ctx context.Context, resourceMgr tools.SourceProvider, step compiledStep, groups map[string][]ResolvedTarget, store *EvidenceStore) (StepResult, error) {
	attempt, err := t.collectEvidence(ctx, resourceMgr, step.collect, groups)
	if err != nil {
		return StepResult{}, err
	}
	store.Put(step.collect.spec.Into, attempt.Evidence)
	return StepResult{
		Name:           step.Name,
		Kind:           StepKindCollect,
		Status:         attempt.Status,
		Code:           attempt.Code,
		Summary:        fmt.Sprintf("collected %d records into %q", attempt.Evidence.Summary.Total, step.collect.spec.Into),
		Groups:         slices.Clone(step.collect.spec.Targets),
		Recommendation: attempt.Recommendation,
		Attempts:       1,
	}, nil
}

func (t Tool) collectEvidenceFromPlans(ctx context.Context, resourceMgr tools.SourceProvider, cc *compiledRunCollectStep) (collectAttemptResult, error) {
	evidence := Evidence{Name: cc.Spec.Into}
	sourceNames := make([]string, 0, len(cc.Plans))
	planBySource := make(map[string]compiledPlanEntry, len(cc.Plans))
	allRetryable := true
	for _, plan := range cc.Plans {
		if plan.Target.SourceName == "" {
			evidence.Records = append(evidence.Records, EvidenceRecord{
				DeviceID:      plan.Target.DeviceID,
				Labels:        cloneMap(plan.Target.Labels),
				Groups:        slices.Clone(plan.Target.GroupNames),
				Transport:     plan.Target.Transport,
				SelectedBy:    plan.SelectedBy,
				SelectionNote: plan.SelectionNote,
				Error:         plan.SelectionNote,
			})
			evidence.Summary.Failed++
			allRetryable = false
			continue
		}
		sourceNames = append(sourceNames, plan.Target.SourceName)
		planBySource[plan.Target.SourceName] = plan
	}
	if len(sourceNames) > 0 {
		result := fanout.Execute(ctx, sourceNames, t.maxConcurrency(), func(ctx context.Context, sourceName string) (any, error) {
			plan := planBySource[sourceName]
			rawSource, ok := resourceMgr.GetSource(sourceName)
			if !ok {
				return nil, fmt.Errorf("source %q not found", sourceName)
			}
			exec := t.baseExecutor
			if len(cc.Spec.Transforms) > 0 {
				exec = exec.WithTransforms(query.TransformSet(cc.Spec.Transforms))
			}
			switch cc.Mode {
			case modeCommand:
				record, err := exec.ExecuteCommand(ctx, rawSource, cc.Spec.Command, sourceName)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", plan.Target.DeviceID, err)
				}
				return record, nil
			default:
				return exec.Execute(ctx, rawSource, cc.Spec.Operation, sourceName)
			}
		})
		if ctx.Err() != nil {
			return collectAttemptResult{}, ctx.Err()
		}
		for _, item := range result.Results {
			plan := planBySource[sourceNameForDevice(item.Device, sourceNames)]
			rec := EvidenceRecord{
				DeviceID:      item.Device,
				SourceName:    plan.Target.SourceName,
				Labels:        cloneMap(plan.Target.Labels),
				Groups:        slices.Clone(plan.Target.GroupNames),
				Transport:     plan.Target.Transport,
				SelectedBy:    plan.SelectedBy,
				SelectionNote: plan.SelectionNote,
			}
			if item.Status == "success" {
				record, ok := item.Data.(*models.Record)
				if !ok {
					return collectAttemptResult{}, fmt.Errorf("unexpected collect result type %T", item.Data)
				}
				rec.Record = record
				evidence.Summary.Succeeded++
			} else {
				rec.Error = item.Error
				evidence.Summary.Failed++
				if classifyCollectFailure(item.Error) != collectFailureRetryable {
					allRetryable = false
				}
			}
			evidence.Records = append(evidence.Records, rec)
		}
	}
	evidence.Summary.Total = len(evidence.Records)
	sort.Slice(evidence.Records, func(i, j int) bool { return evidence.Records[i].SourceName < evidence.Records[j].SourceName })
	switch {
	case evidence.Summary.Failed == 0:
		return collectAttemptResult{Evidence: evidence, Status: StatusPass}, nil
	case evidence.Summary.Succeeded == 0:
		failureCategory := collectFailureNonRetryable
		if allRetryable {
			failureCategory = collectFailureRetryable
		}
		return collectAttemptResult{
			Evidence:        evidence,
			Status:          StatusFail,
			Code:            "collection_failed",
			Recommendation:  "verify transport and source capabilities for this step",
			FailureCategory: failureCategory,
		}, nil
	default:
		return collectAttemptResult{
			Evidence:        evidence,
			Status:          StatusPartial,
			Code:            "collection_partial",
			Recommendation:  "review failed collection records before continuing",
			FailureCategory: collectFailureNonRetryable,
		}, nil
	}
}

func (t Tool) collectEvidence(ctx context.Context, resourceMgr tools.SourceProvider, cc *compiledCollect, groups map[string][]ResolvedTarget) (collectAttemptResult, error) {
	targets, err := selectTargets(cc.spec.Targets, groups)
	if err != nil {
		return collectAttemptResult{}, err
	}
	if len(targets) == 0 {
		return collectAttemptResult{}, fmt.Errorf("no targets resolved for evidence %q", cc.spec.Into)
	}

	plans := buildTransportPlans(targets, cc)
	sourceNames := make([]string, 0, len(plans))
	planBySource := make(map[string]transportPlan, len(plans))
	evidence := Evidence{Name: cc.spec.Into}
	allRetryable := true
	for _, plan := range plans {
		if plan.target.SourceName == "" {
			evidence.Records = append(evidence.Records, EvidenceRecord{
				DeviceID:      plan.target.DeviceID,
				Labels:        cloneMap(plan.target.Labels),
				Groups:        slices.Clone(plan.target.GroupNames),
				Transport:     plan.target.Transport,
				SelectedBy:    plan.selectedBy,
				SelectionNote: plan.selectionNote,
				Error:         plan.selectionNote,
			})
			evidence.Summary.Failed++
			allRetryable = false
			continue
		}
		sourceNames = append(sourceNames, plan.target.SourceName)
		planBySource[plan.target.SourceName] = plan
	}

	if len(sourceNames) > 0 {
		result := fanout.Execute(ctx, sourceNames, t.maxConcurrency(), func(ctx context.Context, sourceName string) (any, error) {
			plan := planBySource[sourceName]
			rawSource, ok := resourceMgr.GetSource(sourceName)
			if !ok {
				return nil, fmt.Errorf("source %q not found", sourceName)
			}
			exec := t.baseExecutor
			if len(cc.spec.Transforms) > 0 {
				exec = exec.WithTransforms(query.TransformSet(cc.spec.Transforms))
			}
			switch cc.mode {
			case modeCommand:
				record, err := exec.ExecuteCommand(ctx, rawSource, cc.spec.Command, sourceName)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", plan.target.DeviceID, err)
				}
				return record, nil
			default:
				return exec.Execute(ctx, rawSource, cc.spec.Operation, sourceName)
			}
		})
		if ctx.Err() != nil {
			return collectAttemptResult{}, ctx.Err()
		}

		for _, item := range result.Results {
			plan := planBySource[sourceNameForDevice(item.Device, sourceNames)]
			rec := EvidenceRecord{
				DeviceID:      item.Device,
				SourceName:    plan.target.SourceName,
				Labels:        cloneMap(plan.target.Labels),
				Groups:        slices.Clone(plan.target.GroupNames),
				Transport:     plan.target.Transport,
				SelectedBy:    plan.selectedBy,
				SelectionNote: plan.selectionNote,
			}
			if item.Status == "success" {
				record, ok := item.Data.(*models.Record)
				if !ok {
					return collectAttemptResult{}, fmt.Errorf("unexpected collect result type %T", item.Data)
				}
				rec.Record = record
				evidence.Summary.Succeeded++
			} else {
				rec.Error = item.Error
				evidence.Summary.Failed++
				if classifyCollectFailure(item.Error) != collectFailureRetryable {
					allRetryable = false
				}
			}
			evidence.Records = append(evidence.Records, rec)
		}
	}

	evidence.Summary.Total = len(evidence.Records)
	sort.Slice(evidence.Records, func(i, j int) bool { return evidence.Records[i].SourceName < evidence.Records[j].SourceName })

	switch {
	case evidence.Summary.Failed == 0:
		return collectAttemptResult{Evidence: evidence, Status: StatusPass}, nil
	case evidence.Summary.Succeeded == 0:
		failureCategory := collectFailureNonRetryable
		if allRetryable {
			failureCategory = collectFailureRetryable
		}
		return collectAttemptResult{
			Evidence:        evidence,
			Status:          StatusFail,
			Code:            "collection_failed",
			Recommendation:  "verify transport and source capabilities for this step",
			FailureCategory: failureCategory,
		}, nil
	default:
		return collectAttemptResult{
			Evidence:        evidence,
			Status:          StatusPartial,
			Code:            "collection_partial",
			Recommendation:  "review failed collection records before continuing",
			FailureCategory: collectFailureNonRetryable,
		}, nil
	}
}

func buildTransportPlans(targets []ResolvedTarget, cc *compiledCollect) []transportPlan {
	byDevice := map[string][]ResolvedTarget{}
	for _, target := range targets {
		byDevice[target.DeviceID] = append(byDevice[target.DeviceID], target)
	}
	plans := make([]transportPlan, 0, len(byDevice))
	for deviceID, candidates := range byDevice {
		plan := selectTransportPlan(deviceID, candidates, cc)
		plans = append(plans, plan)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].target.DeviceID < plans[j].target.DeviceID })
	return plans
}

func serializePlans(in []transportPlan) []compiledPlanEntry {
	out := make([]compiledPlanEntry, 0, len(in))
	for _, plan := range in {
		out = append(out, compiledPlanEntry{
			Target:        plan.target,
			SelectedBy:    plan.selectedBy,
			SelectionNote: plan.selectionNote,
		})
	}
	return out
}

func selectTransportPlan(deviceID string, candidates []ResolvedTarget, cc *compiledCollect) transportPlan {
	filtered := make([]ResolvedTarget, 0, len(candidates))
	for _, candidate := range candidates {
		if !collectModeCompatible(cc.mode, candidate) {
			continue
		}
		if len(cc.transport.Require) > 0 && !slices.Contains(cc.transport.Require, candidate.Transport) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return transportPlan{
			target:        ResolvedTarget{DeviceID: deviceID},
			selectedBy:    "transport_policy",
			selectionNote: fmt.Sprintf("no compatible source found for mode=%s require=%v", cc.mode, cc.transport.Require),
		}
	}

	sort.Slice(filtered, func(i, j int) bool {
		return transportRank(filtered[i].Transport, cc.transport.Prefer) < transportRank(filtered[j].Transport, cc.transport.Prefer)
	})
	chosen := filtered[0]
	selectedBy := "transport_policy"
	note := fmt.Sprintf("selected %s for mode=%s", chosen.Transport, cc.mode)
	return transportPlan{target: chosen, selectedBy: selectedBy, selectionNote: note}
}

func collectModeCompatible(mode collectionMode, target ResolvedTarget) bool {
	switch mode {
	case modeCommand:
		return target.Capabilities.CLI || target.Transport == "ssh"
	default:
		return true
	}
}

func transportRank(transport string, prefer []string) int {
	for i, pref := range prefer {
		if pref == transport {
			return i
		}
	}
	switch transport {
	case "netconf":
		return len(prefer) + 0
	case "gnmi":
		return len(prefer) + 1
	case "ssh":
		return len(prefer) + 2
	default:
		return len(prefer) + 3
	}
}

func sourceNameForDevice(device string, sourceNames []string) string {
	for _, name := range sourceNames {
		if extractDevice(name) == device {
			return name
		}
	}
	return device
}

func (t Tool) runAssertStep(ctx context.Context, step compiledStep, store *EvidenceStore) (StepResult, error) {
	startedAt := time.Now()
	passStreak := 0
	observedFailures := 0
	var firstPassAt *time.Time
	var lastPassedAt *time.Time
	var lastActual any
	var last AssertionResult
	maxAttempts := 1
	if step.converge != nil {
		maxAttempts = step.converge.maxAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := evaluateAssertion(&step.assert.spec, store)
		if err != nil {
			return StepResult{}, err
		}
		last = result
		lastActual = result.Actual
		if result.Status == StatusPass {
			passStreak++
			now := time.Now()
			if firstPassAt == nil {
				firstPassAt = &now
			}
			lastPassedAt = &now
			if step.converge == nil || convergenceSatisfied(step.converge, passStreak, firstPassAt, now) {
				return StepResult{
					Name:       step.Name,
					Kind:       StepKindAssert,
					Status:     result.Status,
					Summary:    fmt.Sprintf("assertion %q completed with status %s", result.Name, result.Status),
					Assertions: []AssertionResult{result},
					Attempts:   attempt,
					Convergence: &ConvergenceResult{
						Met:              true,
						Attempts:         attempt,
						PassStreak:       passStreak,
						ObservedFailures: observedFailures,
						StartedAt:        startedAt,
						CompletedAt:      now,
						LastPassedAt:     lastPassedAt,
						LastActual:       lastActual,
					},
				}, nil
			}
		} else {
			passStreak = 0
			firstPassAt = nil
			observedFailures++
		}

		if attempt < maxAttempts {
			wait := intervalForAttempt(step.converge, attempt)
			if !sleepWithContext(ctx, wait) {
				break
			}
		}
	}

	outcome := OutcomeBlocked
	if last.Severity != SeverityError {
		outcome = OutcomeConverging
	}
	_ = outcome
	return StepResult{
		Name:       step.Name,
		Kind:       StepKindAssert,
		Status:     last.Status,
		Code:       last.Code,
		Summary:    fmt.Sprintf("assertion %q completed with status %s", last.Name, last.Status),
		Assertions: []AssertionResult{last},
		Attempts:   maxAttempts,
		Convergence: &ConvergenceResult{
			Met:              false,
			Attempts:         maxAttempts,
			PassStreak:       passStreak,
			ObservedFailures: observedFailures,
			StartedAt:        startedAt,
			CompletedAt:      time.Now(),
			LastPassedAt:     lastPassedAt,
			LastActual:       lastActual,
			TimeoutReached:   step.converge != nil,
		},
	}, nil
}

func (t Tool) runCompiledAssertStep(ctx context.Context, step compiledRunStep, store *EvidenceStore) (StepResult, error) {
	startedAt := time.Now()
	passStreak := 0
	observedFailures := 0
	var firstPassAt *time.Time
	var lastPassedAt *time.Time
	var lastActual any
	var last AssertionResult
	maxAttempts := 1
	compiledConverge := step.Converge.toCompiled()
	if compiledConverge != nil {
		maxAttempts = compiledConverge.maxAttempts
	}

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := evaluateAssertion(&step.Assert.Spec, store)
		if err != nil {
			return StepResult{}, err
		}
		last = result
		lastActual = result.Actual
		if result.Status == StatusPass {
			passStreak++
			now := time.Now()
			if firstPassAt == nil {
				firstPassAt = &now
			}
			lastPassedAt = &now
			if compiledConverge == nil || convergenceSatisfied(compiledConverge, passStreak, firstPassAt, now) {
				return StepResult{
					Name:       step.Name,
					Kind:       StepKindAssert,
					Status:     result.Status,
					Summary:    fmt.Sprintf("assertion %q completed with status %s", result.Name, result.Status),
					Assertions: []AssertionResult{result},
					Attempts:   attempt,
					Convergence: &ConvergenceResult{
						Met:              true,
						Attempts:         attempt,
						PassStreak:       passStreak,
						ObservedFailures: observedFailures,
						StartedAt:        startedAt,
						CompletedAt:      now,
						LastPassedAt:     lastPassedAt,
						LastActual:       lastActual,
					},
				}, nil
			}
		} else {
			passStreak = 0
			firstPassAt = nil
			observedFailures++
		}
		if attempt < maxAttempts {
			wait := intervalForAttempt(compiledConverge, attempt)
			if !sleepWithContext(ctx, wait) {
				break
			}
		}
	}

	return StepResult{
		Name:       step.Name,
		Kind:       StepKindAssert,
		Status:     last.Status,
		Code:       last.Code,
		Summary:    fmt.Sprintf("assertion %q completed with status %s", last.Name, last.Status),
		Assertions: []AssertionResult{last},
		Attempts:   maxAttempts,
		Convergence: &ConvergenceResult{
			Met:              false,
			Attempts:         maxAttempts,
			PassStreak:       passStreak,
			ObservedFailures: observedFailures,
			StartedAt:        startedAt,
			CompletedAt:      time.Now(),
			LastPassedAt:     lastPassedAt,
			LastActual:       lastActual,
			TimeoutReached:   compiledConverge != nil,
		},
	}, nil
}

func (t Tool) runCompiledAssertAttempt(step compiledRunStep, store *EvidenceStore, input validation.StepExecutionInput) (StepResult, *validation.ConvergenceState, string, bool, error) {
	_ = input.Attempt
	now := time.Now()
	result, err := evaluateAssertion(&step.Assert.Spec, store)
	if err != nil {
		return StepResult{}, nil, "", false, err
	}
	conv := step.Converge.toCompiled()
	state := cloneConvergenceState(input.ConvergenceState)
	if state == nil {
		state = &validation.ConvergenceState{StartedAt: now}
	}
	if state.StartedAt.IsZero() {
		state.StartedAt = now
	}
	state.Attempt = input.Attempt
	state.LastActual = marshalActual(result.Actual)
	if result.Status == StatusPass {
		state.PassStreak++
		if state.FirstPassAt == nil {
			first := now
			state.FirstPassAt = &first
		}
		last := now
		state.LastPassedAt = &last
	} else {
		state.PassStreak = 0
		state.FirstPassAt = nil
		state.ObservedFailures++
	}
	completed := conv == nil || convergenceSatisfied(conv, state.PassStreak, state.FirstPassAt, now)
	timeoutReached := conv != nil && !completed && input.Attempt >= conv.maxAttempts
	state.Completed = completed || timeoutReached
	state.TimeoutReached = timeoutReached
	stepResult := StepResult{
		Name:       step.Name,
		Kind:       StepKindAssert,
		Status:     result.Status,
		Code:       result.Code,
		Summary:    fmt.Sprintf("assertion %q completed with status %s", result.Name, result.Status),
		Assertions: []AssertionResult{result},
		Attempts:   input.Attempt,
	}
	if state.Completed {
		stepResult.Convergence = buildConvergenceResult(state, result.Actual, completed, now)
		return stepResult, state, "", true, nil
	}
	stepResult.Convergence = buildConvergenceResult(state, result.Actual, false, now)
	return stepResult, state, intervalForAttempt(conv, input.Attempt).String(), false, nil
}

func buildConvergenceResult(state *validation.ConvergenceState, actual any, met bool, now time.Time) *ConvergenceResult {
	if state == nil {
		return nil
	}
	return &ConvergenceResult{
		Met:              met,
		Attempts:         state.Attempt,
		PassStreak:       state.PassStreak,
		ObservedFailures: state.ObservedFailures,
		StartedAt:        state.StartedAt,
		CompletedAt:      now,
		LastPassedAt:     state.LastPassedAt,
		LastActual:       actual,
		TimeoutReached:   state.TimeoutReached,
	}
}

func cloneConvergenceState(in *validation.ConvergenceState) *validation.ConvergenceState {
	if in == nil {
		return nil
	}
	out := *in
	if in.FirstPassAt != nil {
		first := *in.FirstPassAt
		out.FirstPassAt = &first
	}
	if in.LastPassedAt != nil {
		last := *in.LastPassedAt
		out.LastPassedAt = &last
	}
	if in.LastActual != nil {
		out.LastActual = append(json.RawMessage(nil), in.LastActual...)
	}
	return &out
}

func marshalActual(actual any) json.RawMessage {
	if actual == nil {
		return nil
	}
	raw, err := json.Marshal(actual)
	if err != nil {
		return nil
	}
	return raw
}

func (c *compiledRunConverge) toCompiled() *compiledConverge {
	if c == nil {
		return nil
	}
	interval, _ := time.ParseDuration(c.Interval)
	timeout, _ := time.ParseDuration(c.Timeout)
	stabilizeFor, _ := time.ParseDuration(c.StabilizeFor)
	return &compiledConverge{
		until:        c.Until,
		interval:     interval,
		timeout:      timeout,
		maxAttempts:  c.MaxAttempts,
		backoff:      c.Backoff,
		stabilizeFor: stabilizeFor,
		minPasses:    c.MinPasses,
	}
}

func convergenceSatisfied(conv *compiledConverge, passStreak int, firstPassAt *time.Time, now time.Time) bool {
	if conv == nil {
		return true
	}
	if passStreak < conv.minPasses {
		return false
	}
	if conv.stabilizeFor > 0 {
		if firstPassAt == nil {
			return false
		}
		if now.Sub(*firstPassAt) < conv.stabilizeFor {
			return false
		}
	}
	return true
}

func intervalForAttempt(conv *compiledConverge, attempt int) time.Duration {
	if conv == nil || conv.interval <= 0 {
		return 0
	}
	if conv.backoff == "linear" {
		return time.Duration(attempt) * conv.interval
	}
	return conv.interval
}

func withCollectAttemptTimeout(ctx context.Context, retry *compiledRunRetry) (context.Context, context.CancelFunc) {
	if retry == nil || strings.TrimSpace(retry.Timeout) == "" {
		return context.WithCancel(ctx)
	}
	timeout, err := time.ParseDuration(retry.Timeout)
	if err != nil || timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func collectRetryInterval(retry *compiledRunRetry) string {
	if retry == nil {
		return ""
	}
	return retry.Interval
}

func collectAttemptRemaining(retry *compiledRunRetry, attempt int) bool {
	if retry == nil {
		return false
	}
	maxAttempts := retry.MaxAttempts
	if maxAttempts <= 1 {
		return false
	}
	return attempt < maxAttempts
}

func classifyCollectFailure(msg string) collectFailureCategory {
	lower := strings.ToLower(strings.TrimSpace(msg))
	switch {
	case lower == "":
		return collectFailureNonRetryable
	case strings.Contains(lower, "context canceled"),
		strings.Contains(lower, "context cancelled"),
		strings.Contains(lower, "operation was canceled"),
		strings.Contains(lower, "operation was cancelled"):
		return collectFailureNone
	case strings.Contains(lower, "deadline exceeded"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "i/o timeout"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "broken pipe"),
		strings.Contains(lower, "network is unreachable"),
		strings.Contains(lower, "no route to host"),
		strings.Contains(lower, "eof"),
		strings.Contains(lower, "transport is closing"),
		strings.Contains(lower, "temporarily unavailable"),
		strings.Contains(lower, "unavailable"):
		return collectFailureRetryable
	case strings.Contains(lower, "permission denied"),
		strings.Contains(lower, "unable to authenticate"),
		strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "no supported methods remain"),
		strings.Contains(lower, "knownhosts"),
		strings.Contains(lower, "host key"),
		strings.Contains(lower, "not found"),
		strings.Contains(lower, "does not implement"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "unsupported"),
		strings.Contains(lower, "safety check failed"):
		return collectFailureNonRetryable
	default:
		return collectFailureNonRetryable
	}
}

func sleepWithContext(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func evaluateAssertion(spec *AssertSpec, store *EvidenceStore) (AssertionResult, error) {
	evidence, err := store.MustGetMany(spec.effectiveFrom())
	if err != nil {
		return AssertionResult{}, err
	}
	input, err := buildAssertionInput(spec, evidence, store)
	if err != nil {
		return AssertionResult{}, err
	}

	result := AssertionResult{
		Name:           spec.effectiveName(),
		Scope:          spec.effectiveScope(),
		Expression:     spec.effectiveExpression(),
		Expected:       spec.Expect,
		Severity:       spec.effectiveSeverity(),
		Recommendation: spec.Message,
	}

	switch spec.effectiveScope() {
	case ScopeAggregate:
		passed, actual, err := runCompiledAssertion(spec, input)
		if err != nil {
			return AssertionResult{}, err
		}
		result.Actual = actual
		if passed {
			result.Status = StatusPass
			result.Passed = 1
		} else {
			result.Status = StatusFail
			result.Failed = 1
			result.Code = failureCode(spec)
			result.Reason = "aggregate assertion failed"
		}
		return result, nil
	case ScopePerRecord:
		records := flattenEvidence(evidence)
		if len(records) == 0 {
			return AssertionResult{}, fmt.Errorf("assertion %q referenced empty evidence", result.Name)
		}
		for _, record := range records {
			passed, actual, err := runCompiledAssertion(spec, record)
			if err != nil {
				return AssertionResult{}, err
			}
			if passed {
				result.Passed++
				continue
			}
			result.Failed++
			result.Failures = append(result.Failures, AssertionFailure{
				DeviceID: recordString(record, "device_id"),
				Message:  "expression evaluated to false",
				Evidence: fmt.Sprintf("%v", actual),
			})
		}
		result.Status = passRateStatus(result.Passed, result.Failed, spec.effectiveMinPassRate())
		if result.Status != StatusPass {
			result.Code = failureCode(spec)
			result.Reason = "per-record assertion failed"
		}
		return result, nil
	case ScopePerDevice:
		byDevice := groupEvidenceByDevice(evidence)
		if len(byDevice) == 0 {
			return AssertionResult{}, fmt.Errorf("assertion %q referenced empty evidence", result.Name)
		}
		for deviceID, records := range byDevice {
			passed, actual, err := runCompiledAssertion(spec, map[string]any{
				"device_id": deviceID,
				"records":   records,
				"summary": map[string]any{
					"count": len(records),
				},
			})
			if err != nil {
				return AssertionResult{}, err
			}
			if passed {
				result.Passed++
				continue
			}
			result.Failed++
			result.Failures = append(result.Failures, AssertionFailure{
				DeviceID: deviceID,
				Message:  "expression evaluated to false",
				Evidence: fmt.Sprintf("%v", actual),
			})
		}
		result.Status = passRateStatus(result.Passed, result.Failed, spec.effectiveMinPassRate())
		if result.Status != StatusPass {
			result.Code = failureCode(spec)
			result.Reason = "per-device assertion failed"
		}
		return result, nil
	default:
		return AssertionResult{}, fmt.Errorf("unsupported assertion scope %q", spec.Scope)
	}
}

func buildAssertionInput(spec *AssertSpec, evidence []Evidence, store *EvidenceStore) (map[string]any, error) {
	input := buildAggregateInput(evidence)
	inputs := map[string]any{}
	for name, ref := range spec.Inputs {
		evs, err := resolveEvidenceRef(ref, store)
		if err != nil {
			return nil, err
		}
		inputs[ref.effectiveAlias(name)] = toJSONValue(flattenEvidence(evs))
	}
	comparisons := make([]any, 0, len(spec.Compare))
	for idx, cmp := range spec.Compare {
		left, err := resolveEvidenceRef(cmp.Left, store)
		if err != nil {
			return nil, err
		}
		right, err := resolveEvidenceRef(cmp.Right, store)
		if err != nil {
			return nil, err
		}
		lflat := flattenEvidence(left)
		rflat := flattenEvidence(right)
		if len(lflat) == 0 || len(rflat) == 0 {
			return nil, fmt.Errorf("comparison %q has an empty side", cmp.effectiveName(idx))
		}
		comparisons = append(comparisons, toJSONValue(map[string]any{
			"name":  cmp.effectiveName(idx),
			"left":  lflat,
			"right": rflat,
		}))
	}
	input["inputs"] = inputs
	input["comparisons"] = comparisons
	return input, nil
}

func buildAggregateInput(evidence []Evidence) map[string]any {
	evidenceMap := make(map[string]any, len(evidence))
	flattened := make([]any, 0)
	total := 0
	failed := 0
	for _, ev := range evidence {
		entries := make([]any, 0, len(ev.Records))
		for _, rec := range ev.Records {
			normalized := normalizeEvidenceRecord(rec)
			entries = append(entries, normalized)
			flattened = append(flattened, normalized)
		}
		evidenceMap[ev.Name] = entries
		total += ev.Summary.Total
		failed += ev.Summary.Failed
	}
	return map[string]any{
		"evidence": evidenceMap,
		"records":  flattened,
		"summary": map[string]any{
			"total":  total,
			"failed": failed,
			"passed": total - failed,
		},
	}
}

func resolveEvidenceRef(ref EvidenceRef, store *EvidenceStore) ([]Evidence, error) {
	ev, ok := store.Get(ref.Evidence)
	if !ok {
		return nil, fmt.Errorf("evidence %q not found", ref.Evidence)
	}
	if ref.Group == "" {
		return []Evidence{ev}, nil
	}
	filtered := Evidence{Name: ev.Name}
	for _, rec := range ev.Records {
		if slices.Contains(rec.Groups, ref.Group) {
			filtered.Records = append(filtered.Records, rec)
		}
	}
	filtered.Summary.Total = len(filtered.Records)
	for _, rec := range filtered.Records {
		if rec.Error == "" {
			filtered.Summary.Succeeded++
		} else {
			filtered.Summary.Failed++
		}
	}
	if len(filtered.Records) == 0 {
		return nil, fmt.Errorf("evidence %q has no records for group %q", ref.Evidence, ref.Group)
	}
	return []Evidence{filtered}, nil
}

func flattenEvidence(evidence []Evidence) []map[string]any {
	out := make([]map[string]any, 0)
	for _, ev := range evidence {
		for _, rec := range ev.Records {
			out = append(out, normalizeEvidenceRecord(rec))
		}
	}
	return out
}

func groupEvidenceByDevice(evidence []Evidence) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, ev := range evidence {
		for _, rec := range ev.Records {
			norm := normalizeEvidenceRecord(rec)
			deviceID := recordString(norm, "device_id")
			out[deviceID] = append(out[deviceID], norm)
		}
	}
	return out
}

func normalizeEvidenceRecord(rec EvidenceRecord) map[string]any {
	out := map[string]any{
		"device_id":      rec.DeviceID,
		"source_name":    rec.SourceName,
		"labels":         rec.Labels,
		"groups":         rec.Groups,
		"transport":      rec.Transport,
		"selected_by":    rec.SelectedBy,
		"selection_note": rec.SelectionNote,
		"error":          rec.Error,
	}
	if rec.Record == nil {
		return out
	}
	out["record_type"] = rec.Record.RecordType
	out["payload"] = toJSONValue(rec.Record.Payload)
	out["quality"] = toJSONValue(rec.Record.Quality)
	out["collection"] = toJSONValue(rec.Record.Collection)
	out["native"] = toJSONValue(rec.Record.Native)
	return out
}

func runCompiledAssertion(spec *AssertSpec, input any) (bool, any, error) {
	if spec.Primitive == nil {
		return runAssertionExpr(spec.Expr, input)
	}
	return evaluatePrimitiveAssertion(spec.Primitive, input)
}

func runAssertionExpr(expr string, input any) (bool, any, error) {
	query, err := gojq.Parse(expr)
	if err != nil {
		return false, nil, fmt.Errorf("invalid assertion expression %q: %w", expr, err)
	}
	iter := query.Run(input)
	v, ok := iter.Next()
	if !ok {
		return false, nil, nil
	}
	if err, ok := v.(error); ok {
		return false, nil, fmt.Errorf("assertion evaluation failed: %w", err)
	}
	b, ok := v.(bool)
	if !ok {
		return false, v, nil
	}
	return b, v, nil
}

func evaluatePrimitiveAssertion(check *AssertionCheck, input any) (bool, any, error) {
	values, err := extractPathValues(input, check.Path)
	if err != nil {
		return false, nil, err
	}
	if check.Filter != "" && check.Type != AssertionDeltaWithin {
		values, err = filterValues(values, check.Filter)
		if err != nil {
			return false, nil, err
		}
	}

	switch check.Type {
	case AssertionEquals:
		if len(values) == 0 {
			return false, nil, nil
		}
		return reflect.DeepEqual(values[0], check.Value), values[0], nil
	case AssertionNotEquals:
		if len(values) == 0 {
			return false, nil, nil
		}
		return !reflect.DeepEqual(values[0], check.Value), values[0], nil
	case AssertionExists:
		return len(values) > 0 && values[0] != nil, values, nil
	case AssertionCountEQ:
		return len(values) == derefInt(check.Count), len(values), nil
	case AssertionCountGTE:
		return len(values) >= derefInt(check.Count), len(values), nil
	case AssertionCountLTE:
		return len(values) <= derefInt(check.Count), len(values), nil
	case AssertionAllMatch:
		return len(values) > 0 && len(values) == countMatches(values, check.Filter), values, nil
	case AssertionAnyMatch:
		return countMatches(values, check.Filter) > 0, values, nil
	case AssertionNoneMatch:
		return countMatches(values, check.Filter) == 0, values, nil
	case AssertionSameAcross:
		if len(values) == 0 {
			return false, values, nil
		}
		first := values[0]
		for _, v := range values[1:] {
			if !reflect.DeepEqual(first, v) {
				return false, values, nil
			}
		}
		return true, values, nil
	case AssertionDeltaWithin:
		comparisons, ok := input.(map[string]any)["comparisons"].([]any)
		if !ok || len(comparisons) == 0 {
			return false, nil, fmt.Errorf("delta_within requires comparisons input")
		}
		comp, ok := comparisons[0].(map[string]any)
		if !ok {
			return false, nil, fmt.Errorf("invalid comparison input")
		}
		left, err := reduceValues(comp["left"], check.Path, check.Reduce)
		if err != nil {
			return false, nil, err
		}
		right, err := reduceValues(comp["right"], check.Path, check.Reduce)
		if err != nil {
			return false, nil, err
		}
		delta := left - right
		if delta < 0 {
			delta = -delta
		}
		return delta <= derefFloat(check.Tolerance), delta, nil
	default:
		return false, nil, fmt.Errorf("unsupported primitive %q", check.Type)
	}
}

func reduceValues(input any, path string, reduce AssertionReduce) (float64, error) {
	values, err := extractPathValues(input, path)
	if err != nil {
		return 0, err
	}
	switch reduce {
	case ReduceCount:
		return float64(len(values)), nil
	case ReduceFirst:
		if len(values) == 0 {
			return 0, nil
		}
		return toFloat(values[0]), nil
	case ReduceSum:
		var total float64
		for _, v := range values {
			total += toFloat(v)
		}
		return total, nil
	default:
		return 0, fmt.Errorf("unsupported reduce %q", reduce)
	}
}

func extractPathValues(input any, path string) ([]any, error) {
	if path == "" {
		switch vv := input.(type) {
		case []any:
			return vv, nil
		default:
			return []any{input}, nil
		}
	}
	query, err := gojq.Parse("." + path)
	if err != nil {
		return nil, fmt.Errorf("invalid path %q: %w", path, err)
	}
	iter := query.Run(input)
	out := []any{}
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			return nil, err
		}
		if arr, ok := v.([]any); ok {
			out = append(out, arr...)
		} else {
			out = append(out, v)
		}
	}
	return out, nil
}

func filterValues(values []any, filter string) ([]any, error) {
	out := make([]any, 0, len(values))
	for _, v := range values {
		ok, _, err := runAssertionExpr(filter, v)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, v)
		}
	}
	return out, nil
}

func countMatches(values []any, filter string) int {
	n := 0
	for _, v := range values {
		ok, _, err := runAssertionExpr(filter, v)
		if err == nil && ok {
			n++
		}
	}
	return n
}

func toFloat(v any) float64 {
	switch vv := v.(type) {
	case float64:
		return vv
	case int:
		return float64(vv)
	case int64:
		return float64(vv)
	case uint64:
		return float64(vv)
	default:
		return 0
	}
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func passRateStatus(passed, failed int, minPassRate float64) ValidationStatus {
	total := passed + failed
	if total == 0 {
		return StatusFail
	}
	if float64(passed)/float64(total) >= minPassRate {
		return StatusPass
	}
	return StatusFail
}

func classifyResult(steps []StepResult) (ValidationStatus, ValidationOutcome, bool) {
	hasCollectionFailure := false
	hasWarningFailure := false
	hasErrorFailure := false
	hasConvergingWarning := false

	for _, step := range steps {
		if step.Kind == StepKindCollect && step.Status != StatusPass {
			hasCollectionFailure = true
		}
		for _, assertion := range step.Assertions {
			if assertion.Status == StatusPass {
				continue
			}
			switch assertion.Severity {
			case SeverityWarning, SeverityInfo:
				hasWarningFailure = true
				if step.Convergence != nil && step.Convergence.TimeoutReached {
					hasConvergingWarning = true
				}
			default:
				hasErrorFailure = true
			}
		}
	}

	switch {
	case hasErrorFailure:
		return StatusFail, OutcomeBlocked, true
	case hasCollectionFailure:
		return StatusFail, OutcomeBlocked, true
	case hasConvergingWarning:
		return StatusPartial, OutcomeConverging, false
	case hasWarningFailure:
		return StatusPartial, OutcomeWarning, false
	default:
		return StatusPass, OutcomeHealthy, false
	}
}

func buildDiagnostics(steps []StepResult, evidence []Evidence) ResultDiagnostics {
	diag := ResultDiagnostics{TotalSteps: len(steps)}
	for _, step := range steps {
		switch step.Status {
		case StatusPass:
			diag.PassedSteps++
		case StatusFail:
			diag.FailedSteps++
		case StatusPartial:
			diag.WarningSteps++
		}
	}
	for _, ev := range evidence {
		diag.CollectedRecords += ev.Summary.Total
		diag.CollectionErrors += ev.Summary.Failed
	}
	return diag
}

func collectRecommendations(steps []StepResult) []string {
	out := []string{}
	for _, step := range steps {
		if step.Recommendation != "" && !slices.Contains(out, step.Recommendation) {
			out = append(out, step.Recommendation)
		}
		for _, assertion := range step.Assertions {
			if assertion.Recommendation != "" && !slices.Contains(out, assertion.Recommendation) {
				out = append(out, assertion.Recommendation)
			}
		}
	}
	return out
}

func failureCode(spec *AssertSpec) string {
	if spec == nil {
		return "assertion_failed"
	}
	if spec.Primitive != nil {
		return string(spec.Primitive.Type) + "_failed"
	}
	if spec.Name != "" {
		return spec.Name + "_failed"
	}
	return "assertion_failed"
}

func (spec *AssertSpec) effectiveScope() AssertionScope {
	if spec == nil || spec.Scope == "" {
		return ScopeAggregate
	}
	return spec.Scope
}

func (spec *AssertSpec) effectiveExpression() string {
	if spec == nil {
		return ""
	}
	if spec.Expr != "" {
		return spec.Expr
	}
	if spec.Primitive != nil {
		return string(spec.Primitive.Type)
	}
	return ""
}

func (spec *AssertSpec) effectiveName() string {
	if spec == nil || spec.Name == "" {
		return "assertion"
	}
	return spec.Name
}

func (spec *AssertSpec) effectiveFrom() []string {
	if spec == nil {
		return nil
	}
	return spec.From
}

func (spec *AssertSpec) effectiveMinPassRate() float64 {
	if spec == nil || spec.MinPassRate == nil {
		return 1.0
	}
	return *spec.MinPassRate
}

func (spec *AssertSpec) effectiveSeverity() ResultSeverity {
	if spec == nil || spec.Severity == "" {
		return SeverityError
	}
	return spec.Severity
}

func (ref EvidenceRef) effectiveAlias(name string) string {
	if ref.Alias != "" {
		return ref.Alias
	}
	return name
}

func (cmp EvidenceComparisonRef) effectiveName(idx int) string {
	if cmp.Name != "" {
		return cmp.Name
	}
	return fmt.Sprintf("comparison_%d", idx+1)
}

func selectTargets(names []string, groups map[string][]ResolvedTarget) ([]ResolvedTarget, error) {
	if len(names) == 0 {
		return groups[""], nil
	}
	seen := map[string]ResolvedTarget{}
	for _, name := range names {
		targets, ok := groups[name]
		if !ok {
			return nil, fmt.Errorf("unknown target group %q", name)
		}
		for _, target := range targets {
			seen[target.SourceName] = target
		}
	}
	out := make([]ResolvedTarget, 0, len(seen))
	for _, target := range seen {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SourceName < out[j].SourceName })
	return out, nil
}

func (t Tool) maxConcurrency() int {
	if t.Defaults != nil && t.Defaults.MaxConcurrency > 0 {
		return t.Defaults.MaxConcurrency
	}
	if t.SourceSelector != nil && t.SourceSelector.MaxConcurrency > 0 {
		return t.SourceSelector.MaxConcurrency
	}
	return fanout.DefaultMaxConcurrency
}

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

func templateOf(sourceName string) string {
	idx := strings.LastIndex(sourceName, "/")
	if idx >= 0 {
		return sourceName[idx+1:]
	}
	return sourceName
}

func matchesAllLabels(actual, wanted map[string]string) bool {
	for k, v := range wanted {
		if actual[k] != v {
			return false
		}
	}
	return true
}

func uniqueDevices(targets []ResolvedTarget) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		if seen[target.DeviceID] {
			continue
		}
		seen[target.DeviceID] = true
		out = append(out, target.DeviceID)
	}
	sort.Strings(out)
	return out
}

func cloneMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func toJSONValue(v any) any {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return v
	}
	return out
}

func recordString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

type resourceVersionProvider interface {
	GetResourceVersion() uint64
}

func resourceVersionFrom(resourceMgr tools.SourceProvider) uint64 {
	provider, ok := resourceMgr.(resourceVersionProvider)
	if !ok {
		return 0
	}
	return provider.GetResourceVersion()
}

func fingerprint(raw []byte) string {
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:])
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
