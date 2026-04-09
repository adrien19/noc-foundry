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

// Package validate implements the validate MCP tool, a read-only
// declarative validation engine for network devices and blast-radius checks.

package validate

import (
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/models"
	"github.com/adrien19/noc-foundry/internal/network/query"
)

// ---------------------------------------------------------------------------
// YAML config types
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Assert config types
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Result types (JSON output)
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Compiled (internal) types
// ---------------------------------------------------------------------------

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
