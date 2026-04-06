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

package gnmi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/adrien19/noc-foundry/internal/network/capabilities"
	pb "github.com/openconfig/gnmi/proto/gnmi"
	"google.golang.org/grpc"
)

func TestSourceType(t *testing.T) {
	s := &Source{Config: Config{Name: "test"}}
	if got := s.SourceType(); got != "gnmi" {
		t.Errorf("SourceType() = %q, want %q", got, "gnmi")
	}
}

func TestDeviceVendor(t *testing.T) {
	s := &Source{Config: Config{Vendor: "nokia"}}
	if got := s.DeviceVendor(); got != "nokia" {
		t.Errorf("DeviceVendor() = %q, want %q", got, "nokia")
	}
}

func TestDevicePlatform(t *testing.T) {
	s := &Source{Config: Config{Platform: "srlinux"}}
	if got := s.DevicePlatform(); got != "srlinux" {
		t.Errorf("DevicePlatform() = %q, want %q", got, "srlinux")
	}
}

func TestCapabilities_OpenConfig(t *testing.T) {
	s := &Source{Config: Config{OpenConfig: true, NativeYang: false}}
	caps := s.Capabilities()
	want := capabilities.SourceCapabilities{
		GnmiSnapshot:    true,
		OpenConfigPaths: true,
		NativeYang:      false,
		CLI:             false,
	}
	if caps != want {
		t.Errorf("Capabilities() = %+v, want %+v", caps, want)
	}
}

func TestCapabilities_NativeYang(t *testing.T) {
	s := &Source{Config: Config{OpenConfig: false, NativeYang: true}}
	caps := s.Capabilities()
	if !caps.NativeYang {
		t.Error("expected NativeYang = true")
	}
	if caps.OpenConfigPaths {
		t.Error("expected OpenConfigPaths = false")
	}
}

func TestCapabilities_Both(t *testing.T) {
	s := &Source{Config: Config{OpenConfig: true, NativeYang: true}}
	caps := s.Capabilities()
	if !caps.GnmiSnapshot || !caps.OpenConfigPaths || !caps.NativeYang {
		t.Errorf("expected all gNMI caps true, got %+v", caps)
	}
	if caps.CLI {
		t.Error("gNMI source should not have CLI capability")
	}
}

func TestParsePath(t *testing.T) {
	tcs := []struct {
		input    string
		wantLen  int
		wantName string
	}{
		{"/interfaces/interface", 2, "interface"},
		{"/openconfig-interfaces:interfaces/interface", 2, "interface"},
		{"/interfaces/interface[name=eth0]/state", 3, "state"},
		{"", 0, ""},
	}

	for _, tc := range tcs {
		t.Run(tc.input, func(t *testing.T) {
			path, err := parsePath(tc.input)
			if err != nil {
				t.Fatalf("parsePath(%q) error: %v", tc.input, err)
			}
			if got := len(path.GetElem()); got != tc.wantLen {
				t.Errorf("got %d elems, want %d", got, tc.wantLen)
			}
			if tc.wantLen > 0 {
				last := path.GetElem()[tc.wantLen-1]
				if last.GetName() != tc.wantName {
					t.Errorf("last elem name = %q, want %q", last.GetName(), tc.wantName)
				}
			}
		})
	}
}

func TestParsePathWithKeys(t *testing.T) {
	path, err := parsePath("/interfaces/interface[name=eth0]")
	if err != nil {
		t.Fatalf("parsePath error: %v", err)
	}
	elems := path.GetElem()
	if len(elems) != 2 {
		t.Fatalf("expected 2 elems, got %d", len(elems))
	}
	keys := elems[1].GetKey()
	if keys["name"] != "eth0" {
		t.Errorf("key[name] = %q, want %q", keys["name"], "eth0")
	}
}

func TestPathToString(t *testing.T) {
	path, _ := parsePath("/interfaces/interface[name=eth0]/state")
	got := pathToString(path)
	if got != "/interfaces/interface[name=eth0]/state" {
		t.Errorf("pathToString = %q, want %q", got, "/interfaces/interface[name=eth0]/state")
	}
}

func TestConfigSourceConfigType(t *testing.T) {
	cfg := Config{Name: "test", Type: "gnmi"}
	if got := cfg.SourceConfigType(); got != "gnmi" {
		t.Errorf("SourceConfigType() = %q, want %q", got, "gnmi")
	}
}

func TestInitializeRequiresPassword(t *testing.T) {
	cfg := Config{
		Name:     "test-gnmi",
		Type:     SourceType,
		Host:     "127.0.0.1",
		Port:     57400,
		Username: "admin",
		Password: "",
	}

	_, err := cfg.Initialize(context.Background(), nil)
	if err == nil {
		t.Fatal("expected Initialize to fail for missing password")
	}
	if !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitializeRejectsInvalidPort(t *testing.T) {
	cfg := Config{
		Name:     "test-gnmi",
		Type:     SourceType,
		Host:     "127.0.0.1",
		Port:     70000,
		Username: "admin",
		Password: "secret",
	}

	_, err := cfg.Initialize(context.Background(), nil)
	if err == nil {
		t.Fatal("expected Initialize to fail for invalid port")
	}
	if !strings.Contains(err.Error(), "port must be between 1 and 65535") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type mockGNMIClient struct{}

func (m *mockGNMIClient) Capabilities(context.Context, *pb.CapabilityRequest, ...grpc.CallOption) (*pb.CapabilityResponse, error) {
	return nil, nil
}

func (m *mockGNMIClient) Get(ctx context.Context, req *pb.GetRequest, opts ...grpc.CallOption) (*pb.GetResponse, error) {
	_ = req
	_ = opts
	<-ctx.Done()
	return nil, ctx.Err()
}

func (m *mockGNMIClient) Set(context.Context, *pb.SetRequest, ...grpc.CallOption) (*pb.SetResponse, error) {
	return nil, nil
}

func (m *mockGNMIClient) Subscribe(context.Context, ...grpc.CallOption) (pb.GNMI_SubscribeClient, error) {
	return nil, nil
}

func TestGnmiGetHonorsSourceTimeout(t *testing.T) {
	s := &Source{
		Config:  Config{Name: "test", Timeout: "10ms", Username: "admin", Password: "secret"},
		client:  &mockGNMIClient{},
		timeout: 10 * time.Millisecond,
	}
	start := time.Now()
	_, err := s.GnmiGet(context.Background(), []string{"/interfaces/interface"}, "JSON_IETF")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Fatalf("GnmiGet took too long: %v", elapsed)
	}
}
