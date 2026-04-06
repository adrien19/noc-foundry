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

package ssh_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/sources/ssh"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/trace/noop"
)

var testTracer = noop.NewTracerProvider().Tracer("test")

func TestParseFromYamlSSH(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want server.SourceConfigs
	}{
		{
			desc: "basic example with defaults",
			in: `
			kind: sources
			name: my-device
			type: ssh
			host: 10.0.0.1
			username: admin
			password: secret
			`,
			want: map[string]sources.SourceConfig{
				"my-device": ssh.Config{
					Name:     "my-device",
					Type:     ssh.SourceType,
					Host:     "10.0.0.1",
					Port:     22,
					Username: "admin",
					Password: "secret",
					Timeout:  "30s",
				},
			},
		},
		{
			desc: "full example with all fields",
			in: `
			kind: sources
			name: nokia-core-1
			type: ssh
			host: 192.168.1.10
			port: 830
			username: netops
			password: s3cret
			timeout: 10s
			vendor: nokia
			platform: sros
			`,
			want: map[string]sources.SourceConfig{
				"nokia-core-1": ssh.Config{
					Name:     "nokia-core-1",
					Type:     ssh.SourceType,
					Host:     "192.168.1.10",
					Port:     830,
					Username: "netops",
					Password: "s3cret",
					Timeout:  "10s",
					Vendor:   "nokia",
					Platform: "sros",
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if !cmp.Equal(tc.want, got) {
				t.Fatalf("incorrect parse: want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestFailParseFromYamlSSH(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
	}{
		{
			desc: "extra field",
			in: `
			kind: sources
			name: my-device
			type: ssh
			host: 10.0.0.1
			username: admin
			password: secret
			unknownField: value
			`,
		},
		{
			desc: "missing type",
			in: `
			kind: sources
			name: my-device
			host: 10.0.0.1
			username: admin
			password: secret
			`,
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err == nil {
				t.Fatal("expected error but got nil")
			}
		})
	}
}

// TestInitializeLazyNoConnection verifies that Initialize succeeds
// without establishing a TCP connection. The connection is deferred
// to first RunCommand call.
func TestInitializeLazyNoConnection(t *testing.T) {
	cfg := ssh.Config{
		Name:     "lazy-test",
		Type:     ssh.SourceType,
		Host:     "192.0.2.1", // TEST-NET-1, unreachable
		Port:     22,
		Username: "admin",
		Password: "secret",
		Timeout:  "1s",
	}

	source, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize should succeed lazily, got error: %v", err)
	}

	if source.SourceType() != ssh.SourceType {
		t.Errorf("SourceType() = %q, want %q", source.SourceType(), ssh.SourceType)
	}
}

// TestInitializeValidation verifies that config validation still
// catches errors at init time (before any connection attempt).
func TestInitializeValidation(t *testing.T) {
	tcs := []struct {
		desc string
		cfg  ssh.Config
	}{
		{
			desc: "missing host",
			cfg: ssh.Config{
				Name: "test", Type: ssh.SourceType,
				Username: "admin", Password: "secret", Timeout: "1s",
			},
		},
		{
			desc: "missing username",
			cfg: ssh.Config{
				Name: "test", Type: ssh.SourceType,
				Host: "10.0.0.1", Password: "secret", Timeout: "1s",
			},
		},
		{
			desc: "missing password",
			cfg: ssh.Config{
				Name: "test", Type: ssh.SourceType,
				Host: "10.0.0.1", Username: "admin", Timeout: "1s",
			},
		},
		{
			desc: "invalid timeout",
			cfg: ssh.Config{
				Name: "test", Type: ssh.SourceType,
				Host: "10.0.0.1", Username: "admin", Password: "secret",
				Timeout: "notaduration",
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			_, err := tc.cfg.Initialize(context.Background(), testTracer)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

// TestRunCommandConnectOnFirstUse verifies that RunCommand returns a
// connection error (not a nil pointer panic) when the device is unreachable.
// This confirms the lazy connect path is exercised.
func TestRunCommandConnectOnFirstUse(t *testing.T) {
	cfg := ssh.Config{
		Name:     "lazy-connect-test",
		Type:     ssh.SourceType,
		Host:     "192.0.2.1", // TEST-NET-1, unreachable
		Port:     22,
		Username: "admin",
		Password: "secret",
		Timeout:  "1s",
	}

	source, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize should succeed lazily: %v", err)
	}

	// Extract CommandRunner interface
	type commandRunner interface {
		RunCommand(ctx context.Context, command string) (string, error)
	}
	runner, ok := source.(commandRunner)
	if !ok {
		t.Fatal("source does not implement CommandRunner")
	}

	// RunCommand should fail with a connection error, not panic
	_, err = runner.RunCommand(context.Background(), "show version")
	if err == nil {
		t.Fatal("expected connection error from RunCommand on unreachable host")
	}
}

func TestRunCommandHonorsCanceledContextBeforeDial(t *testing.T) {
	cfg := ssh.Config{
		Name:     "cancelled-before-dial",
		Type:     ssh.SourceType,
		Host:     "192.0.2.1",
		Port:     22,
		Username: "admin",
		Password: "secret",
		Timeout:  "30s",
	}

	source, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize should succeed lazily: %v", err)
	}

	type commandRunner interface {
		RunCommand(ctx context.Context, command string) (string, error)
	}
	runner, ok := source.(commandRunner)
	if !ok {
		t.Fatal("source does not implement CommandRunner")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err = runner.RunCommand(ctx, "show version")
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if ctx.Err() == nil || !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("expected cancelled dial error, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("RunCommand should return promptly for cancelled context, took %v", elapsed)
	}
}

// TestCloseBeforeConnect verifies that Close is safe to call
// even if no connection was ever established.
func TestCloseBeforeConnect(t *testing.T) {
	cfg := ssh.Config{
		Name:     "close-test",
		Type:     ssh.SourceType,
		Host:     "192.0.2.1",
		Port:     22,
		Username: "admin",
		Password: "secret",
		Timeout:  "1s",
	}

	source, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize should succeed lazily: %v", err)
	}

	type closer interface {
		Close() error
	}
	c, ok := source.(closer)
	if !ok {
		t.Fatal("source does not implement Close")
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close on unconnected source should succeed: %v", err)
	}
}

// testKeyPEM is a passphrase-less ed25519 key used only in tests.
const testKeyPEM = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACDM4BLMmxp7j18E0ZUBK4Z4BrRKMHNgE99M+DBNfqWLgQAAAIhWyhGEVsoR
hAAAAAtzc2gtZWQyNTUxOQAAACDM4BLMmxp7j18E0ZUBK4Z4BrRKMHNgE99M+DBNfqWLgQ
AAAEBw2Yz1HIZY1R8tmeK/OVcwvaWbuNeZ7gl4oxmT6LbLn8zgEsybGnuPXwTRlQErhngG
tEowc2AT30z4ME1+pYuBAAAABHRlc3QB
-----END OPENSSH PRIVATE KEY-----
`

// TestInitializeSSHKeyNoPassword verifies that key-only auth (no password) is
// accepted and that Initialize succeeds without establishing a connection.
func TestInitializeSSHKeyNoPassword(t *testing.T) {
	cfg := ssh.Config{
		Name:       "key-only",
		Type:       ssh.SourceType,
		Host:       "192.0.2.1", // TEST-NET-1, unreachable
		Port:       22,
		Username:   "admin",
		SSHKeyData: testKeyPEM,
		Timeout:    "1s",
	}

	_, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize with key-only auth should succeed lazily: %v", err)
	}
}

// TestInitializeSSHNoAuth verifies that Initialize returns an error when
// neither a password nor an SSH key is provided.
func TestInitializeSSHNoAuth(t *testing.T) {
	cfg := ssh.Config{
		Name:     "no-auth",
		Type:     ssh.SourceType,
		Host:     "10.0.0.1",
		Port:     22,
		Username: "admin",
		Timeout:  "1s",
		// no Password, no SSHKeyPath, no SSHKeyData
	}

	_, err := cfg.Initialize(context.Background(), testTracer)
	if err == nil {
		t.Fatal("expected error when no auth method provided, got nil")
	}
}

// TestParseSSHKeyFields verifies that ssh_key_path is parsed correctly
// from YAML and stored in the Config struct.
func TestParseSSHKeyFields(t *testing.T) {
	in := `
	kind: sources
	name: key-device
	type: ssh
	host: 10.0.0.1
	username: admin
	ssh_key_path: /home/ops/.ssh/id_ed25519
	ssh_key_passphrase: mysecret
	`

	got, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(in))
	if err != nil {
		t.Fatalf("unable to unmarshal: %v", err)
	}

	want := server.SourceConfigs{
		"key-device": ssh.Config{
			Name:             "key-device",
			Type:             ssh.SourceType,
			Host:             "10.0.0.1",
			Port:             22,
			Username:         "admin",
			SSHKeyPath:       "/home/ops/.ssh/id_ed25519",
			SSHKeyPassphrase: "mysecret",
			Timeout:          "30s",
		},
	}

	if !cmp.Equal(want, got) {
		t.Fatalf("incorrect parse: want %v, got %v", want, got)
	}
}
