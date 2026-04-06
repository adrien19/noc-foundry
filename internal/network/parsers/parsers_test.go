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

package parsers_test

import (
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/parsers"
)

func TestLines(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want []string
	}{
		{
			desc: "simple multiline",
			in:   "line1\nline2\nline3",
			want: []string{"line1", "line2", "line3"},
		},
		{
			desc: "strips empty lines",
			in:   "line1\n\n\nline2\n",
			want: []string{"line1", "line2"},
		},
		{
			desc: "strips trailing whitespace",
			in:   "line1  \r\nline2\t\n",
			want: []string{"line1", "line2"},
		},
		{
			desc: "empty string",
			in:   "",
			want: nil,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got := parsers.Lines(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("Lines() returned %d lines, want %d", len(got), len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("Lines()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestExtractKeyValue(t *testing.T) {
	tcs := []struct {
		desc   string
		line   string
		sep    string
		wantK  string
		wantV  string
		wantOK bool
	}{
		{
			desc:  "colon separator",
			line:  "hostname : router-1",
			sep:   ":",
			wantK: "hostname", wantV: "router-1", wantOK: true,
		},
		{
			desc:  "equals separator",
			line:  "version = 24.7.R1",
			sep:   "=",
			wantK: "version", wantV: "24.7.R1", wantOK: true,
		},
		{
			desc:  "no separator",
			line:  "some random line",
			sep:   ":",
			wantK: "", wantV: "", wantOK: false,
		},
		{
			desc:  "multiple separators uses first",
			line:  "key : val : extra",
			sep:   ":",
			wantK: "key", wantV: "val : extra", wantOK: true,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			k, v, ok := parsers.ExtractKeyValue(tc.line, tc.sep)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if k != tc.wantK {
				t.Errorf("key = %q, want %q", k, tc.wantK)
			}
			if v != tc.wantV {
				t.Errorf("val = %q, want %q", v, tc.wantV)
			}
		})
	}
}

func TestIsSeparatorLine(t *testing.T) {
	tcs := []struct {
		desc string
		line string
		char byte
		want bool
	}{
		{desc: "equals line", line: "===============", char: '=', want: true},
		{desc: "dashes line", line: "---------------", char: '-', want: true},
		{desc: "mixed chars", line: "===--===", char: '=', want: false},
		{desc: "empty", line: "", char: '=', want: false},
		{desc: "whitespace only", line: "   ", char: '=', want: false},
		{desc: "with leading whitespace", line: "  ===", char: '=', want: true},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got := parsers.IsSeparatorLine(tc.line, tc.char)
			if got != tc.want {
				t.Errorf("IsSeparatorLine(%q, %q) = %v, want %v", tc.line, tc.char, got, tc.want)
			}
		})
	}
}
