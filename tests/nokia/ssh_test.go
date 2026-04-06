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

// TestSSHShowVersion verifies that we can connect to each device via SSH
// and run "show version" successfully.
func TestSSHShowVersion(t *testing.T) {
	for _, dev := range labDevices {
		t.Run(dev.Name, func(t *testing.T) {
			src, err := newSSHSource(dev)
			if err != nil {
				t.Fatalf("failed to initialize SSH source for %s: %v", dev.Name, err)
			}

			runner, ok := src.(capabilities.CommandRunner)
			if !ok {
				t.Fatalf("SSH source for %s does not implement CommandRunner", dev.Name)
			}

			output, err := runner.RunCommand(context.Background(), "show version")
			if err != nil {
				t.Fatalf("show version failed on %s: %v", dev.Name, err)
			}

			if output == "" {
				t.Fatal("show version returned empty output")
			}
			t.Logf("show version output from %s:\n%s", dev.Name, output)
		})
	}
}

// TestSSHShowInterfaces verifies SSH connectivity and the "show interface brief"
// command returns interface data including the fabric link.
func TestSSHShowInterfaces(t *testing.T) {
	for _, dev := range labDevices {
		t.Run(dev.Name, func(t *testing.T) {
			src, err := newSSHSource(dev)
			if err != nil {
				t.Fatalf("failed to initialize SSH source for %s: %v", dev.Name, err)
			}

			runner := src.(capabilities.CommandRunner)
			output, err := runner.RunCommand(context.Background(), "show interface brief")
			if err != nil {
				t.Fatalf("show interface brief failed on %s: %v", dev.Name, err)
			}

			// The minimal topology has ethernet-1/1 configured on every device.
			if !strings.Contains(output, "ethernet-1/1") {
				t.Errorf("expected ethernet-1/1 in output from %s, got:\n%s", dev.Name, output)
			}
			t.Logf("show interface brief from %s:\n%s", dev.Name, output)
		})
	}
}

// TestSSHShowNetworkInstance verifies IS-IS configuration is active by
// querying the default network instance.
func TestSSHShowNetworkInstance(t *testing.T) {
	dev := labDevices[0] // leaf1
	src, err := newSSHSource(dev)
	if err != nil {
		t.Fatalf("failed to initialize SSH source for %s: %v", dev.Name, err)
	}

	runner := src.(capabilities.CommandRunner)
	output, err := runner.RunCommand(context.Background(), "show network-instance default protocols isis adjacency")
	if err != nil {
		t.Fatalf("show isis adjacency failed: %v", err)
	}

	t.Logf("IS-IS adjacency from %s:\n%s", dev.Name, output)
}

// TestSSHSourceIdentity verifies the SourceIdentity capability reports
// the configured vendor and platform.
func TestSSHSourceIdentity(t *testing.T) {
	dev := labDevices[0]
	src, err := newSSHSource(dev)
	if err != nil {
		t.Fatalf("failed to initialize SSH source: %v", err)
	}

	identity, ok := src.(capabilities.SourceIdentity)
	if !ok {
		t.Fatal("SSH source does not implement SourceIdentity")
	}

	if got := identity.DeviceVendor(); got != "nokia" {
		t.Errorf("DeviceVendor() = %q, want %q", got, "nokia")
	}
	if got := identity.DevicePlatform(); got != "srlinux" {
		t.Errorf("DevicePlatform() = %q, want %q", got, "srlinux")
	}
}
