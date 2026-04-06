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

// Package safety provides guardrails for network device operations,
// including read-only command validation and secret redaction.
package safety

import (
	"fmt"
	"strings"
)

// dangerousKeywords are CLI tokens that indicate a potentially destructive
// or configuration-altering operation.
var dangerousKeywords = []string{
	"configure",
	"delete",
	"set ",
	"commit",
	"rollback",
	"reboot",
	"shutdown",
	"no ",
	"write ",
	"erase",
	"format",
	"reset",
}

// ValidateReadOnlyCommand returns an error if the command appears to be
// destructive or configuration-altering.
func ValidateReadOnlyCommand(command string) error {
	lower := strings.ToLower(strings.TrimSpace(command))
	for _, kw := range dangerousKeywords {
		if strings.HasPrefix(lower, kw) {
			return fmt.Errorf("command %q starts with potentially destructive keyword %q", command, strings.TrimSpace(kw))
		}
	}
	return nil
}
