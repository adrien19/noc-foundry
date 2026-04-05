// Copyright 2026 Google LLC
// Modifications Copyright 2026 Adrien Ndikumana
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
package util

import (
	"fmt"
	"net/http"
	"strings"
)

type ErrorCategory string

const (
	CategoryAgent  ErrorCategory = "AGENT_ERROR"
	CategoryServer ErrorCategory = "SERVER_ERROR"
)

// NOCFoundryError is the interface all custom errors must satisfy
type NOCFoundryError interface {
	error
	Category() ErrorCategory
	Error() string
	Unwrap() error
}

// Agent Errors return 200 to the sender
type AgentError struct {
	Msg   string
	Cause error
}

var _ NOCFoundryError = &AgentError{}

func (e *AgentError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Cause)
	}
	return e.Msg
}

func (e *AgentError) Category() ErrorCategory { return CategoryAgent }

func (e *AgentError) Unwrap() error { return e.Cause }

func NewAgentError(msg string, cause error) *AgentError {
	return &AgentError{Msg: msg, Cause: cause}
}

var _ NOCFoundryError = &AgentError{}

// ClientServerError returns 4XX/5XX error code
type ClientServerError struct {
	Msg   string
	Code  int
	Cause error
}

var _ NOCFoundryError = &ClientServerError{}

func (e *ClientServerError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Cause)
	}
	return e.Msg
}

func (e *ClientServerError) Category() ErrorCategory { return CategoryServer }

func (e *ClientServerError) Unwrap() error { return e.Cause }

func NewClientServerError(msg string, code int, cause error) *ClientServerError {
	return &ClientServerError{Msg: msg, Code: code, Cause: cause}
}

// ProcessGeneralError handles generic errors by inspecting the error string
// for common status code patterns.
func ProcessGeneralError(err error) NOCFoundryError {
	if err == nil {
		return nil
	}

	errStr := err.Error()

	// Check for Unauthorized
	if strings.Contains(errStr, "Error 401") || strings.Contains(errStr, "status 401") {
		return NewClientServerError(
			"failed to access resource",
			http.StatusUnauthorized,
			err,
		)
	}

	// Check for Forbidden
	if strings.Contains(errStr, "Error 403") || strings.Contains(errStr, "status 403") {
		return NewClientServerError(
			"failed to access resource",
			http.StatusForbidden,
			err,
		)
	}

	// Default to AgentError for logical failures (task execution failed)
	return NewAgentError("error processing request", err)
}
