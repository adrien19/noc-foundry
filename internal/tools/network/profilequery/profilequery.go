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

// Package profilequery contains shared plumbing for dedicated network tools
// whose implementation is "execute one schema/profile-routed operation".
package profilequery

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/network/fanout"
	"github.com/adrien19/noc-foundry/internal/network/query"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

// SourceSelector defines label-based device targeting for fleet operations.
type SourceSelector struct {
	MatchLabels    map[string]string `yaml:"matchLabels"`
	MaxConcurrency int               `yaml:"maxConcurrency,omitempty"`
	Template       string            `yaml:"template,omitempty"`
}

// ValidateConfig validates common source/sourceSelector constraints.
func ValidateConfig(toolName, kind, source string, selector *SourceSelector, srcs map[string]sources.Source) error {
	if source == "" && selector == nil {
		return fmt.Errorf("tool %q must specify either 'source' or 'sourceSelector'", toolName)
	}
	if source != "" && selector != nil {
		return fmt.Errorf("tool %q cannot specify both 'source' and 'sourceSelector'", toolName)
	}
	if source != "" {
		if rawS, ok := srcs[source]; ok {
			if _, ok := rawS.(capabilities.SourceIdentity); !ok {
				return fmt.Errorf("invalid source for %q tool: source %q does not expose vendor/platform identity", kind, source)
			}
		}
	}
	return nil
}

// Invoke executes opID against either a single source or a source selector.
func Invoke(ctx context.Context, resourceMgr tools.SourceProvider, executor *query.Executor, source string, selector *SourceSelector, opID string, params parameters.ParamValues) (any, util.NOCFoundryError) {
	if source != "" {
		return invokeSingle(ctx, resourceMgr, executor, source, opID)
	}
	return invokeWithSelector(ctx, resourceMgr, executor, selector, opID, params)
}

func invokeSingle(ctx context.Context, resourceMgr tools.SourceProvider, executor *query.Executor, sourceName, opID string) (any, util.NOCFoundryError) {
	rawSource, ok := resourceMgr.GetSource(sourceName)
	if !ok {
		return nil, util.NewClientServerError("unable to retrieve source", http.StatusInternalServerError, fmt.Errorf("source %q not found", sourceName))
	}
	record, err := executor.Execute(ctx, rawSource, opID, sourceName)
	if err != nil {
		return nil, util.NewClientServerError(fmt.Sprintf("failed to execute %s operation", opID), http.StatusInternalServerError, err)
	}
	return record, nil
}

func invokeWithSelector(ctx context.Context, resourceMgr tools.SourceProvider, executor *query.Executor, selector *SourceSelector, opID string, params parameters.ParamValues) (any, util.NOCFoundryError) {
	if selector == nil {
		return nil, util.NewClientServerError("missing source selector", http.StatusInternalServerError, fmt.Errorf("sourceSelector is nil"))
	}
	deviceName := paramString(params, "device")

	matchLabels := selector.MatchLabels
	if selector.Template != "" {
		merged := make(map[string]string, len(matchLabels)+1)
		for k, v := range matchLabels {
			merged[k] = v
		}
		merged["template"] = selector.Template
		matchLabels = merged
	}

	srcs, err := resourceMgr.GetSourcesByLabels(ctx, matchLabels)
	if err != nil {
		return nil, util.NewClientServerError("failed to resolve sources by selector", http.StatusInternalServerError, err)
	}
	if len(srcs) == 0 {
		return nil, util.NewClientServerError("no sources matched the selector", http.StatusNotFound, fmt.Errorf("sourceSelector matched 0 devices"))
	}
	srcs = pickBestPerDevice(srcs)

	if deviceName != "" {
		for sn := range filterByDevice(srcs, deviceName) {
			return invokeSingle(ctx, resourceMgr, executor, sn, opID)
		}
		return nil, util.NewClientServerError(fmt.Sprintf("device %q not found among matched sources", deviceName), http.StatusNotFound, fmt.Errorf("device %q not in selector results", deviceName))
	}

	sourceNames := make([]string, 0, len(srcs))
	for name := range srcs {
		sourceNames = append(sourceNames, name)
	}
	maxConc := fanout.DefaultMaxConcurrency
	if selector.MaxConcurrency > 0 {
		maxConc = selector.MaxConcurrency
	}
	return fanout.Execute(ctx, sourceNames, maxConc, func(ctx context.Context, sourceName string) (any, error) {
		rawSource, ok := resourceMgr.GetSource(sourceName)
		if !ok {
			return nil, fmt.Errorf("source %q not found", sourceName)
		}
		return executor.Execute(ctx, rawSource, opID, sourceName)
	}), nil
}

func paramString(params parameters.ParamValues, name string) string {
	for _, p := range params {
		if p.Name == name && p.Value != nil {
			if s, ok := p.Value.(string); ok {
				return s
			}
		}
	}
	return ""
}

func filterByDevice(srcs map[string]sources.Source, device string) map[string]sources.Source {
	filtered := make(map[string]sources.Source)
	for name := range srcs {
		if extractDevice(name) == device {
			filtered[name] = srcs[name]
		}
	}
	return filtered
}

func pickBestPerDevice(srcs map[string]sources.Source) map[string]sources.Source {
	best := make(map[string]sources.Source)
	bestName := make(map[string]string)
	for name, src := range srcs {
		device := extractDevice(name)
		if _, exists := best[device]; !exists || sourceRank(name) < sourceRank(bestName[device]) {
			best[device] = src
			bestName[device] = name
		}
	}
	out := make(map[string]sources.Source, len(best))
	for device, src := range best {
		out[bestName[device]] = src
	}
	return out
}

func extractDevice(sourceName string) string {
	parts := strings.Split(sourceName, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2]
	}
	return sourceName
}

func sourceRank(name string) int {
	switch {
	case strings.HasSuffix(name, "/gnmi"):
		return 0
	case strings.HasSuffix(name, "/netconf"):
		return 1
	case strings.HasSuffix(name, "/ssh"):
		return 2
	default:
		return 10
	}
}
