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

package ssh

import (
	"bytes"
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
)

const SourceType string = "ssh"

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
		actual.Port = 22
	}
	return actual, nil
}

// Config holds the YAML-decoded configuration for an SSH source.
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
}

func (c Config) SourceConfigType() string {
	return SourceType
}

// Initialize creates an SSH source. The actual TCP connection is deferred
// until the first command is executed (lazy connect). This avoids blocking
// startup when managing large device fleets and enables automatic
// reconnection when connections drop.
func (c Config) Initialize(ctx context.Context, tracer trace.Tracer) (sources.Source, error) {
	if tracer != nil {
		var span trace.Span
		_, span = sources.InitConnectionSpan(ctx, tracer, SourceType, c.Name)
		defer span.End()
	}

	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse timeout %q: %w", c.Timeout, err)
	}

	if c.Host == "" {
		return nil, fmt.Errorf("host is required for SSH source %q", c.Name)
	}
	if c.Username == "" {
		return nil, fmt.Errorf("username is required for SSH source %q", c.Name)
	}

	authMethods, err := sshauth.BuildAuthMethods(c.Password, sshauth.KeyAuth{
		Path:       c.SSHKeyPath,
		Data:       c.SSHKeyData,
		Passphrase: c.SSHKeyPassphrase,
	})
	if err != nil {
		return nil, fmt.Errorf("SSH source %q: %w", c.Name, err)
	}

	var hostKeyCallback ssh.HostKeyCallback
	if c.KnownHostsFile != "" {
		var err error
		hostKeyCallback, err = knownhosts.New(c.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("SSH source %q: loading known_hosts_file %q: %w", c.Name, c.KnownHostsFile, err)
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

var _ sources.Source = &Source{}

// Verify that Source implements the CommandRunner capability.
var _ capabilities.CommandRunner = &Source{}

// Verify that Source implements identity and capability interfaces.
var _ capabilities.SourceIdentity = &Source{}
var _ capabilities.CapabilityProvider = &Source{}

// Source represents a lazily-connected SSH source to a network device.
// The connection is established on first use and automatically
// re-established if it drops.
type Source struct {
	Config
	sshConfig *ssh.ClientConfig
	addr      string
	timeout   time.Duration

	mu     sync.Mutex
	client *ssh.Client
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

// Capabilities reports that this SSH source supports CLI only.
func (s *Source) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{
		GnmiSnapshot:    false,
		OpenConfigPaths: false,
		NativeYang:      false,
		CLI:             true,
	}
}

// dial creates a new SSH connection. Caller must hold s.mu.
func (s *Source) dial(ctx context.Context) (*ssh.Client, error) {
	// Bound TCP connect and SSH handshake to both the source timeout and the
	// caller context so unreachable devices don't block shutdown.
	dialCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	dialer := &net.Dialer{Timeout: s.timeout}
	conn, err := dialer.DialContext(dialCtx, "tcp", s.addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to SSH source %q at %s: %w", s.Name, s.addr, err)
	}
	stopWatch := context.AfterFunc(dialCtx, func() {
		_ = conn.Close()
	})
	defer stopWatch()
	c, chans, reqs, err := ssh.NewClientConn(conn, s.addr, s.sshConfig)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to connect to SSH source %q at %s: %w", s.Name, s.addr, err)
	}
	client := ssh.NewClient(c, chans, reqs)
	return client, nil
}

// getClient returns the current client, dialing if needed.
func (s *Source) getClient(ctx context.Context) (*ssh.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	client, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	s.client = client
	return client, nil
}

// reconnect closes the stale client and dials a new one.
// Only reconnects if the current client matches staleClient (prevents
// redundant reconnects when multiple goroutines detect the same failure).
func (s *Source) reconnect(ctx context.Context, staleClient *ssh.Client) (*ssh.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Another goroutine may have already reconnected.
	if s.client != nil && s.client != staleClient {
		return s.client, nil
	}
	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
	client, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	s.client = client
	return client, nil
}

// RunCommand executes a single command on the device and returns stdout.
// If the SSH session cannot be created (dead connection), it automatically
// reconnects once and retries.
func (s *Source) RunCommand(ctx context.Context, command string) (string, error) {
	opCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	client, err := s.getClient(opCtx)
	if err != nil {
		return "", err
	}

	session, err := client.NewSession()
	if err != nil {
		// Connection is likely dead — try reconnecting once.
		client, err = s.reconnect(opCtx, client)
		if err != nil {
			return "", fmt.Errorf("reconnection failed for SSH source %q: %w", s.Name, err)
		}
		session, err = client.NewSession()
		if err != nil {
			return "", fmt.Errorf("failed to create SSH session after reconnect: %w", err)
		}
	}
	defer func() { _ = session.Close() }()

	var stdout, stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr
	stopWatch := context.AfterFunc(opCtx, func() {
		_ = session.Close()
		_ = client.Close()
	})
	defer stopWatch()

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case <-opCtx.Done():
		return "", opCtx.Err()
	case err := <-done:
		if err != nil {
			return "", fmt.Errorf("command %q failed: %w (stderr: %s)", command, err, stderr.String())
		}
	}

	return stdout.String(), nil
}

// Close terminates the underlying SSH connection.
func (s *Source) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		err := s.client.Close()
		s.client = nil
		return err
	}
	return nil
}
