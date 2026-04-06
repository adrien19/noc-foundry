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

// Package parsers provides reusable primitives for parsing CLI output from
// network devices: line splitting, key-value extraction, and separator detection.
package parsers

import (
	"strings"
)

// Lines splits raw CLI output into trimmed, non-empty lines.
func Lines(raw string) []string {
	var result []string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimRight(line, "\r \t")
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

// ExtractKeyValue splits a line on the first occurrence of sep and returns
// the trimmed key and value. Returns false if the separator is not found.
func ExtractKeyValue(line, sep string) (string, string, bool) {
	idx := strings.Index(line, sep)
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+len(sep):])
	return key, val, true
}

// IsSeparatorLine returns true if the line consists entirely of the repeated
// character (e.g. '=' or '-'), ignoring leading/trailing whitespace.
func IsSeparatorLine(line string, char byte) bool {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) == 0 {
		return false
	}
	for i := range len(trimmed) {
		if trimmed[i] != char {
			return false
		}
	}
	return true
}

// NormalizeStatus converts vendor-specific status strings to the
// canonical UP/DOWN values used by OpenConfig semantics.
func NormalizeStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "up", "enable", "enabled":
		return "UP"
	case "down", "disable", "disabled":
		return "DOWN"
	default:
		return strings.ToUpper(strings.TrimSpace(raw))
	}
}
