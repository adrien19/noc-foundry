// Copyright 2026 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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

package invoke

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/cmd/internal"
	_ "github.com/adrien19/noc-foundry/internal/sources/http"
	_ "github.com/adrien19/noc-foundry/internal/tools/http"
	"github.com/spf13/cobra"
)

func invokeCommand(args []string) (string, error) {
	parentCmd := &cobra.Command{Use: "nocfoundry"}

	buf := new(bytes.Buffer)
	opts := internal.NewNOCFoundryOptions(internal.WithIOStreams(buf, buf))
	internal.PersistentFlags(parentCmd, opts)

	cmd := NewCommand(opts)
	parentCmd.AddCommand(cmd)
	parentCmd.SetArgs(args)

	err := parentCmd.Execute()
	return buf.String(), err
}

func TestInvokeTool(t *testing.T) {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/hello":
			_, _ = fmt.Fprint(w, `{"greeting":"hello"}`)
		case "/echo":
			_, _ = fmt.Fprintf(w, `{"msg":"%s"}`, r.URL.Query().Get("message"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer testServer.Close()

	// Create a temporary tools file
	tmpDir := t.TempDir()

	toolsFileContent := fmt.Sprintf("sources:\n  my-http:\n    kind: http\n    baseUrl: %s\ntools:\n  hello-http:\n    kind: http\n    source: my-http\n    description: \"hello tool\"\n    method: GET\n    path: /hello\n  echo-tool:\n    kind: http\n    source: my-http\n    description: \"echo tool\"\n    method: GET\n    path: /echo\n    queryParams:\n      - name: message\n        type: string\n        description: message to echo\n", testServer.URL)

	toolsFilePath := filepath.Join(tmpDir, "tools.yaml")
	if err := os.WriteFile(toolsFilePath, []byte(toolsFileContent), 0644); err != nil {
		t.Fatalf("failed to write tools file: %v", err)
	}

	tcs := []struct {
		desc    string
		args    []string
		want    string
		wantErr bool
		errStr  string
	}{
		{
			desc: "success - basic tool call",
			args: []string{"invoke", "hello-http", "--tools-file", toolsFilePath},
			want: `"greeting": "hello"`,
		},
		{
			desc: "success - tool call with parameters",
			args: []string{"invoke", "echo-tool", `{"message": "world"}`, "--tools-file", toolsFilePath},
			want: `"msg": "world"`,
		},
		{
			desc: "success - tool call with validation runtime flags",
			args: []string{
				"invoke", "hello-http",
				"--tools-file", toolsFilePath,
				"--validation-backend", "durabletask",
				"--validation-store", "sqlite",
				"--validation-db", filepath.Join(tmpDir, "validation-runs.sqlite"),
				"--validation-taskhub-db", filepath.Join(tmpDir, "validation-taskhub.sqlite"),
			},
			want: `"greeting": "hello"`,
		},
		{
			desc:    "error - tool not found",
			args:    []string{"invoke", "non-existent", "--tools-file", toolsFilePath},
			wantErr: true,
			errStr:  `tool "non-existent" not found`,
		},
		{
			desc:    "error - invalid JSON params",
			args:    []string{"invoke", "echo-tool", `invalid-json`, "--tools-file", toolsFilePath},
			wantErr: true,
			errStr:  `params must be a valid JSON string`,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := invokeCommand(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("got error %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr && !strings.Contains(err.Error(), tc.errStr) {
				t.Fatalf("got error %v, want error containing %q", err, tc.errStr)
			}
			if !tc.wantErr && !strings.Contains(got, tc.want) {
				t.Fatalf("got %q, want it to contain %q", got, tc.want)
			}
		})
	}
}
