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
	"context"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/sources"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestNewDevicePool(t *testing.T) {
	configs := []*Config{
		{
			Name: "group-a",
			Type: "static",
			SourceTemplates: map[string]map[string]any{
				"ssh": {"type": "ssh", "vendor": "nokia", "platform": "srlinux"},
			},
			Devices: []DeviceEntry{
				{Name: "r1", Host: "10.0.0.1", Labels: map[string]string{"role": "spine"}},
				{Name: "r2", Host: "10.0.0.2", Labels: map[string]string{"role": "leaf"}},
			},
		},
	}

	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), configs, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	// Check source names
	names := pool.SourceNames()
	if len(names) != 2 {
		t.Fatalf("SourceNames() returned %d names, want 2", len(names))
	}
	if names[0] != "group-a/r1/ssh" || names[1] != "group-a/r2/ssh" {
		t.Errorf("SourceNames() = %v, want [group-a/r1/ssh group-a/r2/ssh]", names)
	}

	// Check HasSource
	if !pool.HasSource("group-a/r1/ssh") {
		t.Error("HasSource(group-a/r1/ssh) = false, want true")
	}
	if pool.HasSource("nonexistent/r1/ssh") {
		t.Error("HasSource(nonexistent/r1/ssh) = true, want false")
	}

	// Check label selection
	spines := pool.SelectSources(LabelSelector{MatchLabels: map[string]string{"role": "spine"}})
	if len(spines) != 1 || spines[0] != "group-a/r1/ssh" {
		t.Errorf("SelectSources(role=spine) = %v, want [group-a/r1/ssh]", spines)
	}
}

func TestNewDevicePoolDuplicateGroup(t *testing.T) {
	configs := []*Config{
		{Name: "dup", Type: "static", SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}}, Devices: []DeviceEntry{{Name: "r1", Host: "10.0.0.1"}}},
		{Name: "dup", Type: "static", SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}}, Devices: []DeviceEntry{{Name: "r2", Host: "10.0.0.2"}}},
	}
	tracer := noop.NewTracerProvider().Tracer("test")
	_, err := NewDevicePool(context.Background(), configs, tracer)
	if err == nil {
		t.Fatal("expected error for duplicate group name, got nil")
	}
}

func TestNewDevicePoolDuplicateSourceName(t *testing.T) {
	// Two groups with overlapping device×template names
	configs := []*Config{
		{Name: "lab", Type: "static", SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}}, Devices: []DeviceEntry{{Name: "r1", Host: "10.0.0.1"}}},
		{Name: "lab2", Type: "static", SourceTemplates: map[string]map[string]any{"ssh": {"type": "ssh"}}, Devices: []DeviceEntry{{Name: "r1", Host: "10.0.0.2"}}},
	}
	tracer := noop.NewTracerProvider().Tracer("test")
	// These have different group names so different source names — should NOT conflict
	pool, err := NewDevicePool(context.Background(), configs, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() unexpected error: %v", err)
	}
	if len(pool.SourceNames()) != 2 {
		t.Errorf("SourceNames() = %d, want 2", len(pool.SourceNames()))
	}
}

func TestDevicePoolLabels(t *testing.T) {
	configs := []*Config{
		{
			Name: "lab",
			Type: "static",
			SourceTemplates: map[string]map[string]any{
				"ssh":  {"type": "ssh", "vendor": "nokia", "platform": "srlinux"},
				"gnmi": {"type": "gnmi", "vendor": "nokia", "platform": "srlinux"},
			},
			Devices: []DeviceEntry{
				{Name: "r1", Host: "10.0.0.1", Labels: map[string]string{"role": "spine", "site": "dc1"}},
			},
		},
	}

	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), configs, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	labels := pool.Labels()
	if len(labels) != 2 {
		t.Fatalf("Labels() has %d entries, want 2", len(labels))
	}

	sshLabels := labels["lab/r1/ssh"]
	if sshLabels["group"] != "lab" {
		t.Errorf("group = %q, want lab", sshLabels["group"])
	}
	if sshLabels["role"] != "spine" {
		t.Errorf("role = %q, want spine", sshLabels["role"])
	}
	if sshLabels["template"] != "ssh" {
		t.Errorf("template = %q, want ssh", sshLabels["template"])
	}

	// Select by multiple criteria
	results := pool.SelectSources(LabelSelector{MatchLabels: map[string]string{
		"role":     "spine",
		"template": "gnmi",
	}})
	if len(results) != 1 || results[0] != "lab/r1/gnmi" {
		t.Errorf("SelectSources(role=spine,template=gnmi) = %v, want [lab/r1/gnmi]", results)
	}
}

func TestEvictSource(t *testing.T) {
	pool := &DevicePool{
		states: map[string]*groupState{},
		cache:  map[string]sources.Source{},
	}

	ms := &mockSource{closed: false}
	pool.cache["test-source"] = ms

	// Verify source is cached
	pool.mu.Lock()
	_, ok := pool.cache["test-source"]
	pool.mu.Unlock()
	if !ok {
		t.Fatal("expected source to be in cache before eviction")
	}

	// Evict it
	pool.EvictSource("test-source")

	// Verify source is removed from cache
	pool.mu.Lock()
	_, ok = pool.cache["test-source"]
	pool.mu.Unlock()
	if ok {
		t.Fatal("expected source to be removed from cache after eviction")
	}

	// Verify Close was called
	if !ms.closed {
		t.Fatal("expected Close to be called on evicted source")
	}
}

func TestEvictSourceNotInCache(t *testing.T) {
	pool := &DevicePool{
		states: map[string]*groupState{},
		cache:  map[string]sources.Source{},
	}

	// Should not panic
	pool.EvictSource("nonexistent")
}

// mockSource is a minimal source for testing cache eviction.
type mockSource struct {
	closed bool
}

func (m *mockSource) SourceType() string             { return "mock" }
func (m *mockSource) ToConfig() sources.SourceConfig { return nil }
func (m *mockSource) Close() error {
	m.closed = true
	return nil
}

// --- TTL / snapshot behaviour tests ---

// boolPtr is a helper for *bool fields in CacheConfig.
func boolPtr(b bool) *bool { return &b }

func staticGroupConfig(name string, devices []DeviceEntry) *Config {
	return &Config{
		Name: name,
		Type: "static",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh", "vendor": "nokia", "platform": "srlinux"},
		},
		Devices: devices,
	}
}

// TestSnapshotBuiltOnDemand verifies that a snapshot is built lazily on first
// access rather than at pool construction time.
func TestSnapshotBuiltOnDemand(t *testing.T) {
	cfg := staticGroupConfig("lab", []DeviceEntry{
		{Name: "r1", Host: "10.0.0.1"},
	})
	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), []*Config{cfg}, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	// Snapshot should not yet exist.
	pool.states["lab"].mu.Lock()
	snap := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()
	if snap != nil {
		t.Fatal("expected snapshot to be nil before first access")
	}

	// First access triggers snapshot build.
	names := pool.SourceNames()
	if len(names) != 1 || names[0] != "lab/r1/ssh" {
		t.Errorf("SourceNames() = %v, want [lab/r1/ssh]", names)
	}

	// Snapshot should now be populated.
	pool.states["lab"].mu.Lock()
	snap = pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()
	if snap == nil {
		t.Fatal("expected snapshot to be populated after first access")
	}
}

// TestStartupWarmPreBuildsSnapshot verifies that startupWarm:true causes the
// snapshot to be built during NewDevicePool.
func TestStartupWarmPreBuildsSnapshot(t *testing.T) {
	warm := true
	cfg := &Config{
		Name: "lab",
		Type: "static",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
		},
		Cache: &CacheConfig{StartupWarm: &warm},
	}
	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), []*Config{cfg}, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	pool.states["lab"].mu.Lock()
	snap := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()
	if snap == nil {
		t.Fatal("expected snapshot to be pre-warmed at startup")
	}
}

// TestSnapshotRefreshedAfterTTLExpiry verifies that accessing the pool after
// snapshot expiry triggers a new build, updating the label index.
func TestSnapshotRefreshedAfterTTLExpiry(t *testing.T) {
	cfg := &Config{
		Name: "lab",
		Type: "static",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		Devices: []DeviceEntry{
			{Name: "r1", Host: "10.0.0.1"},
		},
		Cache: &CacheConfig{
			TTL:         "1ms", // expire instantly
			RefreshMode: "blocking",
		},
	}
	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), []*Config{cfg}, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	// Force first snapshot build.
	_ = pool.SourceNames()

	pool.states["lab"].mu.Lock()
	snap1 := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()

	// Wait for expiry and access again.
	<-time.After(5 * time.Millisecond)

	_ = pool.SourceNames()

	pool.states["lab"].mu.Lock()
	snap2 := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()

	if snap1 == snap2 {
		t.Error("expected a new snapshot after TTL expiry, got the same pointer")
	}
	if !snap2.RefreshedAt.After(snap1.RefreshedAt) {
		t.Errorf("snap2.RefreshedAt (%v) should be after snap1.RefreshedAt (%v)", snap2.RefreshedAt, snap1.RefreshedAt)
	}
}

// TestFailOpenServesStaleOnError verifies that a refresh failure does not
// evict the existing snapshot when failOpen is true (the default).
func TestFailOpenServesStaleOnError(t *testing.T) {
	// Use the mock provider registered in resolve_test.go.
	cfg := &Config{
		Name: "lab",
		Type: "static",
		SourceTemplates: map[string]map[string]any{
			"ssh": {"type": "ssh"},
		},
		InventoryProviders: []map[string]any{
			{"type": "mock", "devices": []any{
				map[string]any{"name": "r1", "host": "10.0.0.1"},
			}},
		},
		Cache: &CacheConfig{
			TTL:         "1ms",
			RefreshMode: "blocking",
			FailOpen:    boolPtr(true),
		},
	}
	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), []*Config{cfg}, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	// Build first snapshot.
	names := pool.SourceNames()
	if len(names) != 1 {
		t.Fatalf("initial SourceNames() = %v, want 1 entry", names)
	}

	pool.states["lab"].mu.Lock()
	staleSnap := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()
	if staleSnap == nil {
		t.Fatal("expected stale snapshot to be set")
	}

	// Replace the provider with one that returns an error.
	cfg.InventoryProviders = []map[string]any{
		{"type": "mock", "error": "provider unavailable"},
	}

	// Wait for TTL to expire.
	<-time.After(5 * time.Millisecond)

	// Access should return stale data instead of an error.
	names2 := pool.SourceNames()
	if len(names2) != 1 {
		t.Errorf("after failing refresh, SourceNames() = %v, want 1 entry (stale)", names2)
	}
}

// TestRefreshGroup forces an explicit refresh bypassing the TTL.
func TestRefreshGroup(t *testing.T) {
	cfg := staticGroupConfig("lab", []DeviceEntry{
		{Name: "r1", Host: "10.0.0.1"},
	})
	tracer := noop.NewTracerProvider().Tracer("test")
	pool, err := NewDevicePool(context.Background(), []*Config{cfg}, tracer)
	if err != nil {
		t.Fatalf("NewDevicePool() error: %v", err)
	}

	// Build initial snapshot.
	_ = pool.SourceNames()

	pool.states["lab"].mu.Lock()
	snap1 := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()

	// Force refresh (even though TTL has not expired).
	if err := pool.RefreshGroup(context.Background(), "lab"); err != nil {
		t.Fatalf("RefreshGroup() error: %v", err)
	}

	pool.states["lab"].mu.Lock()
	snap2 := pool.states["lab"].snapshot
	pool.states["lab"].mu.Unlock()

	if snap1 == snap2 {
		t.Error("expected a new snapshot after RefreshGroup, got the same pointer")
	}
}

// TestRefreshGroupUnknown verifies that RefreshGroup returns an error for an
// unknown group name.
func TestRefreshGroupUnknown(t *testing.T) {
	pool := &DevicePool{
		states: map[string]*groupState{},
		cache:  map[string]sources.Source{},
	}
	err := pool.RefreshGroup(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown group, got nil")
	}
}

// TestCacheConfigDefaults verifies the default values returned when CacheConfig
// is nil or has empty fields.
func TestCacheConfigDefaults(t *testing.T) {
	var c *CacheConfig

	if got := c.effectiveTTL(); got != 5*time.Minute {
		t.Errorf("effectiveTTL(nil) = %v, want 5m", got)
	}
	if got := c.effectiveRefreshMode(); got != "stale_while_revalidate" {
		t.Errorf("effectiveRefreshMode(nil) = %q, want stale_while_revalidate", got)
	}
	if got := c.effectiveStartupWarm(); got != false {
		t.Errorf("effectiveStartupWarm(nil) = %v, want false", got)
	}
	if got := c.effectiveFailOpen(); got != true {
		t.Errorf("effectiveFailOpen(nil) = %v, want true", got)
	}

	// Explicit empty struct should use same defaults.
	empty := &CacheConfig{}
	if got := empty.effectiveTTL(); got != 5*time.Minute {
		t.Errorf("effectiveTTL({}) = %v, want 5m", got)
	}
	if got := empty.effectiveRefreshMode(); got != "stale_while_revalidate" {
		t.Errorf("effectiveRefreshMode({}) = %q, want stale_while_revalidate", got)
	}
}

// TestCacheConfigCustom verifies that explicit CacheConfig values are honoured.
func TestCacheConfigCustom(t *testing.T) {
	f := false
	c := &CacheConfig{
		TTL:         "10m",
		RefreshMode: "blocking",
		StartupWarm: boolPtr(true),
		FailOpen:    &f,
	}
	if got := c.effectiveTTL(); got != 10*time.Minute {
		t.Errorf("effectiveTTL = %v, want 10m", got)
	}
	if got := c.effectiveRefreshMode(); got != "blocking" {
		t.Errorf("effectiveRefreshMode = %q, want blocking", got)
	}
	if got := c.effectiveStartupWarm(); got != true {
		t.Errorf("effectiveStartupWarm = %v, want true", got)
	}
	if got := c.effectiveFailOpen(); got != false {
		t.Errorf("effectiveFailOpen = %v, want false", got)
	}
}
