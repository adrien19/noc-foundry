// Copyright 2024 Google LLC
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

package prebuiltconfigs

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

var expectedToolSources = []string{
	"validation-runs",
}

func TestGetPrebuiltSources(t *testing.T) {
	t.Run("Test Get Prebuilt Sources", func(t *testing.T) {
		sources := GetPrebuiltSources()
		if diff := cmp.Diff(expectedToolSources, sources); diff != "" {
			t.Fatalf("incorrect sources parse: diff %v", diff)
		}

	})
}

func TestLoadPrebuiltToolYAMLs(t *testing.T) {
	test_name := "test load prebuilt configs"
	expectedKeys := expectedToolSources
	t.Run(test_name, func(t *testing.T) {
		configsMap, keys, err := loadPrebuiltToolYAMLs()
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}
		if len(expectedKeys) != len(configsMap) {
			t.Fatalf("Failed to load all prebuilt tools.")
		}

		for _, expectedKey := range expectedKeys {
			_, ok := configsMap[expectedKey]
			if !ok {
				t.Fatalf("Prebuilt tools for '%s' was NOT FOUND in the loaded map.", expectedKey)
			}
		}

		if diff := cmp.Diff(expectedKeys, keys); diff != "" {
			t.Fatalf("incorrect sources parse: diff %v", diff)
		}

	})
}

func TestGetPrebuiltTool(t *testing.T) {
	got := string(getOrFatal(t, "validation-runs"))
	for _, want := range []string{
		"name: start_validation_run",
		"name: validation_run_status",
		"name: validation_run_result",
		"name: cancel_validation_run",
		"name: validation-run-tools",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected prebuilt validation-runs catalog to contain %q", want)
		}
	}
}

func TestFailGetPrebuiltTool(t *testing.T) {
	_, err := Get("sql")
	if err == nil {
		t.Fatalf("unexpected an error but got nil.")
	}
}

func getOrFatal(t *testing.T, prebuiltSourceConfig string) []byte {
	bytes, err := Get(prebuiltSourceConfig)
	if err != nil {
		t.Fatalf("Cannot get prebuilt config for %q, error %v", prebuiltSourceConfig, err)
	}
	return bytes
}
