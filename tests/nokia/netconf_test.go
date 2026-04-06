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
	"strings"
	"testing"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
)

// TestNETCONFGetConfig verifies NETCONF <get-config> for the running datastore.
func TestNETCONFGetConfig(t *testing.T) {
	for _, dev := range labDevices {
		t.Run(dev.Name, func(t *testing.T) {
			src, err := newNETCONFSource(dev)
			if err != nil {
				t.Fatalf("failed to initialize NETCONF source for %s: %v", dev.Name, err)
			}

			querier, ok := src.(capabilities.NetconfQuerier)
			if !ok {
				t.Fatalf("NETCONF source for %s does not implement NetconfQuerier", dev.Name)
			}

			// Get the full running config (no filter).
			data, err := querier.NetconfGetConfig(context.Background(), "running", "")
			if err != nil {
				t.Fatalf("NETCONF GetConfig failed on %s: %v", dev.Name, err)
			}

			if len(data) == 0 {
				t.Fatal("NETCONF GetConfig returned empty data")
			}

			t.Logf("NETCONF GetConfig from %s: %d bytes", dev.Name, len(data))
		})
	}
}

// TestNETCONFGetState verifies NETCONF <get> for operational state.
func TestNETCONFGetState(t *testing.T) {
	dev := labDevices[0] // leaf1
	src, err := newNETCONFSource(dev)
	if err != nil {
		t.Fatalf("failed to initialize NETCONF source: %v", err)
	}

	querier := src.(capabilities.NetconfQuerier)

	// Get operational state with a subtree filter for interfaces.
	// SR Linux uses the urn:nokia.com:srlinux:chassis:interfaces YANG module namespace.
	filter := `<interface xmlns="urn:nokia.com:srlinux:chassis:interfaces"/>`
	data, err := querier.NetconfGet(context.Background(), filter)
	if err != nil {
		t.Fatalf("NETCONF Get (interface) failed: %v", err)
	}

	if len(data) == 0 {
		t.Fatal("NETCONF Get returned empty data")
	}

	output := string(data)
	if !strings.Contains(output, "interface") {
		t.Errorf("expected 'interface' in NETCONF response, got:\n%s", output[:min(len(output), 500)])
	}

	t.Logf("NETCONF Get state from %s: %d bytes", dev.Name, len(data))
}
