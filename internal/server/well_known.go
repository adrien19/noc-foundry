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

package server

import (
	"fmt"
	"net/http"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/go-chi/render"
)

// protectedResourceMetadataHandler serves the OAuth 2.0 Protected Resource
// Metadata document (RFC 9728) at /.well-known/oauth-protected-resource.
//
// MCP clients (VS Code, agent SDKs) fetch this endpoint after receiving a 401
// to discover which OIDC authorization server(s) can issue tokens for this
// nocfoundry instance. The client then fetches the issuer's own
// /.well-known/openid-configuration to find the token and authorization
// endpoints and completes the OAuth flow autonomously.
//
// The response includes only OIDC-backed auth services; Google and other
// non-OIDC types are silently skipped (they do not implement auth.IssuerProvider).
func protectedResourceMetadataHandler(s *Server, surface auth.EndpointSurface, w http.ResponseWriter, r *http.Request) {
	// RFC 9728 §7.3: the "resource" value MUST match the resource indicator used
	// by the client to discover this document.  VS Code and MCP SDKs connect to
	// the /mcp endpoint, so we use that URL as the resource identifier.
	resource := protectedResourceURL(r, surface)

	policy, _ := s.authConfig.EndpointPolicy(surface)
	var authServers []string
	for _, name := range policy.AuthServices {
		svc, ok := s.ResourceMgr.GetAuthServiceMap()[name]
		if !ok {
			continue
		}
		if ip, ok := svc.(auth.IssuerProvider); ok {
			authServers = append(authServers, ip.OIDCIssuerURL())
		}
	}

	meta := map[string]any{
		"resource":                 resource,
		"bearer_methods_supported": []string{"header"},
	}
	// Omit authorization_servers when no OIDC services are configured.
	// RFC 9728 §2: "Parameters with zero values MUST be omitted from the response."
	if len(authServers) > 0 {
		meta["authorization_servers"] = authServers
	}

	render.JSON(w, r, meta)
}

// resourceBaseURL derives the canonical base URL of this server from the
// incoming request, honouring X-Forwarded-Proto for TLS termination proxies.
func resourceBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	// Honour reverse-proxy TLS termination.
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// resourceMetadataURL returns the full URL of the protected resource metadata
// document for use in WWW-Authenticate response headers (RFC 9728 §5.1).
func resourceMetadataURL(r *http.Request, surface auth.EndpointSurface) string {
	return resourceBaseURL(r) + "/.well-known/oauth-protected-resource/" + string(surface)
}

func protectedResourceURL(r *http.Request, surface auth.EndpointSurface) string {
	return resourceBaseURL(r) + "/" + string(surface)
}
