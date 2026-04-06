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

// Package fanout provides parallel execution of operations across multiple
// network devices with bounded concurrency and partial failure handling.
package fanout

import (
	"context"
	"sort"
	"sync"
)

// DefaultMaxConcurrency is the default limit on simultaneous device operations.
const DefaultMaxConcurrency = 10

// DeviceResult holds the result of an operation on a single device.
type DeviceResult struct {
	Device string `json:"device"`
	Status string `json:"status"` // "success" or "error"
	Data   any    `json:"data,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Result holds the aggregated results from a fan-out operation.
type Result struct {
	Results []DeviceResult `json:"results"`
}

// DeviceFunc is the function executed per device. It receives the device's
// source name and returns the result data or an error.
type DeviceFunc func(ctx context.Context, sourceName string) (any, error)

// Execute runs fn across all sourceNames with bounded concurrency.
// Results are returned in sorted source name order for determinism.
// Individual device failures are captured per-result; Execute itself
// only returns an error for systemic issues (e.g., context canceled
// before any work started).
func Execute(ctx context.Context, sourceNames []string, maxConcurrency int, fn DeviceFunc) Result {
	if maxConcurrency <= 0 {
		maxConcurrency = DefaultMaxConcurrency
	}

	results := make([]DeviceResult, len(sourceNames))
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	// Sort for deterministic output order
	sorted := make([]string, len(sourceNames))
	copy(sorted, sourceNames)
	sort.Strings(sorted)

	for i, name := range sorted {
		wg.Add(1)
		go func(idx int, sourceName string) {
			defer wg.Done()

			// Acquire semaphore slot
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = DeviceResult{
					Device: extractDeviceName(sourceName),
					Status: "error",
					Error:  ctx.Err().Error(),
				}
				return
			}

			// Check context again after acquiring semaphore
			if ctx.Err() != nil {
				results[idx] = DeviceResult{
					Device: extractDeviceName(sourceName),
					Status: "error",
					Error:  ctx.Err().Error(),
				}
				return
			}

			data, err := fn(ctx, sourceName)
			if err != nil {
				results[idx] = DeviceResult{
					Device: extractDeviceName(sourceName),
					Status: "error",
					Error:  err.Error(),
				}
			} else {
				results[idx] = DeviceResult{
					Device: extractDeviceName(sourceName),
					Status: "success",
					Data:   data,
				}
			}
		}(i, name)
	}

	wg.Wait()
	return Result{Results: results}
}

// extractDeviceName extracts the device name from a "group/device/template"
// source name. Returns the full name if parsing fails.
func extractDeviceName(sourceName string) string {
	// Find the segment between first and second '/'
	first := -1
	for i, c := range sourceName {
		if c == '/' {
			if first < 0 {
				first = i
			} else {
				return sourceName[first+1 : i]
			}
		}
	}
	return sourceName
}
