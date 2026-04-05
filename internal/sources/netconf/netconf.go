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

// Package netconf implements a NETCONF source (RFC 6241) over SSH.
// The TCP connection is established lazily on first RPC call and automatically
// re-established if the session drops.
package netconf

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/sources/sshauth"
	"github.com/goccy/go-yaml"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"nemith.io/netconf"
	"nemith.io/netconf/rpc"
	ncssh "nemith.io/netconf/transport/ssh"
)

// SourceType is the string key used in YAML `type:` fields to select this source.
const SourceType string = "netconf"

// validate interface
var _ sources.SourceConfig = Config{}

func init() {
	if !sources.Register(SourceType, newConfig) {
		panic(fmt.Sprintf("source type %q already registered", SourceType))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (sources.SourceConfig, error) {
	actual := Config{
		Name:    name,
		Timeout: "30s",
	}
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	// Apply default port after decoding so that a value from YAML (including
	// one substituted from an environment variable) always wins. A zero value
	// here means the port was not specified in YAML.
	if actual.Port == 0 {
		actual.Port = 830
	}
	return actual, nil
}

// Config holds the YAML-decoded configuration for a NETCONF source.
// Authentication requires at least one of: password, ssh_key_path, or ssh_key_data.
type Config struct {
	Name             string `yaml:"name" validate:"required"`
	Type             string `yaml:"type" validate:"required"`
	Host             string `yaml:"host" validate:"required"`
	Port             int    `yaml:"port"`
	Username         string `yaml:"username" validate:"required"`
	Password         string `yaml:"password"`
	SSHKeyPath       string `yaml:"ssh_key_path"`
	SSHKeyData       string `yaml:"ssh_key_data"`
	SSHKeyPassphrase string `yaml:"ssh_key_passphrase"`
	Timeout          string `yaml:"timeout"`
	Vendor           string `yaml:"vendor"`
	Platform         string `yaml:"platform"`
	// KnownHostsFile is the path to a known_hosts file for SSH host key verification.
	// When set, the server's host key is verified against this file (strict checking).
	// When empty, host key verification is skipped — insecure, suitable for lab use only.
	KnownHostsFile string `yaml:"known_hosts_file"`
	// OpenConfig indicates whether the device supports OpenConfig YANG models
	// over NETCONF. When true, OpenConfig-based protocol paths are eligible.
	OpenConfig bool `yaml:"openconfig"`
	// NativeYang indicates whether the device supports vendor-native YANG models
	// over NETCONF. When true, native YANG protocol paths are eligible.
	NativeYang bool `yaml:"native_yang"`
}

func (c Config) SourceConfigType() string {
	return SourceType
}

// Initialize validates the config and returns a lazily-connected NETCONF source.
// No TCP connection is made here; it is deferred to the first RPC call.
func (c Config) Initialize(ctx context.Context, tracer trace.Tracer) (sources.Source, error) {
	if tracer != nil {
		var span trace.Span
		_, span = sources.InitConnectionSpan(ctx, tracer, SourceType, c.Name)
		defer span.End()
	}

	if c.Host == "" {
		return nil, fmt.Errorf("host is required for NETCONF source %q", c.Name)
	}
	if c.Username == "" {
		return nil, fmt.Errorf("username is required for NETCONF source %q", c.Name)
	}

	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse timeout %q for NETCONF source %q: %w", c.Timeout, c.Name, err)
	}

	authMethods, err := sshauth.BuildAuthMethods(c.Password, sshauth.KeyAuth{
		Path:       c.SSHKeyPath,
		Data:       c.SSHKeyData,
		Passphrase: c.SSHKeyPassphrase,
	})
	if err != nil {
		return nil, fmt.Errorf("NETCONF source %q: %w", c.Name, err)
	}

	var hostKeyCallback ssh.HostKeyCallback
	if c.KnownHostsFile != "" {
		hostKeyCallback, err = knownhosts.New(c.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("NETCONF source %q: loading known_hosts_file %q: %w", c.Name, c.KnownHostsFile, err)
		}
	} else {
		hostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // host key verification disabled; set known_hosts_file to enable
	}

	sshConfig := &ssh.ClientConfig{
		User:            c.Username,
		Auth:            authMethods,
		Timeout:         timeout,
		HostKeyCallback: hostKeyCallback,
	}

	addr := net.JoinHostPort(c.Host, fmt.Sprintf("%d", c.Port))

	return &Source{
		Config:    c,
		sshConfig: sshConfig,
		addr:      addr,
		timeout:   timeout,
	}, nil
}

// Compile-time interface assertions.
var _ sources.Source = &Source{}
var _ capabilities.NetconfQuerier = &Source{}
var _ capabilities.SourceIdentity = &Source{}
var _ capabilities.CapabilityProvider = &Source{}

// Source represents a lazily-connected NETCONF source.
// The SSH+NETCONF session is established on first use and automatically
// re-established if the session drops.
type Source struct {
	Config
	sshConfig *ssh.ClientConfig
	addr      string
	timeout   time.Duration

	mu      sync.Mutex
	session *netconf.Session
}

func (s *Source) SourceType() string {
	return SourceType
}

func (s *Source) ToConfig() sources.SourceConfig {
	return s.Config
}

// DeviceVendor returns the configured vendor for profile resolution.
func (s *Source) DeviceVendor() string {
	return s.Vendor
}

// DevicePlatform returns the configured platform for profile resolution.
func (s *Source) DevicePlatform() string {
	return s.Platform
}

// Capabilities reports that this NETCONF source supports NETCONF RPCs.
// OpenConfigPaths and NativeYang reflect the openconfig/native_yang config flags.
func (s *Source) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{
		Netconf:         true,
		OpenConfigPaths: s.OpenConfig,
		NativeYang:      s.NativeYang,
	}
}

// dial creates a new NETCONF session over SSH. Caller must hold s.mu.
// ctx should already carry a deadline; ncssh.Dial cancels the SSH handshake
// on ctx.Done() and NewSession respects WithHelloTimeout.
func (s *Source) dial(ctx context.Context) (*netconf.Session, error) {
	transport, err := ncssh.Dial(ctx, "tcp", s.addr, s.sshConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to NETCONF source %q at %s: %w", s.Name, s.addr, err)
	}
	session, err := netconf.NewSession(transport, netconf.WithHelloTimeout(s.timeout))
	if err != nil {
		transport.Close() //nolint:errcheck
		return nil, fmt.Errorf("failed to establish NETCONF session for source %q: %w", s.Name, err)
	}
	return session, nil
}

// getSession returns the current session, dialing if needed.
func (s *Source) getSession(ctx context.Context) (*netconf.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		return s.session, nil
	}
	session, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	s.session = session
	return session, nil
}

// reconnect closes the stale session and dials a new one.
// Only reconnects if the current session matches staleSession (prevents
// redundant reconnects when multiple goroutines detect the same failure).
func (s *Source) reconnect(ctx context.Context, staleSession *netconf.Session) (*netconf.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil && s.session != staleSession {
		return s.session, nil
	}
	if s.session != nil {
		s.session.Close(ctx) //nolint:errcheck
		s.session = nil
	}
	session, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	s.session = session
	return session, nil
}

// subtreeFilter returns a rpc.Filter for a non-empty subtree string, or nil.
func subtreeFilter(filter string) rpc.Filter {
	if filter == "" {
		return nil
	}
	return rpc.SubtreeFilter(filter)
}

// NetconfGet executes a NETCONF <get> operation.
// filter is the raw XML body of the subtree filter element; pass "" for no filter.
// Returns the inner XML bytes of the <data> element.
func (s *Source) NetconfGet(ctx context.Context, filter string) ([]byte, error) {
	// Bound the entire operation — session dial (SSH handshake) + RPC — to the
	// source timeout. op.Exec honours context cancellation in its Do() select,
	// but nemith calls io.ReadAll after Do() returns, which does NOT check ctx.
	// forceCloseCurrentSession is registered via AfterFunc to close the
	// transport when the deadline fires, unblocking any pending io.ReadAll.
	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	session, err := s.getSession(rpcCtx)
	if err != nil {
		return nil, err
	}

	stopWatch := context.AfterFunc(rpcCtx, s.forceCloseCurrentSession)
	defer stopWatch()

	op := &rpc.Get{Filter: subtreeFilter(filter)}
	result, err := op.Exec(rpcCtx, session)
	if err != nil {
		// Session may have dropped — reconnect once and retry.
		session, err = s.reconnect(rpcCtx, session)
		if err != nil {
			return nil, fmt.Errorf("reconnection failed for NETCONF source %q: %w", s.Name, err)
		}
		result, err = op.Exec(rpcCtx, session)
		if err != nil {
			return nil, fmt.Errorf("NETCONF <get> failed for source %q: %w", s.Name, err)
		}
	}
	return result, nil
}

// NetconfGetConfig executes a NETCONF <get-config> operation.
// datastore must be one of "running", "candidate", or "startup".
// filter is the raw XML body of the subtree filter element; pass "" for no filter.
// Returns the inner XML bytes of the <data> element.
func (s *Source) NetconfGetConfig(ctx context.Context, datastore, filter string) ([]byte, error) {
	// Same timeout + force-close pattern as NetconfGet.
	// See NetconfGet for rationale.
	rpcCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	session, err := s.getSession(rpcCtx)
	if err != nil {
		return nil, err
	}

	stopWatch := context.AfterFunc(rpcCtx, s.forceCloseCurrentSession)
	defer stopWatch()

	op := rpc.GetConfig{
		Source: rpc.Datastore(datastore),
		Filter: subtreeFilter(filter),
	}
	result, err := op.Exec(rpcCtx, session)
	if err != nil {
		// Session may have dropped — reconnect once and retry.
		session, err = s.reconnect(rpcCtx, session)
		if err != nil {
			return nil, fmt.Errorf("reconnection failed for NETCONF source %q: %w", s.Name, err)
		}
		result, err = op.Exec(rpcCtx, session)
		if err != nil {
			return nil, fmt.Errorf("NETCONF <get-config> failed for source %q: %w", s.Name, err)
		}
	}
	return result, nil
}

// forceCloseCurrentSession closes the active NETCONF session and evicts it
// from the cache. It is registered via context.AfterFunc so it runs in its
// own goroutine when the RPC deadline fires. Forcibly closing the transport
// is the only safe way to unblock nemith's io.ReadAll, which does not
// propagate context cancellation mid-read.
func (s *Source) forceCloseCurrentSession() {
	s.mu.Lock()
	current := s.session
	s.session = nil
	s.mu.Unlock()
	if current != nil {
		_ = current.Close(context.Background())
	}
}

// Close terminates the NETCONF session gracefully.
// Safe to call before any RPC has been executed (no-op when not connected).
func (s *Source) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	err := s.session.Close(ctx)
	s.session = nil
	return err
}
