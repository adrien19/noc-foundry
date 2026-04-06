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

// Package gnmi implements a gNMI source for snapshot (Get RPC) queries
// against network devices. Subscribe RPCs are out of scope.
package gnmi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/goccy/go-yaml"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

const SourceType string = "gnmi"

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
		actual.Port = 57400
	}
	return actual, nil
}

// Config holds the YAML-decoded configuration for a gNMI source.
type Config struct {
	Name     string `yaml:"name" validate:"required"`
	Type     string `yaml:"type" validate:"required"`
	Host     string `yaml:"host" validate:"required"`
	Port     int    `yaml:"port"`
	Username string `yaml:"username" validate:"required"`
	Password string `yaml:"password" validate:"required"`
	Timeout  string `yaml:"timeout"`
	Vendor   string `yaml:"vendor"`
	Platform string `yaml:"platform"`

	// TLS configuration.
	TLSInsecure bool   `yaml:"tls_insecure"`
	TLSCACert   string `yaml:"tls_ca_cert"`
	TLSCert     string `yaml:"tls_cert"`
	TLSKey      string `yaml:"tls_key"`

	// OpenConfig indicates whether the device supports OpenConfig YANG models.
	OpenConfig bool `yaml:"openconfig"`
	// NativeYang indicates whether the device supports vendor-native YANG models.
	NativeYang bool `yaml:"native_yang"`
}

func (c Config) SourceConfigType() string {
	return SourceType
}

// Initialize creates a connected gNMI source ready for Get RPCs.
func (c Config) Initialize(ctx context.Context, tracer trace.Tracer) (sources.Source, error) {
	if tracer != nil {
		var span trace.Span
		_, span = sources.InitConnectionSpan(ctx, tracer, SourceType, c.Name)
		defer span.End()
	}

	if c.Host == "" {
		return nil, fmt.Errorf("host is required for gNMI source %q", c.Name)
	}
	if c.Username == "" {
		return nil, fmt.Errorf("username is required for gNMI source %q", c.Name)
	}
	if c.Password == "" {
		return nil, fmt.Errorf("password is required for gNMI source %q", c.Name)
	}
	if c.Port <= 0 || c.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535 for gNMI source %q", c.Name)
	}
	timeout, err := time.ParseDuration(c.Timeout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse timeout %q for gNMI source %q: %w", c.Timeout, c.Name, err)
	}

	target := fmt.Sprintf("%s:%d", c.Host, c.Port)

	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(64 * 1024 * 1024)),
	}

	if c.TLSInsecure {
		// tls_insecure skips certificate verification but still uses TLS —
		// SR Linux gNMI speaks TLS on its gRPC port.
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(
			&tls.Config{InsecureSkipVerify: true}, //nolint:gosec // intentional for lab/dev
		)))
	} else if c.TLSCACert != "" || c.TLSCert != "" || c.TLSKey != "" {
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		if c.TLSCACert != "" {
			caPEM, err := os.ReadFile(c.TLSCACert) // #nosec G304 — path from trusted config
			if err != nil {
				return nil, fmt.Errorf("gNMI source %q: reading tls_ca_cert %q: %w", c.Name, c.TLSCACert, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				return nil, fmt.Errorf("gNMI source %q: no valid certificates in tls_ca_cert %q", c.Name, c.TLSCACert)
			}
			tlsConfig.RootCAs = pool
		}
		if c.TLSCert != "" || c.TLSKey != "" {
			if c.TLSCert == "" || c.TLSKey == "" {
				return nil, fmt.Errorf("gNMI source %q: tls_cert and tls_key must both be set", c.Name)
			}
			cert, err := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
			if err != nil {
				return nil, fmt.Errorf("gNMI source %q: loading tls_cert/tls_key: %w", c.Name, err)
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	} else {
		// Default: verify server certificates using system roots.
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)))
	}

	conn, err := grpc.NewClient(target, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to gNMI source %q at %s: %w", c.Name, target, err)
	}

	client := pb.NewGNMIClient(conn)

	return &Source{
		Config:  c,
		conn:    conn,
		client:  client,
		timeout: timeout,
	}, nil
}

var _ sources.Source = &Source{}
var _ capabilities.GnmiQuerier = &Source{}
var _ capabilities.SourceIdentity = &Source{}
var _ capabilities.CapabilityProvider = &Source{}

// Source represents an active gNMI connection to a network device.
type Source struct {
	Config
	conn    *grpc.ClientConn
	client  pb.GNMIClient
	timeout time.Duration
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

// Capabilities reports that this gNMI source supports gNMI Get RPCs.
func (s *Source) Capabilities() capabilities.SourceCapabilities {
	return capabilities.SourceCapabilities{
		GnmiSnapshot:    true,
		OpenConfigPaths: s.OpenConfig,
		NativeYang:      s.NativeYang,
		CLI:             false,
	}
}

// GnmiGet executes a gNMI Get RPC for the given paths and returns
// the response notifications as a structured result.
func (s *Source) GnmiGet(ctx context.Context, paths []string, encoding string) (*capabilities.GnmiGetResult, error) {
	opCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	gnmiPaths := make([]*pb.Path, 0, len(paths))
	for _, p := range paths {
		gnmiPath, err := parsePath(p)
		if err != nil {
			return nil, fmt.Errorf("invalid gNMI path %q: %w", p, err)
		}
		gnmiPaths = append(gnmiPaths, gnmiPath)
	}

	enc := pb.Encoding_JSON_IETF
	switch encoding {
	case "JSON":
		enc = pb.Encoding_JSON
	case "PROTO":
		enc = pb.Encoding_PROTO
	}

	req := &pb.GetRequest{
		Path:     gnmiPaths,
		Encoding: enc,
	}

	// Attach credentials as per-RPC metadata (required by SR Linux and most
	// vendor gNMI implementations that use HTTP Basic-style auth over gRPC).
	if s.Username != "" {
		opCtx = metadata.AppendToOutgoingContext(opCtx,
			"username", s.Username,
			"password", s.Password,
		)
	}

	resp, err := s.client.Get(opCtx, req)
	if err != nil {
		return nil, fmt.Errorf("gNMI Get failed: %w", err)
	}

	result := &capabilities.GnmiGetResult{}
	for _, n := range resp.GetNotification() {
		for _, u := range n.GetUpdate() {
			path := pathToString(u.GetPath())
			val := u.GetVal()
			var jsonBytes []byte
			if val != nil {
				if jv := val.GetJsonIetfVal(); jv != nil {
					jsonBytes = jv
				} else if jv := val.GetJsonVal(); jv != nil {
					jsonBytes = jv
				} else {
					// For non-JSON values, marshal the scalar.
					jsonBytes, _ = json.Marshal(val.String())
				}
			}
			result.Notifications = append(result.Notifications, capabilities.GnmiNotification{
				Timestamp: n.GetTimestamp(),
				Path:      path,
				Value:     jsonBytes,
			})
		}
	}

	return result, nil
}

// Close terminates the underlying gRPC connection.
func (s *Source) Close() error {
	if s.conn != nil {
		return s.conn.Close()
	}
	return nil
}

// parsePath converts a string-form gNMI path to a protobuf Path.
// Supports "/" separated paths with optional key-value selectors.
// Example: "/interfaces/interface[name=eth0]/state"
func parsePath(path string) (*pb.Path, error) {
	if path == "" {
		return &pb.Path{}, nil
	}

	// Strip leading /
	if path[0] == '/' {
		path = path[1:]
	}

	var elems []*pb.PathElem
	for _, segment := range splitPath(path) {
		elem, err := parsePathElem(segment)
		if err != nil {
			return nil, err
		}
		elems = append(elems, elem)
	}
	return &pb.Path{Elem: elems}, nil
}

// splitPath splits a gNMI path string by "/" while respecting brackets.
func splitPath(path string) []string {
	var parts []string
	var current []byte
	depth := 0
	for i := 0; i < len(path); i++ {
		switch path[i] {
		case '[':
			depth++
			current = append(current, path[i])
		case ']':
			depth--
			current = append(current, path[i])
		case '/':
			if depth == 0 {
				if len(current) > 0 {
					parts = append(parts, string(current))
				}
				current = current[:0]
			} else {
				current = append(current, path[i])
			}
		default:
			current = append(current, path[i])
		}
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	return parts
}

// parsePathElem parses a path segment like "interface[name=eth0]" into a PathElem.
func parsePathElem(segment string) (*pb.PathElem, error) {
	idx := indexOf(segment, '[')
	if idx < 0 {
		return &pb.PathElem{Name: segment}, nil
	}

	name := segment[:idx]
	keys := make(map[string]string)
	rest := segment[idx:]

	for len(rest) > 0 {
		if rest[0] != '[' {
			return nil, fmt.Errorf("expected '[' in path segment %q", segment)
		}
		end := indexOf(rest, ']')
		if end < 0 {
			return nil, fmt.Errorf("unclosed '[' in path segment %q", segment)
		}
		kv := rest[1:end]
		eq := indexOf(kv, '=')
		if eq < 0 {
			return nil, fmt.Errorf("missing '=' in key-value %q", kv)
		}
		keys[kv[:eq]] = kv[eq+1:]
		rest = rest[end+1:]
	}

	return &pb.PathElem{Name: name, Key: keys}, nil
}

// pathToString converts a protobuf Path to a string-form path.
func pathToString(path *pb.Path) string {
	if path == nil {
		return "/"
	}
	result := ""
	for _, elem := range path.GetElem() {
		result += "/" + elem.GetName()
		for k, v := range elem.GetKey() {
			result += fmt.Sprintf("[%s=%s]", k, v)
		}
	}
	if result == "" {
		return "/"
	}
	return result
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
