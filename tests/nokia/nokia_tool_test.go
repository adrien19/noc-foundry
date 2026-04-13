// Copyright 2026 Adrien Ndikumana and NOCFoundry Contributors
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

//go:build integration

package nokia

import (
	"context"
	"testing"

	"github.com/adrien19/noc-foundry/internal/sources"
	networkshow "github.com/adrien19/noc-foundry/internal/tools/network/show"
)

// TestNetworkShowToolAdHoc exercises the network-show tool in ad-hoc mode,
// where the command is supplied at invocation time.
func TestNetworkShowToolAdHoc(t *testing.T) {
	dev := labDevices[0] // leaf1

	sshSrc, err := newSSHSource(dev)
	if err != nil {
		t.Fatalf("failed to create SSH source: %v", err)
	}

	sourceName := dev.Name + "/ssh"
	srcMap := map[string]sources.Source{sourceName: sshSrc}

	cfg := networkshow.Config{
		Name:   "test_show_adhoc",
		Type:   "network-show",
		Source: sourceName,
	}

	tool, err := cfg.Initialize(srcMap)
	if err != nil {
		t.Fatalf("tool initialize failed: %v", err)
	}

	provider := &staticSourceProvider{sources: srcMap}
	params := makeParams("command", "show version")

	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("tool invoke failed: %v", toolErr)
	}

	if result == nil {
		t.Fatal("tool invoke returned nil result")
	}

	t.Logf("network-show ad-hoc result: %v", result)
}

// TestNetworkShowToolPredefined exercises the network-show tool in
// predefined-command mode with a parameterized command template.
func TestNetworkShowToolPredefined(t *testing.T) {
	dev := labDevices[0]

	sshSrc, err := newSSHSource(dev)
	if err != nil {
		t.Fatalf("failed to create SSH source: %v", err)
	}

	sourceName := dev.Name + "/ssh"
	srcMap := map[string]sources.Source{sourceName: sshSrc}

	cfg := networkshow.Config{
		Name:        "test_show_interface_detail",
		Type:        "network-show",
		Source:      sourceName,
		Description: "Show interface detail",
		Command:     "show interface {interface}",
		ExtraParams: []networkshow.CommandParam{
			{Name: "interface", Type: "string", Description: "Interface name"},
		},
	}

	tool, err := cfg.Initialize(srcMap)
	if err != nil {
		t.Fatalf("tool initialize failed: %v", err)
	}

	provider := &staticSourceProvider{sources: srcMap}
	params := makeParams("interface", "ethernet-1/1")

	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("tool invoke failed: %v", toolErr)
	}

	if result == nil {
		t.Fatal("tool invoke returned nil result")
	}

	t.Logf("network-show predefined result: %v", result)
}

// TestNetworkShowToolWithJQ exercises ad-hoc mode with a runtime jq transform.
func TestNetworkShowToolWithJQ(t *testing.T) {
	dev := labDevices[0]

	sshSrc, err := newSSHSource(dev)
	if err != nil {
		t.Fatalf("failed to create SSH source: %v", err)
	}

	sourceName := dev.Name + "/ssh"
	srcMap := map[string]sources.Source{sourceName: sshSrc}

	cfg := networkshow.Config{
		Name:   "test_show_jq",
		Type:   "network-show",
		Source: sourceName,
	}

	tool, err := cfg.Initialize(srcMap)
	if err != nil {
		t.Fatalf("tool initialize failed: %v", err)
	}

	provider := &staticSourceProvider{sources: srcMap}
	// Use jq to extract the text field from a plain-text command.
	params := makeParams("command", "show version", "jq", ".text")

	result, toolErr := tool.Invoke(context.Background(), provider, params, "")
	if toolErr != nil {
		t.Fatalf("tool invoke with jq failed: %v", toolErr)
	}

	if result == nil {
		t.Fatal("tool invoke with jq returned nil result")
	}

	t.Logf("network-show with jq result: %v", result)
}
