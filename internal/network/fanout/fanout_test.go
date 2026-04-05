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

package fanout

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecute_SingleDevice(t *testing.T) {
	result := Execute(context.Background(), []string{"dc1/spine-1/default"}, 10, func(ctx context.Context, sourceName string) (any, error) {
		return map[string]string{"hostname": "spine-1"}, nil
	})

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Device != "spine-1" {
		t.Errorf("expected device 'spine-1', got %q", result.Results[0].Device)
	}
	if result.Results[0].Status != "success" {
		t.Errorf("expected status 'success', got %q", result.Results[0].Status)
	}
	if result.Results[0].Data == nil {
		t.Error("expected data, got nil")
	}
}

func TestExecute_MultipleDevices(t *testing.T) {
	sources := []string{"dc1/spine-1/default", "dc1/spine-2/default", "dc1/leaf-1/default"}
	result := Execute(context.Background(), sources, 10, func(ctx context.Context, sourceName string) (any, error) {
		return map[string]string{"source": sourceName}, nil
	})

	if len(result.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result.Results))
	}

	// Results should be sorted by source name
	expected := []string{"leaf-1", "spine-1", "spine-2"}
	for i, exp := range expected {
		if result.Results[i].Device != exp {
			t.Errorf("result[%d]: expected device %q, got %q", i, exp, result.Results[i].Device)
		}
		if result.Results[i].Status != "success" {
			t.Errorf("result[%d]: expected status 'success', got %q", i, result.Results[i].Status)
		}
	}
}

func TestExecute_PartialFailure(t *testing.T) {
	sources := []string{"dc1/spine-1/default", "dc1/spine-2/default"}
	result := Execute(context.Background(), sources, 10, func(ctx context.Context, sourceName string) (any, error) {
		if sourceName == "dc1/spine-2/default" {
			return nil, fmt.Errorf("connection refused")
		}
		return "ok", nil
	})

	if len(result.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result.Results))
	}

	// Sorted: spine-1, spine-2
	if result.Results[0].Status != "success" {
		t.Errorf("spine-1 should succeed, got %q", result.Results[0].Status)
	}
	if result.Results[1].Status != "error" {
		t.Errorf("spine-2 should fail, got %q", result.Results[1].Status)
	}
	if result.Results[1].Error != "connection refused" {
		t.Errorf("expected 'connection refused', got %q", result.Results[1].Error)
	}
}

func TestExecute_BoundedConcurrency(t *testing.T) {
	var maxActive atomic.Int32
	var currentActive atomic.Int32

	sources := make([]string, 20)
	for i := range sources {
		sources[i] = fmt.Sprintf("dc1/device-%02d/default", i)
	}

	result := Execute(context.Background(), sources, 3, func(ctx context.Context, sourceName string) (any, error) {
		cur := currentActive.Add(1)
		for {
			old := maxActive.Load()
			if cur <= old {
				break
			}
			if maxActive.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		currentActive.Add(-1)
		return "ok", nil
	})

	if len(result.Results) != 20 {
		t.Fatalf("expected 20 results, got %d", len(result.Results))
	}

	peak := maxActive.Load()
	if peak > 3 {
		t.Errorf("concurrency exceeded limit: peak was %d, expected <= 3", peak)
	}
	if peak < 1 {
		t.Errorf("concurrency too low: peak was %d", peak)
	}
}

func TestExecute_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	sources := []string{"dc1/spine-1/default"}
	result := Execute(ctx, sources, 10, func(ctx context.Context, sourceName string) (any, error) {
		return "should not reach", nil
	})

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Status != "error" {
		t.Errorf("expected error status on cancelled context, got %q", result.Results[0].Status)
	}
}

func TestExecute_EmptySourceList(t *testing.T) {
	result := Execute(context.Background(), []string{}, 10, func(ctx context.Context, sourceName string) (any, error) {
		return nil, nil
	})

	if len(result.Results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(result.Results))
	}
}

func TestExecute_DefaultConcurrency(t *testing.T) {
	// maxConcurrency <= 0 should fall back to default
	result := Execute(context.Background(), []string{"dc1/s1/default"}, 0, func(ctx context.Context, sourceName string) (any, error) {
		return "ok", nil
	})

	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0].Status != "success" {
		t.Errorf("expected success, got %q", result.Results[0].Status)
	}
}

func TestExtractDeviceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"dc1/spine-1/default", "spine-1"},
		{"group/device/template", "device"},
		{"simple-name", "simple-name"},
		{"one-slash/only", "one-slash/only"},
		{"a/b/c/d", "b"},
	}
	for _, tt := range tests {
		got := extractDeviceName(tt.input)
		if got != tt.expected {
			t.Errorf("extractDeviceName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
