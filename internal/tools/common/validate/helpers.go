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
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
)

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
