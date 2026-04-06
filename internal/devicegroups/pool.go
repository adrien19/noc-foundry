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
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/util"
	"go.opentelemetry.io/otel/trace"
)

// InventorySnapshot is an immutable point-in-time view of a device group's
// inventory: the resolved device list, the label index, and the sorted source
// names.  Snapshots are replaced atomically; readers always see a consistent
// view.
type InventorySnapshot struct {
	Devices     []DeviceEntry
	Labels      map[string]map[string]string
	Names       []string
	RefreshedAt time.Time
	ExpiresAt   time.Time
}

// groupState holds the runtime inventory state for a single device group.
// The mu lock protects snapshot, refreshing, and lastRefreshErr only; it
// must never be held while calling provider.Discover (network I/O).
type groupState struct {
	cfg            *Config
	mu             sync.Mutex
	snapshot       *InventorySnapshot
	refreshing     bool
	lastRefreshErr error
}

// DevicePool manages lazy creation and caching of sources from device groups.
// Inventory snapshots are built on demand and cached with a configurable TTL,
// so large or dynamic fleets no longer require a full discovery pass at
// startup.  Per-device source connections are still created lazily (only when
// a tool first requests them).
type DevicePool struct {
	states map[string]*groupState // groupName → runtime state
	mu     sync.Mutex             // protects the source connection cache only
	cache  map[string]sources.Source
	tracer trace.Tracer
}

// NewDevicePool creates a pool from a list of device group configs.
// It validates group names for duplicates but does NOT eagerly discover
// devices from inventory providers unless a group's cache.startupWarm is true.
func NewDevicePool(ctx context.Context, configs []*Config, tracer trace.Tracer) (*DevicePool, error) {
	states := make(map[string]*groupState, len(configs))
	for _, cfg := range configs {
		if _, exists := states[cfg.Name]; exists {
			return nil, fmt.Errorf("duplicate device group name: %q", cfg.Name)
		}
		states[cfg.Name] = &groupState{cfg: cfg}
	}

	pool := &DevicePool{
		states: states,
		cache:  make(map[string]sources.Source),
		tracer: tracer,
	}

	// Pre-warm snapshots for groups that opt in.
	for name, state := range states {
		if state.cfg.Cache.effectiveStartupWarm() {
			if _, err := pool.doRefresh(ctx, state); err != nil {
				return nil, fmt.Errorf("device group %q startup warm failed: %w", name, err)
			}
		}
	}

	return pool, nil
}

// buildSnapshot performs a full inventory discovery for cfg and returns an
// immutable InventorySnapshot.  It does not mutate cfg.
func buildSnapshot(ctx context.Context, cfg *Config) (*InventorySnapshot, error) {
	devices, err := resolveDevices(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// Build the label index using a temporary Config that already has devices
	// resolved, so BuildLabelIndex picks them up without mutating cfg.
	tmp := &Config{
		Name:            cfg.Name,
		SourceTemplates: cfg.SourceTemplates,
		resolvedDevices: devices,
		resolved:        true,
	}
	labels := tmp.BuildLabelIndex()

	names := make([]string, 0, len(labels))
	for n := range labels {
		names = append(names, n)
	}
	sort.Strings(names)

	ttl := cfg.Cache.effectiveTTL()
	now := time.Now()
	return &InventorySnapshot{
		Devices:     devices,
		Labels:      labels,
		Names:       names,
		RefreshedAt: now,
		ExpiresAt:   now.Add(ttl),
	}, nil
}

// ensureSnapshot returns a usable snapshot for state, respecting the group's
// cache policy.
//
//   - If the current snapshot is still fresh it is returned immediately.
//   - "stale_while_revalidate": a stale snapshot is returned while a
//     background refresh is scheduled (at most one concurrent refresh).
//   - "blocking": a stale or missing snapshot triggers a synchronous refresh.
//   - "manual": the existing snapshot (even if stale/nil) is returned as-is.
func (p *DevicePool) ensureSnapshot(ctx context.Context, state *groupState) (*InventorySnapshot, error) {
	state.mu.Lock()
	snap := state.snapshot
	state.mu.Unlock()

	if snap != nil && time.Now().Before(snap.ExpiresAt) {
		return snap, nil // fast path: fresh
	}

	mode := state.cfg.Cache.effectiveRefreshMode()

	if snap != nil {
		switch mode {
		case "manual":
			return snap, nil
		case "stale_while_revalidate":
			p.scheduleBackgroundRefresh(state)
			return snap, nil
		}
		// "blocking": fall through to synchronous refresh
	}

	// No snapshot yet, or blocking mode: refresh synchronously.
	return p.doRefresh(ctx, state)
}

// scheduleBackgroundRefresh triggers an async snapshot refresh for state,
// ensuring at most one refresh is in-flight at a time.
func (p *DevicePool) scheduleBackgroundRefresh(state *groupState) {
	state.mu.Lock()
	if state.refreshing {
		state.mu.Unlock()
		return
	}
	state.refreshing = true
	state.mu.Unlock()

	go func() {
		snap, err := buildSnapshot(context.Background(), state.cfg)
		state.mu.Lock()
		defer state.mu.Unlock()
		state.refreshing = false
		if err == nil {
			state.snapshot = snap
			state.lastRefreshErr = nil
		} else {
			state.lastRefreshErr = err
		}
	}()
}

// doRefresh performs a synchronous snapshot refresh.  If another goroutine
// is already refreshing, this call waits for that refresh to complete rather
// than starting a second concurrent discovery.
func (p *DevicePool) doRefresh(ctx context.Context, state *groupState) (*InventorySnapshot, error) {
	state.mu.Lock()
	if state.refreshing {
		// Another goroutine is building the snapshot already.
		existing := state.snapshot
		state.mu.Unlock()
		if existing != nil && state.cfg.Cache.effectiveFailOpen() {
			return existing, nil
		}
		// Poll until the in-flight refresh completes.
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-ticker.C:
				state.mu.Lock()
				snap := state.snapshot
				inFlight := state.refreshing
				lastErr := state.lastRefreshErr
				state.mu.Unlock()
				if !inFlight {
					if snap != nil {
						return snap, nil
					}
					return nil, lastErr
				}
			}
		}
	}
	state.refreshing = true
	state.mu.Unlock()

	snap, err := buildSnapshot(ctx, state.cfg)

	state.mu.Lock()
	defer state.mu.Unlock()
	state.refreshing = false

	if err != nil {
		state.lastRefreshErr = err
		if state.snapshot != nil && state.cfg.Cache.effectiveFailOpen() {
			return state.snapshot, nil // serve stale on failure
		}
		return nil, err
	}
	state.snapshot = snap
	state.lastRefreshErr = nil
	return snap, nil
}

// HasSource returns true if the pool can provide a source with the given name.
func (p *DevicePool) HasSource(name string) bool {
	parts := strings.SplitN(name, "/", 3)
	if len(parts) != 3 {
		return false
	}
	state, ok := p.states[parts[0]]
	if !ok {
		return false
	}
	snap, err := p.ensureSnapshot(context.Background(), state)
	if err != nil || snap == nil {
		return false
	}
	_, exists := snap.Labels[name]
	return exists
}

// SourceNames returns all source names the pool can provide across all groups.
func (p *DevicePool) SourceNames() []string {
	var all []string
	for _, state := range p.states {
		snap, err := p.ensureSnapshot(context.Background(), state)
		if err != nil || snap == nil {
			continue
		}
		all = append(all, snap.Names...)
	}
	sort.Strings(all)
	return all
}

// Labels returns the merged label index for all sources across all groups.
func (p *DevicePool) Labels() map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, state := range p.states {
		snap, err := p.ensureSnapshot(context.Background(), state)
		if err != nil || snap == nil {
			continue
		}
		for name, labels := range snap.Labels {
			result[name] = labels
		}
	}
	return result
}

// SelectSources returns source names matching the given selector.
func (p *DevicePool) SelectSources(selector LabelSelector) []string {
	return SelectSources(selector, p.Labels())
}

// GetOrCreate returns a cached source or creates one on-demand.
// The source is built lazily from the device group's current snapshot.
func (p *DevicePool) GetOrCreate(ctx context.Context, name string) (sources.Source, error) {
	// Fast path: already cached.
	p.mu.Lock()
	if s, ok := p.cache[name]; ok {
		p.mu.Unlock()
		return s, nil
	}
	p.mu.Unlock()

	parts := strings.SplitN(name, "/", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid device pool source name %q: expected group/device/template", name)
	}
	groupName, deviceName, templateName := parts[0], parts[1], parts[2]

	state, ok := p.states[groupName]
	if !ok {
		return nil, fmt.Errorf("device group %q not found", groupName)
	}

	snap, err := p.ensureSnapshot(ctx, state)
	if err != nil {
		return nil, fmt.Errorf("device group %q: failed to load inventory: %w", groupName, err)
	}

	// Find device in snapshot.
	var device *DeviceEntry
	for i := range snap.Devices {
		if snap.Devices[i].Name == deviceName {
			d := snap.Devices[i]
			device = &d
			break
		}
	}
	if device == nil {
		return nil, fmt.Errorf("device %q not found in group %q", deviceName, groupName)
	}

	template, ok := state.cfg.SourceTemplates[templateName]
	if !ok {
		return nil, fmt.Errorf("source template %q not found in group %q", templateName, groupName)
	}

	// Acquire source cache lock for creation (double-check to avoid duplicates).
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.cache[name]; ok {
		return s, nil
	}

	sourceMap := buildSourceMap(name, device, template)
	sourceType, ok := sourceMap["type"].(string)
	if !ok {
		return nil, fmt.Errorf("source template %q in group %q missing 'type'", templateName, groupName)
	}

	dec, err := util.NewStrictDecoder(sourceMap)
	if err != nil {
		return nil, fmt.Errorf("error creating decoder for source %q: %w", name, err)
	}

	sourceConfig, err := sources.DecodeConfig(ctx, sourceType, name, dec)
	if err != nil {
		return nil, fmt.Errorf("error decoding source config for %q: %w", name, err)
	}

	source, err := sourceConfig.Initialize(ctx, p.tracer)
	if err != nil {
		return nil, fmt.Errorf("error initializing source %q: %w", name, err)
	}

	p.cache[name] = source
	return source, nil
}

// buildSourceMap merges template fields with device-specific overrides into
// a raw source config map ready for decoding.
func buildSourceMap(name string, device *DeviceEntry, template map[string]any) map[string]any {
	sourceMap := make(map[string]any, len(template)+8)
	for k, v := range template {
		sourceMap[k] = v
	}
	sourceMap["name"] = name
	sourceMap["host"] = device.Host
	if device.Port != nil {
		sourceMap["port"] = *device.Port
	}
	if device.Username != "" {
		sourceMap["username"] = device.Username
	}
	if device.Password != "" {
		sourceMap["password"] = device.Password
	}
	if device.SSHKeyPath != "" {
		sourceMap["ssh_key_path"] = device.SSHKeyPath
	}
	if device.SSHKeyData != "" {
		sourceMap["ssh_key_data"] = device.SSHKeyData
	}
	if device.SSHKeyPassphrase != "" {
		sourceMap["ssh_key_passphrase"] = device.SSHKeyPassphrase
	}
	return sourceMap
}

// Close closes all cached sources that implement io.Closer.
func (p *DevicePool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.cache {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	p.cache = make(map[string]sources.Source)
}

// EvictSource removes a source from the cache and closes it. The next
// call to GetOrCreate for this source will create a fresh connection.
func (p *DevicePool) EvictSource(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if s, ok := p.cache[name]; ok {
		if closer, ok := s.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
		delete(p.cache, name)
	}
}

// RefreshGroup forces an immediate synchronous refresh of the inventory
// snapshot for the named device group, bypassing the TTL.
func (p *DevicePool) RefreshGroup(ctx context.Context, groupName string) error {
	state, ok := p.states[groupName]
	if !ok {
		return fmt.Errorf("device group %q not found", groupName)
	}
	_, err := p.doRefresh(ctx, state)
	return err
}
