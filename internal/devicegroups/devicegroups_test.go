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

package devicegroups

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func intPtr(i int) *int { return &i }

func TestExpandToSourceMaps(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		want    map[string]map[string]any
		wantErr bool
	}{
		{
			name: "basic expansion",
			config: Config{
				Name: "lab",
				Type: "static",
				SourceTemplates: map[string]map[string]any{
					"ssh": {
						"type":     "ssh",
						"port":     22,
						"username": "admin",
						"password": "pass",
						"vendor":   "nokia",
						"platform": "srlinux",
					},
				},
				Devices: []DeviceEntry{
					{Name: "spine-1", Host: "10.0.0.1"},
					{Name: "spine-2", Host: "10.0.0.2"},
				},
			},
			want: map[string]map[string]any{
				"lab/spine-1/ssh": {
					"type": "ssh", "port": 22, "username": "admin", "password": "pass",
					"vendor": "nokia", "platform": "srlinux",
					"name": "lab/spine-1/ssh", "host": "10.0.0.1",
				},
				"lab/spine-2/ssh": {
					"type": "ssh", "port": 22, "username": "admin", "password": "pass",
					"vendor": "nokia", "platform": "srlinux",
					"name": "lab/spine-2/ssh", "host": "10.0.0.2",
				},
			},
		},
		{
			name: "multi-template expansion",
			config: Config{
				Name: "lab",
				Type: "static",
				SourceTemplates: map[string]map[string]any{
					"ssh":  {"type": "ssh", "port": 22, "username": "admin"},
					"gnmi": {"type": "gnmi", "port": 57400, "username": "admin"},
				},
				Devices: []DeviceEntry{
					{Name: "r1", Host: "10.0.0.1"},
				},
			},
			want: map[string]map[string]any{
				"lab/r1/ssh":  {"type": "ssh", "port": 22, "username": "admin", "name": "lab/r1/ssh", "host": "10.0.0.1"},
				"lab/r1/gnmi": {"type": "gnmi", "port": 57400, "username": "admin", "name": "lab/r1/gnmi", "host": "10.0.0.1"},
			},
		},
		{
			name: "device overrides",
			config: Config{
				Name: "lab",
				Type: "static",
				SourceTemplates: map[string]map[string]any{
					"ssh": {"type": "ssh", "port": 22, "username": "admin", "password": "default"},
				},
				Devices: []DeviceEntry{
					{Name: "r1", Host: "10.0.0.1", Port: intPtr(2222), Username: "custom", Password: "secret"},
				},
			},
			want: map[string]map[string]any{
				"lab/r1/ssh": {
					"type": "ssh", "port": 2222, "username": "custom", "password": "secret",
					"name": "lab/r1/ssh", "host": "10.0.0.1",
				},
			},
		},
		{
			name: "no devices error",
			config: Config{
				Name:            "lab",
				SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}},
			},
			wantErr: true,
		},
		{
			name: "no templates error",
			config: Config{
				Name:    "lab",
				Devices: []DeviceEntry{{Name: "r1", Host: "10.0.0.1"}},
			},
			wantErr: true,
		},
		{
			name: "template missing type error",
			config: Config{
				Name:            "lab",
				SourceTemplates: map[string]map[string]any{"ssh": {"port": 22}},
				Devices:         []DeviceEntry{{Name: "r1", Host: "10.0.0.1"}},
			},
			wantErr: true,
		},
		{
			name: "device missing name error",
			config: Config{
				Name:            "lab",
				SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}},
				Devices:         []DeviceEntry{{Host: "10.0.0.1"}},
			},
			wantErr: true,
		},
		{
			name: "device missing host error",
			config: Config{
				Name:            "lab",
				SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}},
				Devices:         []DeviceEntry{{Name: "r1"}},
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.config.ExpandToSourceMaps()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ExpandToSourceMaps() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestBuildLabelIndex(t *testing.T) {
	config := Config{
		Name: "nokia-lab",
		Type: "static",
		SourceTemplates: map[string]map[string]any{
			"ssh":  {"type": "ssh", "vendor": "nokia", "platform": "srlinux"},
			"gnmi": {"type": "gnmi", "vendor": "nokia", "platform": "srlinux"},
		},
		Devices: []DeviceEntry{
			{Name: "spine-1", Host: "10.0.0.1", Labels: map[string]string{"role": "spine", "site": "east"}},
			{Name: "leaf-1", Host: "10.0.0.2", Labels: map[string]string{"role": "leaf", "site": "east"}},
		},
	}

	labels := config.BuildLabelIndex()

	// Check SSH source labels for spine-1
	sshLabels, ok := labels["nokia-lab/spine-1/ssh"]
	if !ok {
		t.Fatal("missing labels for nokia-lab/spine-1/ssh")
	}
	if sshLabels["group"] != "nokia-lab" {
		t.Errorf("group label = %q, want %q", sshLabels["group"], "nokia-lab")
	}
	if sshLabels["device"] != "spine-1" {
		t.Errorf("device label = %q, want %q", sshLabels["device"], "spine-1")
	}
	if sshLabels["template"] != "ssh" {
		t.Errorf("template label = %q, want %q", sshLabels["template"], "ssh")
	}
	if sshLabels["vendor"] != "nokia" {
		t.Errorf("vendor label = %q, want %q", sshLabels["vendor"], "nokia")
	}
	if sshLabels["role"] != "spine" {
		t.Errorf("role label = %q, want %q", sshLabels["role"], "spine")
	}
	if sshLabels["site"] != "east" {
		t.Errorf("site label = %q, want %q", sshLabels["site"], "east")
	}

	// Check total source count: 2 devices × 2 templates = 4
	if len(labels) != 4 {
		t.Errorf("label index has %d entries, want 4", len(labels))
	}
}

func TestLabelSelectorMatches(t *testing.T) {
	tests := []struct {
		name     string
		selector LabelSelector
		labels   map[string]string
		want     bool
	}{
		{
			name:     "empty selector matches everything",
			selector: LabelSelector{},
			labels:   map[string]string{"role": "spine"},
			want:     true,
		},
		{
			name:     "single label match",
			selector: LabelSelector{MatchLabels: map[string]string{"role": "spine"}},
			labels:   map[string]string{"role": "spine", "site": "east"},
			want:     true,
		},
		{
			name:     "multi-label match",
			selector: LabelSelector{MatchLabels: map[string]string{"role": "spine", "site": "east"}},
			labels:   map[string]string{"role": "spine", "site": "east", "vendor": "nokia"},
			want:     true,
		},
		{
			name:     "label mismatch",
			selector: LabelSelector{MatchLabels: map[string]string{"role": "spine"}},
			labels:   map[string]string{"role": "leaf"},
			want:     false,
		},
		{
			name:     "missing label",
			selector: LabelSelector{MatchLabels: map[string]string{"role": "spine"}},
			labels:   map[string]string{"site": "east"},
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.selector.Matches(tc.labels); got != tc.want {
				t.Errorf("Matches() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectSources(t *testing.T) {
	labelIndex := map[string]map[string]string{
		"lab/spine-1/ssh":  {"group": "lab", "device": "spine-1", "role": "spine", "template": "ssh", "vendor": "nokia"},
		"lab/spine-1/gnmi": {"group": "lab", "device": "spine-1", "role": "spine", "template": "gnmi", "vendor": "nokia"},
		"lab/leaf-1/ssh":   {"group": "lab", "device": "leaf-1", "role": "leaf", "template": "ssh", "vendor": "nokia"},
		"lab/leaf-1/gnmi":  {"group": "lab", "device": "leaf-1", "role": "leaf", "template": "gnmi", "vendor": "nokia"},
	}

	tests := []struct {
		name     string
		selector LabelSelector
		want     []string
	}{
		{
			name:     "select by role",
			selector: LabelSelector{MatchLabels: map[string]string{"role": "spine"}},
			want:     []string{"lab/spine-1/gnmi", "lab/spine-1/ssh"},
		},
		{
			name:     "select by template",
			selector: LabelSelector{MatchLabels: map[string]string{"template": "ssh"}},
			want:     []string{"lab/leaf-1/ssh", "lab/spine-1/ssh"},
		},
		{
			name:     "select by role and template",
			selector: LabelSelector{MatchLabels: map[string]string{"role": "leaf", "template": "gnmi"}},
			want:     []string{"lab/leaf-1/gnmi"},
		},
		{
			name:     "no matches",
			selector: LabelSelector{MatchLabels: map[string]string{"vendor": "juniper"}},
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SelectSources(tc.selector, labelIndex)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("SelectSources() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSourceName(t *testing.T) {
	got := SourceName("nokia-lab", "spine-1", "ssh")
	want := "nokia-lab/spine-1/ssh"
	if got != want {
		t.Errorf("SourceName() = %q, want %q", got, want)
	}
}
