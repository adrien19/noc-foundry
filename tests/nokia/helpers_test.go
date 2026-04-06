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
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/adrien19/noc-foundry/internal/sources"
	"github.com/adrien19/noc-foundry/internal/sources/gnmi"
	"github.com/adrien19/noc-foundry/internal/sources/netconf"
	"github.com/adrien19/noc-foundry/internal/sources/ssh"
	"github.com/adrien19/noc-foundry/internal/util/parameters"
	"go.opentelemetry.io/otel/trace/noop"
)

const (
	deviceReadyTimeout = 90 * time.Second

	// Lab credentials — matches SR Linux default admin user.
	labUsername = "admin"
	labPassword = "NokiaSrl1!"
)

// labDevice describes a device in the integration test topology.
type labDevice struct {
	Name        string
	MgmtIP      string
	SSHPort     int
	GNMIPort    int
	NETCONFPort int
	Vendor      string
	Platform    string
}

// labDevices defines the devices deployed by the minimal topology.
var labDevices = []labDevice{
	{Name: "leaf1", MgmtIP: "172.31.251.11", SSHPort: 22, GNMIPort: 57400, NETCONFPort: 830, Vendor: "nokia", Platform: "srlinux"},
	{Name: "spine1", MgmtIP: "172.31.251.12", SSHPort: 22, GNMIPort: 57400, NETCONFPort: 830, Vendor: "nokia", Platform: "srlinux"},
}

// waitForTCP polls a TCP endpoint until it accepts connections or the timeout elapses.
func waitForTCP(host string, port int, timeout time.Duration) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(3 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %s after %s", addr, timeout)
}

// newSSHSource creates and initializes an SSH source for the given device.
func newSSHSource(dev labDevice) (sources.Source, error) {
	cfg := ssh.Config{
		Name:     dev.Name + "/ssh",
		Type:     "ssh",
		Host:     dev.MgmtIP,
		Port:     dev.SSHPort,
		Username: labUsername,
		Password: labPassword,
		Timeout:  "30s",
		Vendor:   dev.Vendor,
		Platform: dev.Platform,
	}
	return cfg.Initialize(context.Background(), noop.NewTracerProvider().Tracer("test"))
}

// newGNMISource creates and initializes a gNMI source for the given device.
func newGNMISource(dev labDevice) (sources.Source, error) {
	cfg := gnmi.Config{
		Name:        dev.Name + "/gnmi",
		Type:        "gnmi",
		Host:        dev.MgmtIP,
		Port:        dev.GNMIPort,
		Username:    labUsername,
		Password:    labPassword,
		Timeout:     "30s",
		Vendor:      dev.Vendor,
		Platform:    dev.Platform,
		TLSInsecure: true,
		NativeYang:  true,
	}
	return cfg.Initialize(context.Background(), noop.NewTracerProvider().Tracer("test"))
}

// newNETCONFSource creates and initializes a NETCONF source for the given device.
func newNETCONFSource(dev labDevice) (sources.Source, error) {
	cfg := netconf.Config{
		Name:     dev.Name + "/netconf",
		Type:     "netconf",
		Host:     dev.MgmtIP,
		Port:     dev.NETCONFPort,
		Username: labUsername,
		Password: labPassword,
		Timeout:  "30s",
		Vendor:   dev.Vendor,
		Platform: dev.Platform,
	}
	return cfg.Initialize(context.Background(), noop.NewTracerProvider().Tracer("test"))
}

// staticSourceProvider wraps a sources map to implement tools.SourceProvider.
type staticSourceProvider struct {
	sources map[string]sources.Source
}

func (p *staticSourceProvider) GetSource(name string) (sources.Source, bool) {
	s, ok := p.sources[name]
	return s, ok
}

func (p *staticSourceProvider) GetSourcesByLabels(_ context.Context, _ map[string]string) (map[string]sources.Source, error) {
	return nil, nil
}

func (p *staticSourceProvider) GetDevicePoolLabels() map[string]map[string]string {
	return nil
}

// makeParams creates ParamValues from key-value pairs.
func makeParams(kvs ...string) parameters.ParamValues {
	if len(kvs)%2 != 0 {
		panic("makeParams requires an even number of arguments (key, value pairs)")
	}
	var pv parameters.ParamValues
	for i := 0; i < len(kvs); i += 2 {
		pv = append(pv, parameters.ParamValue{Name: kvs[i], Value: kvs[i+1]})
	}
	return pv
}
