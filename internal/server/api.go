// Copyright 2024 Google LLC
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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"

	"github.com/adrien19/noc-foundry/internal/auth"
	"github.com/adrien19/noc-foundry/internal/tools"
	"github.com/adrien19/noc-foundry/internal/util"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// apiRouter creates a router that represents the routes under /api
func apiRouter(s *Server) (chi.Router, error) {
	r := chi.NewRouter()

	r.Use(middleware.AllowContentType("application/json"))
	r.Use(middleware.StripSlashes)
	r.Use(render.SetContentType(render.ContentTypeJSON))
	r.Use(endpointAuthMiddleware(s, auth.EndpointSurfaceAPI))

	r.Get("/toolset", func(w http.ResponseWriter, r *http.Request) { toolsetHandler(s, w, r) })
	r.Get("/toolset/{toolsetName}", func(w http.ResponseWriter, r *http.Request) { toolsetHandler(s, w, r) })
	r.Get("/tools", func(w http.ResponseWriter, r *http.Request) { toolsListHandler(s, w, r) })
	r.Get("/toolsets", func(w http.ResponseWriter, r *http.Request) { toolsetsListHandler(s, w, r) })
	r.Get("/prompts", func(w http.ResponseWriter, r *http.Request) { promptsListHandler(s, w, r) })
	r.Get("/promptsets", func(w http.ResponseWriter, r *http.Request) { promptsetsListHandler(s, w, r) })
	r.Get("/promptset/{promptsetName}", func(w http.ResponseWriter, r *http.Request) { promptsetHandler(s, w, r) })
	r.Get("/authservices", func(w http.ResponseWriter, r *http.Request) { authServicesHandler(s, w, r) })

	r.Route("/tool/{toolName}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, r *http.Request) { toolGetHandler(s, w, r) })
		r.Post("/invoke", func(w http.ResponseWriter, r *http.Request) { toolInvokeHandler(s, w, r) })
	})

	return r, nil
}

type toolListItem struct {
	Name         string                         `json:"name"`
	Description  string                         `json:"description"`
	Parameters   []parameters.ParameterManifest `json:"parameters"`
	AuthRequired []string                       `json:"authRequired"`
}

type toolsetListItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	ToolCount   int    `json:"toolCount"`
}

type promptListItem struct {
	Name          string                         `json:"name"`
	Description   string                         `json:"description"`
	ArgumentCount int                            `json:"argumentCount"`
	Arguments     []parameters.ParameterManifest `json:"arguments"`
}

type toolsetDetailResponse struct {
	tools.ToolsetManifest
	Promptset string `json:"promptset,omitempty"`
}

type promptsetListItem struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	PromptCount int    `json:"promptCount"`
}

// authServicesHandler returns a map of auth service name → auth service type.
// The UI uses this to render the appropriate authentication widget per service.
func authServicesHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	result := make(map[string]string)
	for name, svc := range s.ResourceMgr.GetAuthServiceMap() {
		result[name] = svc.AuthServiceType()
	}
	render.JSON(w, r, result)
}

// toolsetHandler handles the request for information about a Toolset.
func toolsetHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.Tracer.Start(r.Context(), "nocfoundry/server/toolset/get")
	r = r.WithContext(ctx)

	toolsetName := chi.URLParam(r, "toolsetName")
	s.logger.DebugContext(ctx, fmt.Sprintf("toolset name: %s", toolsetName))
	span.SetAttributes(attribute.String("toolset.name", toolsetName))
	var err error
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	toolset, ok := s.ResourceMgr.GetToolset(toolsetName)
	if !ok {
		err = fmt.Errorf("toolset %q does not exist", toolsetName)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusNotFound))
		return
	}
	render.JSON(w, r, toolsetDetailResponse{
		ToolsetManifest: toolset.Manifest,
		Promptset:       toolset.ToConfig().Promptset,
	})
}

func toolsListHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	toolsMap := s.ResourceMgr.GetToolsMap()
	toolNames := make([]string, 0, len(toolsMap))
	for name := range toolsMap {
		toolNames = append(toolNames, name)
	}
	sort.Strings(toolNames)

	result := make([]toolListItem, 0, len(toolNames))
	for _, name := range toolNames {
		manifest := toolsMap[name].Manifest()
		result = append(result, toolListItem{
			Name:         name,
			Description:  manifest.Description,
			Parameters:   manifest.Parameters,
			AuthRequired: manifest.AuthRequired,
		})
	}

	render.JSON(w, r, result)
}

func toolsetsListHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	toolsetsMap := s.ResourceMgr.GetToolsetsMap()
	toolsetNames := make([]string, 0, len(toolsetsMap))
	for name := range toolsetsMap {
		toolsetNames = append(toolsetNames, name)
	}
	sort.Strings(toolsetNames)

	result := make([]toolsetListItem, 0, len(toolsetNames))
	for _, name := range toolsetNames {
		displayName := name
		if name == "" {
			displayName = "default"
		}
		result = append(result, toolsetListItem{
			Name:        name,
			DisplayName: displayName,
			ToolCount:   len(toolsetsMap[name].Manifest.ToolsManifest),
		})
	}

	render.JSON(w, r, result)
}

func promptsListHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	promptsMap := s.ResourceMgr.GetPromptsMap()
	promptNames := make([]string, 0, len(promptsMap))
	for name := range promptsMap {
		promptNames = append(promptNames, name)
	}
	sort.Strings(promptNames)

	result := make([]promptListItem, 0, len(promptNames))
	for _, name := range promptNames {
		manifest := promptsMap[name].Manifest()
		result = append(result, promptListItem{
			Name:          name,
			Description:   manifest.Description,
			ArgumentCount: len(manifest.Arguments),
			Arguments:     manifest.Arguments,
		})
	}

	render.JSON(w, r, result)
}

func promptsetsListHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	promptsetsMap := s.ResourceMgr.GetPromptsetsMap()
	promptsetNames := make([]string, 0, len(promptsetsMap))
	for name := range promptsetsMap {
		promptsetNames = append(promptsetNames, name)
	}
	sort.Strings(promptsetNames)

	result := make([]promptsetListItem, 0, len(promptsetNames))
	for _, name := range promptsetNames {
		displayName := name
		if name == "" {
			displayName = "default"
		}
		result = append(result, promptsetListItem{
			Name:        name,
			DisplayName: displayName,
			PromptCount: len(promptsetsMap[name].Manifest.PromptsManifest),
		})
	}

	render.JSON(w, r, result)
}

func promptsetHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.Tracer.Start(r.Context(), "nocfoundry/server/promptset/get")
	r = r.WithContext(ctx)

	promptsetName := chi.URLParam(r, "promptsetName")
	s.logger.DebugContext(ctx, fmt.Sprintf("promptset name: %s", promptsetName))
	span.SetAttributes(attribute.String("promptset.name", promptsetName))
	var err error
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	promptset, ok := s.ResourceMgr.GetPromptset(promptsetName)
	if !ok {
		err = fmt.Errorf("promptset %q does not exist", promptsetName)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusNotFound))
		return
	}
	render.JSON(w, r, promptset.Manifest)
}

// toolGetHandler handles requests for a single Tool.
func toolGetHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.Tracer.Start(r.Context(), "nocfoundry/server/tool/get")
	r = r.WithContext(ctx)

	toolName := chi.URLParam(r, "toolName")
	s.logger.DebugContext(ctx, fmt.Sprintf("tool name: %s", toolName))
	span.SetAttributes(attribute.String("tool_name", toolName))
	var err error
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	tool, ok := s.ResourceMgr.GetTool(toolName)
	if !ok {
		err = fmt.Errorf("invalid tool name: tool with name %q does not exist", toolName)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusNotFound))
		return
	}
	// TODO: this can be optimized later with some caching
	m := tools.ToolsetManifest{
		ServerVersion: s.version,
		ToolsManifest: map[string]tools.Manifest{
			toolName: tool.Manifest(),
		},
	}

	render.JSON(w, r, m)
}

// toolInvokeHandler handles the API request to invoke a specific Tool.
func toolInvokeHandler(s *Server, w http.ResponseWriter, r *http.Request) {
	ctx, span := s.instrumentation.Tracer.Start(r.Context(), "nocfoundry/server/tool/invoke")
	r = r.WithContext(ctx)
	ctx = util.WithLogger(r.Context(), s.logger)

	toolName := chi.URLParam(r, "toolName")
	s.logger.DebugContext(ctx, fmt.Sprintf("tool name: %s", toolName))
	span.SetAttributes(attribute.String("tool_name", toolName))
	var err error
	defer func() {
		if err != nil {
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

	tool, ok := s.ResourceMgr.GetTool(toolName)
	if !ok {
		err = fmt.Errorf("invalid tool name: tool with name %q does not exist", toolName)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusNotFound))
		return
	}

	// Extract OAuth access token from the "Authorization" header (currently for
	// BigQuery end-user credentials usage only)
	accessToken := tools.AccessToken(r.Header.Get("Authorization"))

	// Check if this specific tool requires the standard authorization header
	clientAuth, err := tool.RequiresClientAuthorization(s.ResourceMgr)
	if err != nil {
		errMsg := fmt.Errorf("error during invocation: %w", err)
		s.logger.DebugContext(ctx, errMsg.Error())
		_ = render.Render(w, r, newErrResponse(errMsg, http.StatusNotFound))
		return
	}
	if clientAuth {
		if accessToken == "" {
			err = fmt.Errorf("tool requires client authorization but access token is missing from the request header")
			s.logger.DebugContext(ctx, err.Error())
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL(r, auth.EndpointSurfaceAPI)+`"`)
			_ = render.Render(w, r, newErrResponse(err, http.StatusUnauthorized))
			return
		}
	}

	// Tool authentication
	// claimsFromAuth maps the name of the authservice to the claims retrieved from it.
	claimsFromAuth := make(map[string]map[string]any)
	for _, aS := range s.ResourceMgr.GetAuthServiceMap() {
		claims, err := aS.GetClaimsFromHeader(ctx, r.Header)
		if err != nil {
			s.logger.DebugContext(ctx, err.Error())
			continue
		}
		if claims == nil {
			// authService not present in header
			continue
		}
		claimsFromAuth[aS.GetName()] = claims
	}

	// Tool authorization check
	verifiedAuthServices := make([]string, len(claimsFromAuth))
	i := 0
	for k := range claimsFromAuth {
		verifiedAuthServices[i] = k
		i++
	}

	// Check if any of the specified auth services is verified
	isAuthorized := tool.Authorized(verifiedAuthServices)
	if !isAuthorized {
		err = fmt.Errorf("tool invocation not authorized. Please make sure you specify correct auth headers")
		s.logger.DebugContext(ctx, err.Error())
		w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL(r, auth.EndpointSurfaceAPI)+`"`)
		_ = render.Render(w, r, newErrResponse(err, http.StatusUnauthorized))
		return
	}
	s.logger.DebugContext(ctx, "tool invocation authorized")

	var data map[string]any
	if err = util.DecodeJSON(r.Body, &data); err != nil {
		render.Status(r, http.StatusBadRequest)
		err = fmt.Errorf("request body was invalid JSON: %w", err)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusBadRequest))
		return
	}

	params, err := parameters.ParseParams(tool.GetParameters(), data, claimsFromAuth)
	if err != nil {
		var clientServerErr *util.ClientServerError

		// Return 401 Authentication errors
		if errors.As(err, &clientServerErr) && clientServerErr.Code == http.StatusUnauthorized {
			s.logger.DebugContext(ctx, fmt.Sprintf("auth error: %v", err))
			w.Header().Set("WWW-Authenticate", `Bearer resource_metadata="`+resourceMetadataURL(r, auth.EndpointSurfaceAPI)+`"`)
			_ = render.Render(w, r, newErrResponse(err, http.StatusUnauthorized))
			return
		}

		var agentErr *util.AgentError
		if errors.As(err, &agentErr) {
			s.logger.DebugContext(ctx, fmt.Sprintf("agent validation error: %v", err))
			errMap := map[string]string{"error": err.Error()}
			errMarshal, _ := json.Marshal(errMap)

			_ = render.Render(w, r, &resultResponse{Result: string(errMarshal)})
			return
		}

		// Return 500 if it's a specific ClientServerError that isn't a 401, or any other unexpected error
		s.logger.ErrorContext(ctx, fmt.Sprintf("internal server error: %v", err))
		_ = render.Render(w, r, newErrResponse(err, http.StatusInternalServerError))
		return
	}
	s.logger.DebugContext(ctx, fmt.Sprintf("invocation params: %s", params))

	params, err = tool.EmbedParams(ctx, params, s.ResourceMgr.GetEmbeddingModelMap())
	if err != nil {
		err = fmt.Errorf("error embedding parameters: %w", err)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusBadRequest))
		return
	}

	res, err := tool.Invoke(ctx, s.ResourceMgr, params, accessToken)

	// Determine what error to return to the users.
	if err != nil {
		var tbErr util.NOCFoundryError

		if errors.As(err, &tbErr) {
			switch tbErr.Category() {
			case util.CategoryAgent:
				// Agent Errors -> 200 OK
				// Avoid logging full error to prevent leaking sensitive details from source configs.
				s.logger.DebugContext(ctx, "Tool invocation agent error")
				res = map[string]string{
					"error": err.Error(),
				}

			case util.CategoryServer:
				// Server Errors -> Check the specific code inside
				var clientServerErr *util.ClientServerError
				statusCode := http.StatusInternalServerError // Default to 500

				if errors.As(err, &clientServerErr) {
					if clientServerErr.Code != 0 {
						statusCode = clientServerErr.Code
					}
				}

				// Process auth error
				if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
					if clientAuth {
						// Token error, pass through 401/403. Avoid logging full error to prevent leaking sensitive details.
						s.logger.DebugContext(ctx, "Client credentials lack authorization")
						_ = render.Render(w, r, newErrResponse(err, statusCode))
						return
					}
					// ADC/Config error, return 500
					statusCode = http.StatusInternalServerError
				}

				// Avoid logging full error to prevent leaking sensitive details from source configs.
				s.logger.ErrorContext(ctx, "Tool invocation server error")
				_ = render.Render(w, r, newErrResponse(err, statusCode))
				return
			}
		} else {
			// Unknown error -> 500
			// Avoid logging full error details here, as they may contain sensitive data from underlying sources.
			s.logger.ErrorContext(ctx, "Tool invocation unknown error (details omitted to protect sensitive data)")
			_ = render.Render(w, r, newErrResponse(err, http.StatusInternalServerError))
			return
		}
	}

	resMarshal, err := json.Marshal(res)
	if err != nil {
		err = fmt.Errorf("unable to marshal result: %w", err)
		s.logger.DebugContext(ctx, err.Error())
		_ = render.Render(w, r, newErrResponse(err, http.StatusInternalServerError))
		return
	}

	_ = render.Render(w, r, &resultResponse{Result: string(resMarshal)})
}

var _ render.Renderer = &resultResponse{} // Renderer interface for managing response payloads.

// resultResponse is the response sent back when the tool was invocated successfully.
type resultResponse struct {
	Result string `json:"result"` // result of tool invocation
}

// Render renders a single payload and respond to the client request.
func (rr resultResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, http.StatusOK)
	return nil
}

var _ render.Renderer = &errResponse{} // Renderer interface for managing response payloads.

// newErrResponse is a helper function initializing an ErrResponse
func newErrResponse(err error, code int) *errResponse {
	return &errResponse{
		Err:            err,
		HTTPStatusCode: code,

		StatusText: http.StatusText(code),
		ErrorText:  err.Error(),
	}
}

// errResponse is the response sent back when an error has been encountered.
type errResponse struct {
	Err            error `json:"-"` // low-level runtime error
	HTTPStatusCode int   `json:"-"` // http response status code

	StatusText string `json:"status"`          // user-level status message
	ErrorText  string `json:"error,omitempty"` // application-level error message, for debugging
}

func (e *errResponse) Render(w http.ResponseWriter, r *http.Request) error {
	render.Status(r, e.HTTPStatusCode)
	return nil
}
