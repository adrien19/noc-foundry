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

package netconf_test

import (
	"context"
	"testing"

	"github.com/adrien19/noc-foundry/internal/server"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/sources/netconf"
	"github.com/adrien19/noc-foundry/internal/testutils"
	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel/trace/noop"
)

var testTracer = noop.NewTracerProvider().Tracer("test")

func TestParseFromYamlNetconf(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
		want server.SourceConfigs
	}{
		{
			desc: "basic example with defaults",
			in: `
			kind: sources
			name: my-netconf-device
			type: netconf
			host: 10.0.0.1
			username: admin
			password: secret
			`,
			want: map[string]sources.SourceConfig{
				"my-netconf-device": netconf.Config{
					Name:     "my-netconf-device",
					Type:     netconf.SourceType,
					Host:     "10.0.0.1",
					Port:     830,
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
			name: spine-1-netconf
			type: netconf
			host: 192.168.1.10
			port: 830
			username: netops
			password: s3cret
			ssh_key_path: /etc/ssh/keys/spine1.pem
			timeout: 10s
			vendor: nokia
			platform: srlinux
			`,
			want: map[string]sources.SourceConfig{
				"spine-1-netconf": netconf.Config{
					Name:       "spine-1-netconf",
					Type:       netconf.SourceType,
					Host:       "192.168.1.10",
					Port:       830,
					Username:   "netops",
					Password:   "s3cret",
					SSHKeyPath: "/etc/ssh/keys/spine1.pem",
					Timeout:    "10s",
					Vendor:     "nokia",
					Platform:   "srlinux",
				},
			},
		},
		{
			desc: "key-only auth (no password)",
			in: `
			kind: sources
			name: key-only-device
			type: netconf
			host: 10.0.0.2
			username: admin
			ssh_key_data: |
			  -----BEGIN OPENSSH PRIVATE KEY-----
			  dummydata
			  -----END OPENSSH PRIVATE KEY-----
			`,
			want: map[string]sources.SourceConfig{
				"key-only-device": netconf.Config{
					Name:       "key-only-device",
					Type:       netconf.SourceType,
					Host:       "10.0.0.2",
					Port:       830,
					Username:   "admin",
					SSHKeyData: "-----BEGIN OPENSSH PRIVATE KEY-----\ndummydata\n-----END OPENSSH PRIVATE KEY-----\n",
					Timeout:    "30s",
				},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.desc, func(t *testing.T) {
			got, _, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err != nil {
				t.Fatalf("unable to unmarshal: %s", err)
			}
			if !cmp.Equal(tc.want, got) {
				t.Fatalf("incorrect parse: want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestFailParseFromYamlNetconf(t *testing.T) {
	tcs := []struct {
		desc string
		in   string
	}{
		{
			desc: "extra unknown field",
			in: `
			kind: sources
			name: my-device
			type: netconf
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
			_, _, _, _, _, _, _, _, _, err := server.UnmarshalResourceConfig(context.Background(), testutils.FormatYaml(tc.in))
			if err == nil {
				t.Fatal("expected error but got nil")
			}
		})
	}
}

// TestInitializeLazyNoConnection verifies that Initialize succeeds
// without establishing a TCP connection — connection is deferred to
// the first RPC call.
func TestInitializeLazyNoConnection(t *testing.T) {
	cfg := netconf.Config{
		Name:     "lazy-test",
		Type:     netconf.SourceType,
		Host:     "192.0.2.1", // TEST-NET-1, unreachable
		Port:     830,
		Username: "admin",
		Password: "secret",
		Timeout:  "1s",
	}

	src, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize should succeed lazily, got error: %v", err)
	}

	if src.SourceType() != netconf.SourceType {
		t.Errorf("SourceType() = %q, want %q", src.SourceType(), netconf.SourceType)
	}
}

// TestInitializeValidation verifies that config validation errors are
// returned at init time, before any TCP connection is attempted.
func TestInitializeValidation(t *testing.T) {
	tcs := []struct {
		desc string
		cfg  netconf.Config
	}{
		{
			desc: "missing host",
			cfg: netconf.Config{
				Name: "test", Type: netconf.SourceType,
				Username: "admin", Password: "secret", Timeout: "1s",
			},
		},
		{
			desc: "missing username",
			cfg: netconf.Config{
				Name: "test", Type: netconf.SourceType,
				Host: "10.0.0.1", Password: "secret", Timeout: "1s",
			},
		},
		{
			desc: "no auth method",
			cfg: netconf.Config{
				Name: "test", Type: netconf.SourceType,
				Host: "10.0.0.1", Username: "admin", Timeout: "1s",
				// no password, no key
			},
		},
		{
			desc: "invalid timeout",
			cfg: netconf.Config{
				Name: "test", Type: netconf.SourceType,
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

// TestNetconfGetConnectError verifies that NetconfGet returns a connection
// error (not a nil pointer panic) when the device is unreachable.
func TestNetconfGetConnectError(t *testing.T) {
	cfg := netconf.Config{
		Name:     "unreachable",
		Type:     netconf.SourceType,
		Host:     "192.0.2.1", // TEST-NET-1, unreachable
		Port:     830,
		Username: "admin",
		Password: "secret",
		Timeout:  "1s",
	}

	src, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize() unexpected error: %v", err)
	}

	ns, ok := src.(interface {
		NetconfGet(ctx context.Context, filter string) ([]byte, error)
	})
	if !ok {
		t.Fatal("source does not implement NetconfGet")
	}

	_, err = ns.NetconfGet(context.Background(), "")
	if err == nil {
		t.Fatal("expected connection error from NetconfGet on unreachable host, got nil")
	}
}

// TestCloseBeforeConnect verifies that Close on a never-connected source
// is safe and returns nil.
func TestCloseBeforeConnect(t *testing.T) {
	cfg := netconf.Config{
		Name:     "close-test",
		Type:     netconf.SourceType,
		Host:     "192.0.2.1",
		Port:     830,
		Username: "admin",
		Password: "secret",
		Timeout:  "1s",
	}

	src, err := cfg.Initialize(context.Background(), testTracer)
	if err != nil {
		t.Fatalf("Initialize() unexpected error: %v", err)
	}

	closer, ok := src.(interface {
		Close(ctx context.Context) error
	})
	if !ok {
		t.Fatal("source does not implement Close(ctx)")
	}

	if err := closer.Close(context.Background()); err != nil {
		t.Errorf("Close() on unconnected source returned error: %v", err)
	}
}
