// Copyright 2025 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/go-chi/chi/v5"
)

//go:embed static/* static/css/* static/js/* static/assets/*
var uiFS embed.FS

type uiAuthConfigResponse struct {
	Enabled                       bool     `json:"enabled"`
	AuthService                   string   `json:"authService,omitempty"`
	Issuer                        string   `json:"issuer,omitempty"`
	AuthorizationEndpoint         string   `json:"authorizationEndpoint,omitempty"`
	TokenEndpoint                 string   `json:"tokenEndpoint,omitempty"`
	EndSessionEndpoint            string   `json:"endSessionEndpoint,omitempty"`
	ClientID                      string   `json:"clientId,omitempty"`
	Scopes                        []string `json:"scopes,omitempty"`
	RedirectURI                   string   `json:"redirectUri,omitempty"`
	APIAudience                   string   `json:"apiAudience,omitempty"`
	CodeChallengeMethodsSupported []string `json:"codeChallengeMethodsSupported,omitempty"`
}

func RegisterWebUI(r chi.Router, s *Server) error {
	staticSub, err := fs.Sub(uiFS, "static")
	if err != nil {
		return err
	}

	assetServer := http.FileServer(http.FS(staticSub))

	r.Route("/ui", func(r chi.Router) {
		r.Get("/", serveHTML("index.html"))
		r.Get("/tools", serveHTML("tools.html"))
		r.Get("/toolsets", serveHTML("toolsets.html"))
		r.Get("/auth/callback", serveHTML("auth-callback.html"))
		r.Get("/auth/config", serveUIAuthConfig(s))
		r.Handle("/static/*", http.StripPrefix("/ui/static/", cacheStatic(assetServer)))
	})

	return nil
}

func serveUIAuthConfig(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := uiAuthConfigResponse{Enabled: false}
		if !s.authConfig.UI.Enabled {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		authSvc, ok := s.ResourceMgr.GetAuthServiceMap()[s.authConfig.UI.AuthService]
		if !ok {
			http.Error(w, "ui auth service not found", http.StatusInternalServerError)
			return
		}

		metaProvider, ok := authSvc.(auth.AuthorizationServerMetadataProvider)
		if !ok {
			http.Error(w, "ui auth metadata unavailable", http.StatusInternalServerError)
			return
		}
		meta := metaProvider.AuthorizationServerMetadata()
		redirectPath := s.authConfig.UI.RedirectPath
		if !strings.HasPrefix(redirectPath, "/") {
			redirectPath = "/" + redirectPath
		}

		resp = uiAuthConfigResponse{
			Enabled:                       true,
			AuthService:                   s.authConfig.UI.AuthService,
			Issuer:                        meta.Issuer,
			AuthorizationEndpoint:         meta.AuthorizationEndpoint,
			TokenEndpoint:                 meta.TokenEndpoint,
			EndSessionEndpoint:            meta.EndSessionEndpoint,
			ClientID:                      s.authConfig.UI.ClientID,
			Scopes:                        append([]string(nil), s.authConfig.UI.Scopes...),
			RedirectURI:                   resourceBaseURL(r) + redirectPath,
			APIAudience:                   s.authConfig.EndpointAuth.API.Audience,
			CodeChallengeMethodsSupported: append([]string(nil), meta.CodeChallengeMethodsSupported...),
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func serveHTML(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clean := path.Clean(name)
		if strings.Contains(clean, "..") {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}

		content, err := uiFS.ReadFile(path.Join("static", clean))
		if err != nil {
			http.Error(w, "page not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}
}

func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".css") || strings.HasSuffix(r.URL.Path, ".js") || strings.HasSuffix(r.URL.Path, ".svg") {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
		next.ServeHTTP(w, r)
	})
}
