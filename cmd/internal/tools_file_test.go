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

package internal

import (
	"fmt"
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/internal/auth/oidc"
	"github.com/adrien19/noc-foundry/internal/embeddingmodels/gemini"
	"github.com/adrien19/noc-foundry/internal/prebuiltconfigs"
	"github.com/adrien19/noc-foundry/internal/prompts"
	"github.com/adrien19/noc-foundry/internal/prompts/custom"
	"github.com/adrien19/noc-foundry/internal/server"
	httpsrc "github.com/adrien19/noc-foundry/internal/sources/http"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/tools/http"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/google/go-cmp/cmp"
)

func TestParseEnv(t *testing.T) {
	tcs := []struct {
		desc      string
		env       map[string]string
		in        string
		want      string
		err       bool
		errString string
	}{
		{
			desc:      "without default without env",
			in:        "${FOO}",
			want:      "",
			err:       true,
			errString: `environment variable not found: "FOO"`,
		},
		{
			desc: "without default with env",
			env: map[string]string{
				"FOO": "bar",
			},
			in:   "${FOO}",
			want: "bar",
		},
		{
			desc: "with empty default",
			in:   "${FOO:}",
			want: "",
		},
		{
			desc: "with default",
			in:   "${FOO:bar}",
			want: "bar",
		},
		{
			desc: "with default with env",
			env: map[string]string{
				"FOO": "hello",
			},
			in:   "${FOO:bar}",
			want: "hello",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			if tc.env != nil {
				for k, v := range tc.env {
					t.Setenv(k, v)
				}
			}
			parser := &ToolsFileParser{}
			got, err := parser.parseEnv(tc.in)
			if tc.err {
				if err == nil {
					t.Fatalf("expected error not found")
				}
				if tc.errString != err.Error() {
					t.Fatalf("incorrect error string: got %s, want %s", err, tc.errString)
				}
			}
			if tc.want != got {
				t.Fatalf("unexpected want: got %s, want %s", got, tc.want)
			}
		})
	}
}

func TestConvertToolsFile(t *testing.T) {
	tcs := []struct {
		desc   string
		in     string
		want   string
		isErr  bool
		errStr string
	}{
		{
			desc: "basic convert",
			in: `
            sources:
                my-pg-instance:
                    kind: cloud-sql-postgres
                    project: my-project
                    region: my-region
                    instance: my-instance
                    database: my_db
                    user: my_user
                    password: my_pass
            authServices:
                my-google-auth:
                    kind: oidc
                    issuerUrl: https://issuer.example.com
                    clientId: testing-id
            tools:
                example_tool:
                    kind: postgres-sql
                    source: my-pg-instance
                    description: some description
                    statement: SELECT * FROM SQL_STATEMENT;
                    parameters:
                        - name: country
                          type: string
                          description: some description
            toolsets:
                example_toolset:
                    - example_tool
            prompts:
                code_review:
                    description: ask llm to analyze code quality
                    messages:
                      - content: "please review the following code for quality: {{.code}}"
                    arguments:
                        - name: code
                          description: the code to review
            embeddingModels:
                gemini-model:
                    kind: gemini
                    model: gemini-embedding-001
                    apiKey: some-key
                    dimension: 768`,
			want: `kind: sources
name: my-pg-instance
type: cloud-sql-postgres
project: my-project
region: my-region
instance: my-instance
database: my_db
user: my_user
password: my_pass
---
kind: authServices
name: my-google-auth
type: oidc
issuerUrl: https://issuer.example.com
clientId: testing-id
---
kind: tools
name: example_tool
type: postgres-sql
source: my-pg-instance
description: some description
statement: SELECT * FROM SQL_STATEMENT;
parameters:
- name: country
  type: string
  description: some description
---
kind: toolsets
name: example_toolset
tools:
- example_tool
---
kind: prompts
name: code_review
description: ask llm to analyze code quality
messages:
- content: "please review the following code for quality: {{.code}}"
arguments:
- name: code
  description: the code to review
---
kind: embeddingModels
name: gemini-model
type: gemini
model: gemini-embedding-001
apiKey: some-key
dimension: 768
`,
		},
		{
			desc: "preserve resource order",
			in: `
            tools:
                example_tool:
                    kind: postgres-sql
                    source: my-pg-instance
                    description: some description
                    statement: SELECT * FROM SQL_STATEMENT;
                    parameters:
                        - name: country
                          type: string
                          description: some description
            sources:
                my-pg-instance:
                    kind: cloud-sql-postgres
                    project: my-project
                    region: my-region
                    instance: my-instance
                    database: my_db
                    user: my_user
                    password: my_pass
            authServices:
                my-google-auth:
                    kind: oidc
                    issuerUrl: https://issuer.example.com
                    clientId: testing-id
            toolsets:
                example_toolset:
                    - example_tool
            authSources:
                my-google-auth2:
                    kind: oidc
                    issuerUrl: https://issuer.example.com
                    clientId: testing-id`,
			want: `kind: tools
name: example_tool
type: postgres-sql
source: my-pg-instance
description: some description
statement: SELECT * FROM SQL_STATEMENT;
parameters:
- name: country
  type: string
  description: some description
---
kind: sources
name: my-pg-instance
type: cloud-sql-postgres
project: my-project
region: my-region
instance: my-instance
database: my_db
user: my_user
password: my_pass
---
kind: authServices
name: my-google-auth
type: oidc
issuerUrl: https://issuer.example.com
clientId: testing-id
---
kind: toolsets
name: example_toolset
tools:
- example_tool
---
kind: authServices
name: my-google-auth2
type: oidc
issuerUrl: https://issuer.example.com
clientId: testing-id
`,
		},
		{
			desc: "convert combination of v1 and v2",
			in: `
            sources:
                my-pg-instance:
                    kind: cloud-sql-postgres
                    project: my-project
                    region: my-region
                    instance: my-instance
                    database: my_db
                    user: my_user
                    password: my_pass
            authServices:
                my-google-auth:
                    kind: oidc
                    issuerUrl: https://issuer.example.com
                    clientId: testing-id
            tools:
                example_tool:
                    kind: postgres-sql
                    source: my-pg-instance
                    description: some description
                    statement: SELECT * FROM SQL_STATEMENT;
                    parameters:
                        - name: country
                          type: string
                          description: some description
            toolsets:
                example_toolset:
                    - example_tool
            prompts:
                code_review:
                    description: ask llm to analyze code quality
                    messages:
                      - content: "please review the following code for quality: {{.code}}"
                    arguments:
                        - name: code
                          description: the code to review
            embeddingModels:
                gemini-model:
                    kind: gemini
                    model: gemini-embedding-001
                    apiKey: some-key
                    dimension: 768
---
            kind: sources
            name: my-pg-instance2
            type: cloud-sql-postgres
            project: my-project
            region: my-region
            instance: my-instance
---
            kind: authServices
            name: my-google-auth2
            type: oidc
            issuerUrl: https://issuer.example.com
            clientId: testing-id
---
            kind: tools
            name: example_tool2
            type: postgres-sql
            source: my-pg-instance
            description: some description
            statement: SELECT * FROM SQL_STATEMENT;
            parameters:
            - name: country
              type: string
              description: some description
---
            kind: toolsets
            name: example_toolset2
            tools:
            - example_tool
---
            tools:
            - example_tool
            kind: toolsets
            name: example_toolset3
---
            kind: prompts
            name: code_review2
            description: ask llm to analyze code quality
            messages:
            - content: "please review the following code for quality: {{.code}}"
            arguments:
            - name: code
              description: the code to review
---
            kind: embeddingModels
            name: gemini-model2
            type: gemini`,
			want: `kind: sources
name: my-pg-instance
type: cloud-sql-postgres
project: my-project
region: my-region
instance: my-instance
database: my_db
user: my_user
password: my_pass
---
kind: authServices
name: my-google-auth
type: oidc
issuerUrl: https://issuer.example.com
clientId: testing-id
---
kind: tools
name: example_tool
type: postgres-sql
source: my-pg-instance
description: some description
statement: SELECT * FROM SQL_STATEMENT;
parameters:
- name: country
  type: string
  description: some description
---
kind: toolsets
name: example_toolset
tools:
- example_tool
---
kind: prompts
name: code_review
description: ask llm to analyze code quality
messages:
- content: "please review the following code for quality: {{.code}}"
arguments:
- name: code
  description: the code to review
---
kind: embeddingModels
name: gemini-model
type: gemini
model: gemini-embedding-001
apiKey: some-key
dimension: 768
---
kind: sources
name: my-pg-instance2
type: cloud-sql-postgres
project: my-project
region: my-region
instance: my-instance
---
kind: authServices
name: my-google-auth2
type: oidc
issuerUrl: https://issuer.example.com
clientId: testing-id
---
kind: tools
name: example_tool2
type: postgres-sql
source: my-pg-instance
description: some description
statement: SELECT * FROM SQL_STATEMENT;
parameters:
- name: country
  type: string
  description: some description
---
kind: toolsets
name: example_toolset2
tools:
- example_tool
---
tools:
- example_tool
kind: toolsets
name: example_toolset3
---
kind: prompts
name: code_review2
description: ask llm to analyze code quality
messages:
- content: "please review the following code for quality: {{.code}}"
arguments:
- name: code
  description: the code to review
---
kind: embeddingModels
name: gemini-model2
type: gemini
`,
		},
		{
			desc: "no convertion needed",
			in: `kind: sources
name: my-pg-instance
type: cloud-sql-postgres
project: my-project
region: my-region
instance: my-instance
database: my_db
user: my_user
password: my_pass
---
kind: tools
name: example_tool
type: postgres-sql
source: my-pg-instance
description: some description
statement: SELECT * FROM SQL_STATEMENT;
parameters:
- name: country
  type: string
  description: some description
---
kind: toolsets
name: example_toolset
tools:
- example_tool`,
			want: `kind: sources
name: my-pg-instance
type: cloud-sql-postgres
project: my-project
region: my-region
instance: my-instance
database: my_db
user: my_user
password: my_pass
---
kind: tools
name: example_tool
type: postgres-sql
source: my-pg-instance
description: some description
statement: SELECT * FROM SQL_STATEMENT;
parameters:
- name: country
  type: string
  description: some description
---
kind: toolsets
name: example_toolset
tools:
- example_tool
`,
		},
		{
			desc: "invalid source",
			in:   `sources: invalid`,
			want: "",
		},
		{
			desc: "invalid toolset",
			in:   `toolsets: invalid`,
			want: "",
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			output, err := convertToolsFile([]byte(tc.in))
			if err != nil {
				t.Fatalf("unexpected error: %s", err)
			}

			if diff := cmp.Diff(string(output), tc.want); diff != "" {
				t.Fatalf("incorrect toolsets parse: diff %v", diff)
			}
		})
	}
}

func TestParseToolFile(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	tcs := []struct {
		description   string
		in            string
		wantToolsFile ToolsFile
	}{
		{
			description: "basic example tools file v1",
			in: `
			sources:
				my-http-instance:
					kind: http
					baseUrl: http://example.com
					timeout: 10s
			tools:
				example_tool:
					kind: http
					source: my-http-instance
					description: some description
					method: GET
					path: /search
					queryParams:
						- name: country
							type: string
							description: some description
			toolsets:
				example_toolset:
					- example_tool
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:    "my-http-instance",
						Type:    httpsrc.SourceType,
						BaseURL: "http://example.com",
						Timeout: "10s",
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-http-instance",
						Description:  "some description",
						AuthRequired: []string{},
						Method:       "GET",
						Path:         "/search",
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameter("country", "some description"),
						},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"example_toolset": tools.ToolsetConfig{
						Name:      "example_toolset",
						ToolNames: []string{"example_tool"},
					},
				},
				AuthServices: nil,
				Prompts:      nil,
			},
		},
		{
			description: "basic example tools file v2",
			in: `
			kind: sources
			name: my-http-instance
			type: http
			baseUrl: http://example.com
			timeout: 10s
---
			kind: authServices
			name: my-google-auth
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: testing-id
---
			kind: embeddingModels
			name: gemini-model
			type: gemini
			model: gemini-embedding-001
			apiKey: some-key
			dimension: 768
---
			kind: tools
			name: example_tool
			type: http
			source: my-http-instance
			description: some description
			method: GET
			path: /search
			queryParams:
			- name: country
			  type: string
			  description: some description
---
			kind: toolsets
			name: example_toolset
			tools:
			- example_tool
---
			kind: prompts
			name: code_review
			description: ask llm to analyze code quality
			messages:
			- content: "please review the following code for quality: {{.code}}"
			arguments:
			- name: code
			  description: the code to review
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:    "my-http-instance",
						Type:    httpsrc.SourceType,
						BaseURL: "http://example.com",
						Timeout: "10s",
					},
				},
				AuthServices: server.AuthServiceConfigs{
					"my-google-auth": oidc.Config{
						Name:      "my-google-auth",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "testing-id",
					},
				},
				EmbeddingModels: server.EmbeddingModelConfigs{
					"gemini-model": gemini.Config{
						Name:      "gemini-model",
						Type:      gemini.EmbeddingModelType,
						Model:     "gemini-embedding-001",
						ApiKey:    "some-key",
						Dimension: 768,
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-http-instance",
						Description:  "some description",
						AuthRequired: []string{},
						Method:       "GET",
						Path:         "/search",
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameter("country", "some description"),
						},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"example_toolset": tools.ToolsetConfig{
						Name:      "example_toolset",
						ToolNames: []string{"example_tool"},
					},
				},
				Prompts: server.PromptConfigs{
					"code_review": &custom.Config{
						Name:        "code_review",
						Description: "ask llm to analyze code quality",
						Arguments: prompts.Arguments{
							{Parameter: parameters.NewStringParameter("code", "the code to review")},
						},
						Messages: []prompts.Message{
							{Role: "user", Content: "please review the following code for quality: {{.code}}"},
						},
					},
				},
			},
		},
		{
			description: "only prompts",
			in: `
            kind: prompts
            name: my-prompt
            description: A prompt template for data analysis.
            arguments:
                - name: country
                  description: The country to analyze.
            messages:
                - content: Analyze the data for {{.country}}.
            `,
			wantToolsFile: ToolsFile{
				Sources:      nil,
				AuthServices: nil,
				Tools:        nil,
				Toolsets:     nil,
				Prompts: server.PromptConfigs{
					"my-prompt": &custom.Config{
						Name:        "my-prompt",
						Description: "A prompt template for data analysis.",
						Arguments: prompts.Arguments{
							{Parameter: parameters.NewStringParameter("country", "The country to analyze.")},
						},
						Messages: []prompts.Message{
							{Role: "user", Content: "Analyze the data for {{.country}}."},
						},
					},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			parser := ToolsFileParser{}
			toolsFile, err := parser.ParseToolsFile(ctx, testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("failed to parse input: %v", err)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Sources, toolsFile.Sources); diff != "" {
				t.Fatalf("incorrect sources parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.AuthServices, toolsFile.AuthServices); diff != "" {
				t.Fatalf("incorrect authServices parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Tools, toolsFile.Tools); diff != "" {
				t.Fatalf("incorrect tools parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Toolsets, toolsFile.Toolsets); diff != "" {
				t.Fatalf("incorrect toolsets parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Prompts, toolsFile.Prompts); diff != "" {
				t.Fatalf("incorrect prompts parse: diff %v", diff)
			}
		})
	}
}

func TestParseToolFileWithAuth(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	tcs := []struct {
		description   string
		in            string
		wantToolsFile ToolsFile
	}{
		{
			description: "basic example",
			in: `
			kind: sources
			name: my-http-instance
			type: http
			baseUrl: http://example.com
---
			kind: authServices
			name: my-google-service
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: my-client-id
---
			kind: authServices
			name: other-google-service
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: other-client-id
---
			kind: tools
			name: example_tool
			type: http
			source: my-http-instance
			description: some description
			method: GET
			path: /search
			authRequired:
				- my-google-service
			queryParams:
				- name: country
					type: string
					description: some description
				- name: id
					type: integer
					description: user id
					authServices:
					- name: my-google-service
						field: user_id
				- name: email
					type: string
					description: user email
					authServices:
					- name: my-google-service
						field: email
					- name: other-google-service
						field: other_email
---
			kind: toolsets
			name: example_toolset
			tools:
				- example_tool
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:    "my-http-instance",
						Type:    httpsrc.SourceType,
						BaseURL: "http://example.com",
						Timeout: "30s",
					},
				},
				AuthServices: server.AuthServiceConfigs{
					"my-google-service": oidc.Config{
						Name:      "my-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "my-client-id",
					},
					"other-google-service": oidc.Config{
						Name:      "other-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "other-client-id",
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-http-instance",
						Description:  "some description",
						Method:       "GET",
						Path:         "/search",
						AuthRequired: []string{"my-google-service"},
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameter("country", "some description"),
							parameters.NewIntParameterWithAuth("id", "user id", []parameters.ParamAuthService{{Name: "my-google-service", Field: "user_id"}}),
							parameters.NewStringParameterWithAuth("email", "user email", []parameters.ParamAuthService{{Name: "my-google-service", Field: "email"}, {Name: "other-google-service", Field: "other_email"}}),
						},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"example_toolset": tools.ToolsetConfig{
						Name:      "example_toolset",
						ToolNames: []string{"example_tool"},
					},
				},
				Prompts: nil,
			},
		},
		{
			description: "basic example with authSources",
			in: `
			sources:
				my-http-instance:
					kind: http
					baseUrl: http://example.com
			authSources:
				my-google-service:
					kind: oidc
					issuerUrl: https://issuer.example.com
					clientId: my-client-id
				other-google-service:
					kind: oidc
					issuerUrl: https://issuer.example.com
					clientId: other-client-id

			tools:
				example_tool:
					kind: http
					source: my-http-instance
					description: some description
					method: GET
					path: /search
					queryParams:
						- name: country
						  type: string
						  description: some description
						- name: id
						  type: integer
						  description: user id
						  authSources:
							- name: my-google-service
								field: user_id
						- name: email
							type: string
							description: user email
							authSources:
							- name: my-google-service
							  field: email
							- name: other-google-service
							  field: other_email

			toolsets:
				example_toolset:
					- example_tool
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:    "my-http-instance",
						Type:    httpsrc.SourceType,
						BaseURL: "http://example.com",
						Timeout: "30s",
					},
				},
				AuthServices: server.AuthServiceConfigs{
					"my-google-service": oidc.Config{
						Name:      "my-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "my-client-id",
					},
					"other-google-service": oidc.Config{
						Name:      "other-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "other-client-id",
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-http-instance",
						Description:  "some description",
						Method:       "GET",
						Path:         "/search",
						AuthRequired: []string{},
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameter("country", "some description"),
							parameters.NewIntParameterWithAuth("id", "user id", []parameters.ParamAuthService{{Name: "my-google-service", Field: "user_id"}}),
							parameters.NewStringParameterWithAuth("email", "user email", []parameters.ParamAuthService{{Name: "my-google-service", Field: "email"}, {Name: "other-google-service", Field: "other_email"}}),
						},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"example_toolset": tools.ToolsetConfig{
						Name:      "example_toolset",
						ToolNames: []string{"example_tool"},
					},
				},
				Prompts: nil,
			},
		},
		{
			description: "basic example with authRequired",
			in: `
			kind: sources
			name: my-http-instance
			type: http
			baseUrl: http://example.com
---
			kind: authServices
			name: my-google-service
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: my-client-id
---
			kind: authServices
			name: other-google-service
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: other-client-id
---
			kind: tools
			name: example_tool
			type: http
			source: my-http-instance
			description: some description
			method: GET
			path: /search
			authRequired:
				- my-google-service
			queryParams:
				- name: country
					type: string
					description: some description
				- name: id
					type: integer
					description: user id
					authServices:
					- name: my-google-service
						field: user_id
				- name: email
					type: string
					description: user email
					authServices:
					- name: my-google-service
						field: email
					- name: other-google-service
						field: other_email
---
			kind: toolsets
			name: example_toolset
			tools:
				- example_tool
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:    "my-http-instance",
						Type:    httpsrc.SourceType,
						BaseURL: "http://example.com",
						Timeout: "30s",
					},
				},
				AuthServices: server.AuthServiceConfigs{
					"my-google-service": oidc.Config{
						Name:      "my-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "my-client-id",
					},
					"other-google-service": oidc.Config{
						Name:      "other-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "other-client-id",
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-http-instance",
						Description:  "some description",
						Method:       "GET",
						Path:         "/search",
						AuthRequired: []string{"my-google-service"},
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameter("country", "some description"),
							parameters.NewIntParameterWithAuth("id", "user id", []parameters.ParamAuthService{{Name: "my-google-service", Field: "user_id"}}),
							parameters.NewStringParameterWithAuth("email", "user email", []parameters.ParamAuthService{{Name: "my-google-service", Field: "email"}, {Name: "other-google-service", Field: "other_email"}}),
						},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"example_toolset": tools.ToolsetConfig{
						Name:      "example_toolset",
						ToolNames: []string{"example_tool"},
					},
				},
				Prompts: nil,
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			parser := ToolsFileParser{}
			toolsFile, err := parser.ParseToolsFile(ctx, testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("failed to parse input: %v", err)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Sources, toolsFile.Sources); diff != "" {
				t.Fatalf("incorrect sources parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.AuthServices, toolsFile.AuthServices); diff != "" {
				t.Fatalf("incorrect authServices parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Tools, toolsFile.Tools); diff != "" {
				t.Fatalf("incorrect tools parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Toolsets, toolsFile.Toolsets); diff != "" {
				t.Fatalf("incorrect toolsets parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Prompts, toolsFile.Prompts); diff != "" {
				t.Fatalf("incorrect prompts parse: diff %v", diff)
			}
		})
	}

}

func TestEnvVarReplacement(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	t.Setenv("TestHeader", "ACTUAL_HEADER")
	t.Setenv("API_KEY", "ACTUAL_API_KEY")
	t.Setenv("clientId", "ACTUAL_CLIENT_ID")
	t.Setenv("clientId2", "ACTUAL_CLIENT_ID_2")
	t.Setenv("toolset_name", "ACTUAL_TOOLSET_NAME")
	t.Setenv("cat_string", "cat")
	t.Setenv("food_string", "food")
	t.Setenv("TestHeader", "ACTUAL_HEADER")
	t.Setenv("prompt_name", "ACTUAL_PROMPT_NAME")
	t.Setenv("prompt_content", "ACTUAL_CONTENT")

	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	tcs := []struct {
		description   string
		in            string
		wantToolsFile ToolsFile
	}{
		{
			description: "file with env var example",
			in: `
			sources:
				my-http-instance:
					kind: http
					baseUrl: http://test_server/
					timeout: 10s
					headers:
						Authorization: ${TestHeader}
					queryParams:
						api-key: ${API_KEY}
			authServices:
				my-google-service:
					kind: oidc
					issuerUrl: https://issuer.example.com
					clientId: ${clientId}
				other-google-service:
					kind: oidc
					issuerUrl: https://issuer.example.com
					clientId: ${clientId2}

			tools:
				example_tool:
					kind: http
					source: my-instance
					method: GET
					path: "search?name=alice&pet=${cat_string}"
					description: some description
					authRequired:
						- my-google-auth-service
						- other-auth-service
					queryParams:
						- name: country
						  type: string
						  description: some description
						  authServices:
							- name: my-google-auth-service
							  field: user_id
							- name: other-auth-service
							  field: user_id
					requestBody: |
							{
								"age": {{.age}},
								"city": "{{.city}}",
								"food": "${food_string}",
								"other": "$OTHER"
							}
					bodyParams:
						- name: age
						  type: integer
						  description: age num
						- name: city
						  type: string
						  description: city string
					headers:
						Authorization: API_KEY
						Content-Type: application/json
					headerParams:
						- name: Language
						  type: string
						  description: language string

			toolsets:
				${toolset_name}:
					- example_tool


			prompts:
				${prompt_name}:
					description: A test prompt for {{.name}}.
					messages:
						- role: user
						  content: ${prompt_content}
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:           "my-http-instance",
						Type:           httpsrc.SourceType,
						BaseURL:        "http://test_server/",
						Timeout:        "10s",
						DefaultHeaders: map[string]string{"Authorization": "ACTUAL_HEADER"},
						QueryParams:    map[string]string{"api-key": "ACTUAL_API_KEY"},
					},
				},
				AuthServices: server.AuthServiceConfigs{
					"my-google-service": oidc.Config{
						Name:      "my-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "ACTUAL_CLIENT_ID",
					},
					"other-google-service": oidc.Config{
						Name:      "other-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "ACTUAL_CLIENT_ID_2",
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-instance",
						Method:       "GET",
						Path:         "search?name=alice&pet=cat",
						Description:  "some description",
						AuthRequired: []string{"my-google-auth-service", "other-auth-service"},
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameterWithAuth("country", "some description",
								[]parameters.ParamAuthService{{Name: "my-google-auth-service", Field: "user_id"},
									{Name: "other-auth-service", Field: "user_id"}}),
						},
						RequestBody: `{
  "age": {{.age}},
  "city": "{{.city}}",
  "food": "food",
  "other": "$OTHER"
}
`,
						BodyParams:   []parameters.Parameter{parameters.NewIntParameter("age", "age num"), parameters.NewStringParameter("city", "city string")},
						Headers:      map[string]string{"Authorization": "API_KEY", "Content-Type": "application/json"},
						HeaderParams: []parameters.Parameter{parameters.NewStringParameter("Language", "language string")},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"ACTUAL_TOOLSET_NAME": tools.ToolsetConfig{
						Name:      "ACTUAL_TOOLSET_NAME",
						ToolNames: []string{"example_tool"},
					},
				},
				Prompts: server.PromptConfigs{
					"ACTUAL_PROMPT_NAME": &custom.Config{
						Name:        "ACTUAL_PROMPT_NAME",
						Description: "A test prompt for {{.name}}.",
						Messages: []prompts.Message{
							{
								Role:    "user",
								Content: "ACTUAL_CONTENT",
							},
						},
						Arguments: nil,
					},
				},
			},
		},
		{
			description: "file with env var example toolsfile v2",
			in: `
			kind: sources
			name: my-http-instance
			type: http
			baseUrl: http://test_server/
			timeout: 10s
			headers:
				Authorization: ${TestHeader}
			queryParams:
				api-key: ${API_KEY}
---
			kind: authServices
			name: my-google-service
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: ${clientId}
---
			kind: authServices
			name: other-google-service
			type: oidc
			issuerUrl: https://issuer.example.com
			clientId: ${clientId2}
---
			kind: tools
			name: example_tool
			type: http
			source: my-instance
			method: GET
			path: "search?name=alice&pet=${cat_string}"
			description: some description
			authRequired:
				- my-google-auth-service
				- other-auth-service
			queryParams:
				- name: country
					type: string
					description: some description
					authServices:
					- name: my-google-auth-service
						field: user_id
					- name: other-auth-service
						field: user_id
			requestBody: |
					{
						"age": {{.age}},
						"city": "{{.city}}",
						"food": "${food_string}",
						"other": "$OTHER"
					}
			bodyParams:
				- name: age
					type: integer
					description: age num
				- name: city
					type: string
					description: city string
			headers:
				Authorization: API_KEY
				Content-Type: application/json
			headerParams:
				- name: Language
					type: string
					description: language string
---
			kind: toolsets
			name: ${toolset_name}
			tools:
				- example_tool
---
			kind: prompts
			name: ${prompt_name}
			description: A test prompt for {{.name}}.
			messages:
				- role: user
					content: ${prompt_content}
			`,
			wantToolsFile: ToolsFile{
				Sources: server.SourceConfigs{
					"my-http-instance": httpsrc.Config{
						Name:           "my-http-instance",
						Type:           httpsrc.SourceType,
						BaseURL:        "http://test_server/",
						Timeout:        "10s",
						DefaultHeaders: map[string]string{"Authorization": "ACTUAL_HEADER"},
						QueryParams:    map[string]string{"api-key": "ACTUAL_API_KEY"},
					},
				},
				AuthServices: server.AuthServiceConfigs{
					"my-google-service": oidc.Config{
						Name:      "my-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "ACTUAL_CLIENT_ID",
					},
					"other-google-service": oidc.Config{
						Name:      "other-google-service",
						Type:      oidc.AuthServiceType,
						IssuerURL: "https://issuer.example.com",
						ClientID:  "ACTUAL_CLIENT_ID_2",
					},
				},
				Tools: server.ToolConfigs{
					"example_tool": http.Config{
						Name:         "example_tool",
						Type:         "http",
						Source:       "my-instance",
						Method:       "GET",
						Path:         "search?name=alice&pet=cat",
						Description:  "some description",
						AuthRequired: []string{"my-google-auth-service", "other-auth-service"},
						QueryParams: []parameters.Parameter{
							parameters.NewStringParameterWithAuth("country", "some description",
								[]parameters.ParamAuthService{{Name: "my-google-auth-service", Field: "user_id"},
									{Name: "other-auth-service", Field: "user_id"}}),
						},
						RequestBody: `{
  "age": {{.age}},
  "city": "{{.city}}",
  "food": "food",
  "other": "$OTHER"
}
`,
						BodyParams:   []parameters.Parameter{parameters.NewIntParameter("age", "age num"), parameters.NewStringParameter("city", "city string")},
						Headers:      map[string]string{"Authorization": "API_KEY", "Content-Type": "application/json"},
						HeaderParams: []parameters.Parameter{parameters.NewStringParameter("Language", "language string")},
					},
				},
				Toolsets: server.ToolsetConfigs{
					"ACTUAL_TOOLSET_NAME": tools.ToolsetConfig{
						Name:      "ACTUAL_TOOLSET_NAME",
						ToolNames: []string{"example_tool"},
					},
				},
				Prompts: server.PromptConfigs{
					"ACTUAL_PROMPT_NAME": &custom.Config{
						Name:        "ACTUAL_PROMPT_NAME",
						Description: "A test prompt for {{.name}}.",
						Messages: []prompts.Message{
							{
								Role:    "user",
								Content: "ACTUAL_CONTENT",
							},
						},
						Arguments: nil,
					},
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.description, func(t *testing.T) {
			parser := ToolsFileParser{}
			toolsFile, err := parser.ParseToolsFile(ctx, testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("failed to parse input: %v", err)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Sources, toolsFile.Sources); diff != "" {
				t.Fatalf("incorrect sources parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.AuthServices, toolsFile.AuthServices); diff != "" {
				t.Fatalf("incorrect authServices parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Tools, toolsFile.Tools); diff != "" {
				t.Fatalf("incorrect tools parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Toolsets, toolsFile.Toolsets); diff != "" {
				t.Fatalf("incorrect toolsets parse: diff %v", diff)
			}
			if diff := cmp.Diff(tc.wantToolsFile.Prompts, toolsFile.Prompts); diff != "" {
				t.Fatalf("incorrect prompts parse: diff %v", diff)
			}
		})
	}

}

func TestPrebuiltTools(t *testing.T) {
	want := []string{"validation-runs"}
	if diff := cmp.Diff(want, prebuiltconfigs.GetPrebuiltSources()); diff != "" {
		t.Fatalf("unexpected bundled prebuilt sources diff (-want +got):\n%s", diff)
	}

	_, err := prebuiltconfigs.Get("cloud-sql-postgres")
	if err == nil {
		t.Fatal("expected error when fetching removed prebuilt config")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := prebuiltconfigs.Get("validation-runs")
	if err != nil {
		t.Fatalf("unexpected error fetching validation-runs prebuilt config: %v", err)
	}
	if !strings.Contains(string(got), "start_validation_run") {
		t.Fatalf("expected validation-runs prebuilt to include start_validation_run")
	}
}

func TestMergeToolsFiles(t *testing.T) {
	file1 := ToolsFile{
		Sources:         server.SourceConfigs{"source1": httpsrc.Config{Name: "source1"}},
		Tools:           server.ToolConfigs{"tool1": http.Config{Name: "tool1"}},
		Toolsets:        server.ToolsetConfigs{"set1": tools.ToolsetConfig{Name: "set1"}},
		EmbeddingModels: server.EmbeddingModelConfigs{"model1": gemini.Config{Name: "gemini-text"}},
	}
	file2 := ToolsFile{
		AuthServices: server.AuthServiceConfigs{"auth1": oidc.Config{Name: "auth1", Type: oidc.AuthServiceType, IssuerURL: "https://issuer.example.com", ClientID: "client-id"}},
		Tools:        server.ToolConfigs{"tool2": http.Config{Name: "tool2"}},
		Toolsets:     server.ToolsetConfigs{"set2": tools.ToolsetConfig{Name: "set2"}},
	}
	fileWithConflicts := ToolsFile{
		Sources: server.SourceConfigs{"source1": httpsrc.Config{Name: "source1"}},
		Tools:   server.ToolConfigs{"tool2": http.Config{Name: "tool2"}},
	}

	testCases := []struct {
		name    string
		files   []ToolsFile
		want    ToolsFile
		wantErr bool
	}{
		{
			name:  "merge two distinct files",
			files: []ToolsFile{file1, file2},
			want: ToolsFile{
				Sources:         server.SourceConfigs{"source1": httpsrc.Config{Name: "source1"}},
				AuthServices:    server.AuthServiceConfigs{"auth1": oidc.Config{Name: "auth1", Type: oidc.AuthServiceType, IssuerURL: "https://issuer.example.com", ClientID: "client-id"}},
				Tools:           server.ToolConfigs{"tool1": http.Config{Name: "tool1"}, "tool2": http.Config{Name: "tool2"}},
				Toolsets:        server.ToolsetConfigs{"set1": tools.ToolsetConfig{Name: "set1"}, "set2": tools.ToolsetConfig{Name: "set2"}},
				Prompts:         server.PromptConfigs{},
				EmbeddingModels: server.EmbeddingModelConfigs{"model1": gemini.Config{Name: "gemini-text"}},
			},
			wantErr: false,
		},
		{
			name:    "merge with conflicts",
			files:   []ToolsFile{file1, file2, fileWithConflicts},
			wantErr: true,
		},
		{
			name:  "merge single file",
			files: []ToolsFile{file1},
			want: ToolsFile{
				Sources:         file1.Sources,
				AuthServices:    make(server.AuthServiceConfigs),
				EmbeddingModels: server.EmbeddingModelConfigs{"model1": gemini.Config{Name: "gemini-text"}},
				Tools:           file1.Tools,
				Toolsets:        file1.Toolsets,
				Prompts:         server.PromptConfigs{},
			},
		},
		{
			name:  "merge empty list",
			files: []ToolsFile{},
			want: ToolsFile{
				Sources:         make(server.SourceConfigs),
				AuthServices:    make(server.AuthServiceConfigs),
				EmbeddingModels: make(server.EmbeddingModelConfigs),
				Tools:           make(server.ToolConfigs),
				Toolsets:        make(server.ToolsetConfigs),
				Prompts:         server.PromptConfigs{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := mergeToolsFiles(tc.files...)
			if (err != nil) != tc.wantErr {
				t.Fatalf("mergeToolsFiles() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr {
				if diff := cmp.Diff(tc.want, got); diff != "" {
					t.Errorf("mergeToolsFiles() mismatch (-want +got):\n%s", diff)
				}
			} else {
				if err == nil {
					t.Fatal("expected an error for conflicting files but got none")
				}
				if !strings.Contains(err.Error(), "resource conflicts detected") {
					t.Errorf("expected conflict error, but got: %v", err)
				}
			}
		})
	}
}

func TestParameterReferenceValidation(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	// Base template
	baseYaml := `
sources:
  dummy-source:
    kind: http
    baseUrl: http://example.com
tools:
  test-tool:
		kind: http
    source: dummy-source
    description: test tool
		method: GET
		path: /test
		queryParams:
%s`

	tcs := []struct {
		desc      string
		params    string
		wantErr   bool
		errSubstr string
	}{
		{
			desc: "valid backward reference",
			params: `
      - name: source_param
        type: string
        description: source
      - name: copy_param
        type: string
        description: copy
        valueFromParam: source_param`,
			wantErr: false,
		},
		{
			desc: "valid forward reference (out of order)",
			params: `
      - name: copy_param
        type: string
        description: copy
        valueFromParam: source_param
      - name: source_param
        type: string
        description: source`,
			wantErr: false,
		},
		{
			desc: "invalid missing reference",
			params: `
      - name: copy_param
        type: string
        description: copy
        valueFromParam: non_existent_param`,
			wantErr: false,
		},
		{
			desc: "invalid self reference",
			params: `
      - name: myself
        type: string
        description: self
        valueFromParam: myself`,
			wantErr: false,
		},
		{
			desc: "multiple valid references",
			params: `
      - name: a
        type: string
        description: a
      - name: b
        type: string
        description: b
        valueFromParam: a
      - name: c
        type: string
        description: c
        valueFromParam: a`,
			wantErr: false,
		},
	}

	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			// Indent parameters to match YAML structure
			yamlContent := strings.ReplaceAll(fmt.Sprintf(baseYaml, tc.params), "\t", "  ")
			parser := ToolsFileParser{}
			_, err := parser.ParseToolsFile(ctx, []byte(yamlContent))

			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Errorf("error %q does not contain expected substring %q", err.Error(), tc.errSubstr)
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestParseToolsFile_RichToolsetAndPromptset(t *testing.T) {
	ctx, err := testutils.ContextWithNewLogger()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	parser := ToolsFileParser{}
	toolsFile, err := parser.ParseToolsFile(ctx, []byte(`
---
kind: sources
name: my-http
type: http
baseUrl: https://example.com

---
kind: tools
name: show_interfaces
type: http
source: my-http
description: Inspect interfaces.
method: GET
path: /interfaces

---
kind: prompts
name: summarize_interfaces
description: Summarize interfaces for an operator.
messages:
  - role: user
    content: Summarize {{.result_json}}.
arguments:
  - name: result_json
    description: Result payload.

---
kind: promptsets
name: inspection-guidance
prompts:
  - summarize_interfaces

---
kind: toolsets
name: inspection-workflow
description: Inspect devices and summarize the output.
promptset: inspection-guidance
tools:
  - show_interfaces
`))
	if err != nil {
		t.Fatalf("ParseToolsFile() unexpected error: %v", err)
	}

	toolset, ok := toolsFile.Toolsets["inspection-workflow"]
	if !ok {
		t.Fatalf("expected toolset to be parsed")
	}
	if toolset.Description != "Inspect devices and summarize the output." {
		t.Fatalf("unexpected toolset description: %q", toolset.Description)
	}
	if toolset.Promptset != "inspection-guidance" {
		t.Fatalf("unexpected toolset promptset: %q", toolset.Promptset)
	}
	if diff := cmp.Diff([]string{"show_interfaces"}, toolset.ToolNames); diff != "" {
		t.Fatalf("unexpected tool names (-want +got):\n%s", diff)
	}

	promptset, ok := toolsFile.Promptsets["inspection-guidance"]
	if !ok {
		t.Fatalf("expected promptset to be parsed")
	}
	if diff := cmp.Diff([]string{"summarize_interfaces"}, promptset.PromptNames); diff != "" {
		t.Fatalf("unexpected prompt names (-want +got):\n%s", diff)
	}
}

func TestMergeToolsFiles_PromptsetConflict(t *testing.T) {
	file1 := ToolsFile{
		Promptsets: server.PromptsetConfigs{
			"ops-guidance": {Name: "ops-guidance", PromptNames: []string{"prompt1"}},
		},
	}
	file2 := ToolsFile{
		Promptsets: server.PromptsetConfigs{
			"ops-guidance": {Name: "ops-guidance", PromptNames: []string{"prompt2"}},
		},
	}

	_, err := mergeToolsFiles(file1, file2)
	if err == nil {
		t.Fatal("expected merge conflict")
	}
	if !strings.Contains(err.Error(), "promptset 'ops-guidance'") {
		t.Fatalf("expected promptset conflict, got %v", err)
	}
}
