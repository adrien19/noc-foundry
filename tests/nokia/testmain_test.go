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

// Package nokia contains integration tests that run against a Containerlab
// topology with Nokia SR Linux devices. The tests exercise real SSH, gNMI,
// and NETCONF connections.
//
// Run with: go test -tags integration -race -v -timeout 10m ./tests/nokia/
package nokia

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	// defaultTopoFile is the containerlab topology relative to this package.
	defaultTopoFile = "topologies/minimal.clab.yaml"
)

// topoFilePath returns the absolute path to the topology file used by
// containerlab deploy/destroy commands.
func topoFilePath() string {
	if override := os.Getenv("NOC_FOUNDRY_TOPO_FILE"); override != "" {
		return override
	}
	// Resolve relative to the test file's directory.
	abs, err := filepath.Abs(defaultTopoFile)
	if err != nil {
		log.Fatalf("unable to resolve topology file path: %v", err)
	}
	return abs
}

// TestMain manages the containerlab lifecycle around the test suite.
//
//   - If containerlab is not installed, the suite is skipped.
//   - Deploy is called before any tests run.
//   - Destroy + cleanup is called after all tests complete.
//   - Device readiness is verified before yielding to tests.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("containerlab"); err != nil {
		fmt.Println("SKIP: containerlab not installed, skipping Nokia integration tests")
		os.Exit(0)
	}

	topo := topoFilePath()
	if _, err := os.Stat(topo); err != nil {
		log.Fatalf("topology file not found: %s", topo)
	}

	// Deploy the lab.
	log.Printf("Deploying containerlab topology: %s", topo)
	deploy := exec.Command("containerlab", "deploy", "-t", topo, "--reconfigure")
	deploy.Stdout = os.Stdout
	deploy.Stderr = os.Stderr
	if err := deploy.Run(); err != nil {
		log.Printf("containerlab deploy failed: %v", err)
		log.Printf("HINT: ensure your user is in the clab_admins group:")
		log.Printf("  sudo usermod -aG clab_admins $(whoami) && newgrp clab_admins")
		os.Exit(1)
	}

	// Wait for all devices to become ready.
	log.Println("Waiting for devices to become reachable...")
	for _, dev := range labDevices {
		if err := waitForTCP(dev.MgmtIP, dev.SSHPort, deviceReadyTimeout); err != nil {
			destroy(topo)
			log.Fatalf("device %s not reachable on SSH: %v", dev.Name, err)
		}
		if err := waitForTCP(dev.MgmtIP, dev.GNMIPort, deviceReadyTimeout); err != nil {
			destroy(topo)
			log.Fatalf("device %s not reachable on gNMI: %v", dev.Name, err)
		}
		if err := waitForTCP(dev.MgmtIP, dev.NETCONFPort, deviceReadyTimeout); err != nil {
			destroy(topo)
			log.Fatalf("device %s not reachable on NETCONF: %v", dev.Name, err)
		}
		log.Printf("Device %s is ready (SSH:%d, gNMI:%d, NETCONF:%d)",
			dev.Name, dev.SSHPort, dev.GNMIPort, dev.NETCONFPort)
	}

	code := m.Run()

	destroy(topo)
	os.Exit(code)
}

// destroy tears down the containerlab topology.
func destroy(topo string) {
	log.Printf("Destroying containerlab topology: %s", topo)
	cmd := exec.Command("containerlab", "destroy", "-t", topo, "--cleanup")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("WARNING: containerlab destroy failed: %v", err)
	}
}
