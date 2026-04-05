// Copyright 2025 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/goccy/go-yaml"
	"go.opentelemetry.io/otel/trace"
)

const SourceType string = "http"

// validate interface
var _ sources.SourceConfig = Config{}

func init() {
	if !sources.Register(SourceType, newConfig) {
		panic(fmt.Sprintf("source type %q already registered", SourceType))
	}
}

func newConfig(ctx context.Context, name string, decoder *yaml.Decoder) (sources.SourceConfig, error) {
	actual := Config{Name: name, Timeout: "30s"} // Default timeout
	if err := decoder.DecodeContext(ctx, &actual); err != nil {
		return nil, err
	}
	return actual, nil
}

type Config struct {
	Name                   string            `yaml:"name" validate:"required"`
	Type                   string            `yaml:"type" validate:"required"`
	BaseURL                string            `yaml:"baseUrl"`
	Timeout                string            `yaml:"timeout"`
	DefaultHeaders         map[string]string `yaml:"headers"`
	QueryParams            map[string]string `yaml:"queryParams"`
	DisableSslVerification bool              `yaml:"disableSslVerification"`
	// TLSCACert is the path to a PEM-encoded CA certificate file used to verify the server.
	// When set, the server's certificate must chain to this CA instead of the system roots.
	TLSCACert string `yaml:"tls_ca_cert"`
	// TLSCert and TLSKey are the paths to a PEM-encoded client certificate and private key
	// for mutual TLS authentication. Both must be set together.
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`
}

func (r Config) SourceConfigType() string {
	return SourceType
}

// Initialize initializes an HTTP Source instance.
func (r Config) Initialize(ctx context.Context, tracer trace.Tracer) (sources.Source, error) {
	duration, err := time.ParseDuration(r.Timeout)
	if err != nil {
		return nil, fmt.Errorf("unable to parse Timeout string as time.Duration: %s", err)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("timeout must be greater than 0 for HTTP source %q", r.Name)
	}
	if r.BaseURL == "" {
		return nil, fmt.Errorf("baseUrl is required for HTTP source %q", r.Name)
	}

	logger, err := util.LoggerFromContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to get logger from ctx: %s", err)
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}

	if r.DisableSslVerification {
		tlsConfig.InsecureSkipVerify = true //nolint:gosec // intentional for lab/dev
		logger.WarnContext(ctx, fmt.Sprintf("Insecure HTTP is enabled for source %q. TLS certificate verification is skipped.", r.Name))
	}

	if r.TLSCACert != "" {
		caPEM, err := os.ReadFile(r.TLSCACert) // #nosec G304 — path from trusted config
		if err != nil {
			return nil, fmt.Errorf("HTTP source %q: reading tls_ca_cert %q: %w", r.Name, r.TLSCACert, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("HTTP source %q: no valid certificates in tls_ca_cert %q", r.Name, r.TLSCACert)
		}
		tlsConfig.RootCAs = pool
	}

	if r.TLSCert != "" || r.TLSKey != "" {
		if r.TLSCert == "" || r.TLSKey == "" {
			return nil, fmt.Errorf("HTTP source %q: tls_cert and tls_key must both be set", r.Name)
		}
		cert, err := tls.LoadX509KeyPair(r.TLSCert, r.TLSKey)
		if err != nil {
			return nil, fmt.Errorf("HTTP source %q: loading tls_cert/tls_key: %w", r.Name, err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	tr := &http.Transport{TLSClientConfig: tlsConfig}

	client := http.Client{
		Timeout:   duration,
		Transport: tr,
	}

	// Validate BaseURL
	_, err = url.ParseRequestURI(r.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse BaseUrl %v", err)
	}

	ua, err := util.UserAgentFromContext(ctx)
	if err != nil {
		logger.WarnContext(ctx, fmt.Sprintf("Unable to retrieve user agent from context for source %q: %v", r.Name, err))
		ua = "noc-foundry/unknown"
	}
	if r.DefaultHeaders == nil {
		r.DefaultHeaders = make(map[string]string)
	}
	if existingUA, ok := r.DefaultHeaders["User-Agent"]; ok {
		ua = ua + " " + existingUA
	}
	r.DefaultHeaders["User-Agent"] = ua

	s := &Source{
		Config: r,
		client: &client,
	}
	return s, nil

}

var _ sources.Source = &Source{}

type Source struct {
	Config
	client *http.Client
}

func (s *Source) SourceType() string {
	return SourceType
}

func (s *Source) ToConfig() sources.SourceConfig {
	return s.Config
}

func (s *Source) HttpDefaultHeaders() map[string]string {
	return s.DefaultHeaders
}

func (s *Source) HttpBaseURL() string {
	return s.BaseURL
}

func (s *Source) HttpQueryParams() map[string]string {
	return s.QueryParams
}

func (s *Source) Client() *http.Client {
	return s.client
}

func (s *Source) RunRequest(req *http.Request) (any, error) {
	// Make request and fetch response
	resp, err := s.Client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("error making HTTP request: %s", err)
	}
	defer resp.Body.Close()

	var body []byte
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("unexpected status code: %d, response body: %s", resp.StatusCode, string(body))
	}

	var data any
	if err = json.Unmarshal(body, &data); err != nil {
		// if unable to unmarshal data, return result as string.
		return string(body), nil
	}
	return data, nil
}
