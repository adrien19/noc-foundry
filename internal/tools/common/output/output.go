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

// Package output provides a shared result envelope for network tools,
// ensuring consistent response structure across all device tools.
package output

import (
	"github.com/adrien19/noc-foundry/internal/network/models"
)

// NewToolResult creates a standardised result map from a CommandResult.
// All network tools should use this to produce consistent output.
func NewToolResult(cr models.CommandResult) map[string]any {
	return map[string]any{
		"raw_output": cr.RawOutput,
		"metadata": map[string]string{
			"command": cr.Command,
			"tool":    cr.ToolKind,
			"source":  cr.Source,
		},
	}
}
