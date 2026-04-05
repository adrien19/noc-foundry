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

// Package capabilities defines interfaces for device source capabilities
// that tools depend on. Sources implement these interfaces; tools consume them.
package capabilities

import "context"

// CommandRunner represents a source that can execute CLI commands on a device.
type CommandRunner interface {
	RunCommand(ctx context.Context, command string) (string, error)
}

// GnmiQuerier represents a source that can execute gNMI Get (snapshot) RPCs.
// Subscriptions are out of scope for this interface.
type GnmiQuerier interface {
	GnmiGet(ctx context.Context, paths []string, encoding string) (*GnmiGetResult, error)
}

// NetconfQuerier represents a source that can execute NETCONF Get and
// GetConfig RPCs (RFC 6241). filter is a raw XML subtree body placed inside
// <filter type="subtree">…</filter>; pass an empty string for no filter.
type NetconfQuerier interface {
	NetconfGet(ctx context.Context, filter string) ([]byte, error)
	NetconfGetConfig(ctx context.Context, datastore, filter string) ([]byte, error)
}

// GnmiGetResult holds the raw response from a gNMI Get RPC.
type GnmiGetResult struct {
	Notifications []GnmiNotification
}

// GnmiNotification represents a single notification in a gNMI GetResponse.
type GnmiNotification struct {
	Timestamp int64  // nanoseconds since epoch
	Path      string // string-form of the gNMI path
	Value     []byte // JSON-encoded value
}

// SourceCapabilities describes what protocols a source supports for
// read-only data retrieval. Used by the query executor for routing.
type SourceCapabilities struct {
	GnmiSnapshot    bool // supports gNMI Get RPCs
	OpenConfigPaths bool // supports OpenConfig YANG paths
	NativeYang      bool // supports vendor-native YANG paths
	CLI             bool // supports CLI command execution
	Netconf         bool // supports NETCONF Get/GetConfig RPCs
}

// CapabilityProvider is implemented by sources that can report their
// protocol capabilities to the query executor.
type CapabilityProvider interface {
	Capabilities() SourceCapabilities
}

// SourceIdentity provides vendor and platform metadata for profile
// resolution. Sources that expose this allow the query executor to
// look up the correct model profile.
type SourceIdentity interface {
	DeviceVendor() string
	DevicePlatform() string
}
