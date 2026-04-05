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

package sshauth_test

import (
	"os"
	"testing"

	"github.com/adrien19/noc-foundry/internal/sources/sshauth"
)

// testKeyPEM is a passphrase-less ed25519 key used only in tests.
const testKeyPEM = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW
QyNTUxOQAAACDM4BLMmxp7j18E0ZUBK4Z4BrRKMHNgE99M+DBNfqWLgQAAAIhWyhGEVsoR
hAAAAAtzc2gtZWQyNTUxOQAAACDM4BLMmxp7j18E0ZUBK4Z4BrRKMHNgE99M+DBNfqWLgQ
AAAEBw2Yz1HIZY1R8tmeK/OVcwvaWbuNeZ7gl4oxmT6LbLn8zgEsybGnuPXwTRlQErhngG
tEowc2AT30z4ME1+pYuBAAAABHRlc3QB
-----END OPENSSH PRIVATE KEY-----
`

// testKeyEncPEM is an ed25519 key encrypted with passphrase "hunter2".
const testKeyEncPEM = `-----BEGIN OPENSSH PRIVATE KEY-----
b3BlbnNzaC1rZXktdjEAAAAACmFlczI1Ni1jdHIAAAAGYmNyeXB0AAAAGAAAABBOHyRm2j
yYiJlvQQQU8+0nAAAAEAAAAAEAAAAzAAAAC3NzaC1lZDI1NTE5AAAAIETXy4gGo9uRlB15
69HXLYTSRu+Yfmzn9Jq9k2JO5OR7AAAAkIRmakqY1V/MWEX3PFQ4pMauAvdDsiOjd08DMK
BVD7474LSSNbZIpyvniVgDohGnbawoXN4oNd2hf/GCU5Lsf3nol1qYqkRczVpu7xm9Z8RL
CkF4i8XAU/L0VYDKrXgKoSfmsSGKWOpwUB8HFCoU81B5MN8+mfv1w5CpAkKpYQuJX0EO+v
ZJ78S7usYze926Zg==
-----END OPENSSH PRIVATE KEY-----
`

func TestBuildAuthMethodsPassword(t *testing.T) {
	methods, err := sshauth.BuildAuthMethods("secret", sshauth.KeyAuth{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestBuildAuthMethodsNoAuth(t *testing.T) {
	_, err := sshauth.BuildAuthMethods("", sshauth.KeyAuth{})
	if err == nil {
		t.Fatal("expected error when no auth method provided, got nil")
	}
}

func TestBuildAuthMethodsKeyData(t *testing.T) {
	methods, err := sshauth.BuildAuthMethods("", sshauth.KeyAuth{Data: testKeyPEM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestBuildAuthMethodsKeyDataEncrypted(t *testing.T) {
	methods, err := sshauth.BuildAuthMethods("", sshauth.KeyAuth{
		Data:       testKeyEncPEM,
		Passphrase: "hunter2",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestBuildAuthMethodsKeyDataWrongPassphrase(t *testing.T) {
	_, err := sshauth.BuildAuthMethods("", sshauth.KeyAuth{
		Data:       testKeyEncPEM,
		Passphrase: "wrong",
	})
	if err == nil {
		t.Fatal("expected error with wrong passphrase, got nil")
	}
}

func TestBuildAuthMethodsKeyPath(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "test_key_*")
	if err != nil {
		t.Fatalf("creating temp key file: %v", err)
	}
	if _, err := f.WriteString(testKeyPEM); err != nil {
		t.Fatalf("writing temp key file: %v", err)
	}
	f.Close()

	methods, err := sshauth.BuildAuthMethods("", sshauth.KeyAuth{Path: f.Name()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 1 {
		t.Fatalf("expected 1 auth method, got %d", len(methods))
	}
}

func TestBuildAuthMethodsKeyPathNotFound(t *testing.T) {
	_, err := sshauth.BuildAuthMethods("", sshauth.KeyAuth{Path: "/nonexistent/key"})
	if err == nil {
		t.Fatal("expected error for missing key file, got nil")
	}
}

func TestBuildAuthMethodsBothPasswordAndKey(t *testing.T) {
	methods, err := sshauth.BuildAuthMethods("secret", sshauth.KeyAuth{Data: testKeyPEM})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Password first, then key
	if len(methods) != 2 {
		t.Fatalf("expected 2 auth methods (password+key), got %d", len(methods))
	}
}
