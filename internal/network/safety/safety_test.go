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

package safety_test

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/safety"
)

func TestValidateReadOnlyCommand(t *testing.T) {
	tcs := []struct {
		desc    string
		command string
		wantErr bool
	}{
		{desc: "show command ok", command: "show interface", wantErr: false},
		{desc: "display command ok", command: "display version", wantErr: false},
		{desc: "configure blocked", command: "configure terminal", wantErr: true},
		{desc: "delete blocked", command: "delete interface eth0", wantErr: true},
		{desc: "set blocked", command: "set system hostname foo", wantErr: true},
		{desc: "commit blocked", command: "commit", wantErr: true},
		{desc: "rollback blocked", command: "rollback", wantErr: true},
		{desc: "reboot blocked", command: "reboot now", wantErr: true},
		{desc: "shutdown blocked", command: "shutdown", wantErr: true},
		{desc: "no blocked", command: "no interface eth0", wantErr: true},
		{desc: "write blocked", command: "write memory", wantErr: true},
		{desc: "erase blocked", command: "erase startup-config", wantErr: true},
		{desc: "case insensitive", command: "Configure terminal", wantErr: true},
		{desc: "show with pipe ok", command: "show interface | grep Up", wantErr: false},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			err := safety.ValidateReadOnlyCommand(tc.command)
			if tc.wantErr && err == nil {
				t.Fatalf("expected error for command %q", tc.command)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error for command %q: %v", tc.command, err)
			}
		})
	}
}
