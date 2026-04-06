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

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
)

// TestGNMIGetSystemName verifies gNMI Get against the system hostname path.
func TestGNMIGetSystemName(t *testing.T) {
	for _, dev := range labDevices {
		t.Run(dev.Name, func(t *testing.T) {
			src, err := newGNMISource(dev)
			if err != nil {
				t.Fatalf("failed to initialize gNMI source for %s: %v", dev.Name, err)
			}

			querier, ok := src.(capabilities.GnmiQuerier)
			if !ok {
				t.Fatalf("gNMI source for %s does not implement GnmiQuerier", dev.Name)
			}

			result, err := querier.GnmiGet(context.Background(), []string{"/system/name"}, "JSON_IETF")
			if err != nil {
				t.Fatalf("gNMI Get /system/name failed on %s: %v", dev.Name, err)
			}

			if len(result.Notifications) == 0 {
				t.Fatal("gNMI Get returned no notifications")
			}

			t.Logf("gNMI /system/name from %s: %s", dev.Name, string(result.Notifications[0].Value))
		})
	}
}

// TestGNMIGetInterfaces verifies gNMI Get for the interface tree.
func TestGNMIGetInterfaces(t *testing.T) {
	dev := labDevices[0] // leaf1
	src, err := newGNMISource(dev)
	if err != nil {
		t.Fatalf("failed to initialize gNMI source for %s: %v", dev.Name, err)
	}

	querier := src.(capabilities.GnmiQuerier)
	result, err := querier.GnmiGet(context.Background(), []string{"/interface[name=ethernet-1/1]"}, "JSON_IETF")
	if err != nil {
		t.Fatalf("gNMI Get /interface[name=ethernet-1/1] failed: %v", err)
	}

	if len(result.Notifications) == 0 {
		t.Fatal("gNMI Get returned no notifications for ethernet-1/1")
	}

	for _, n := range result.Notifications {
		t.Logf("gNMI interface path=%s value=%s", n.Path, string(n.Value))
	}
}

// TestGNMIGetNetworkInstance verifies gNMI Get for the default network instance.
func TestGNMIGetNetworkInstance(t *testing.T) {
	dev := labDevices[0]
	src, err := newGNMISource(dev)
	if err != nil {
		t.Fatalf("failed to initialize gNMI source: %v", err)
	}

	querier := src.(capabilities.GnmiQuerier)
	result, err := querier.GnmiGet(context.Background(), []string{"/network-instance[name=default]/protocols/isis"}, "JSON_IETF")
	if err != nil {
		t.Fatalf("gNMI Get ISIS failed: %v", err)
	}

	if len(result.Notifications) == 0 {
		t.Fatal("gNMI Get returned no ISIS data")
	}

	t.Logf("gNMI ISIS data from %s: %d notifications", dev.Name, len(result.Notifications))
}
