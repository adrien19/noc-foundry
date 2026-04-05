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

// Package sshauth builds golang.org/x/crypto/ssh AuthMethod slices from
// the common password / private-key configuration fields shared by the
// SSH and NETCONF sources.
package sshauth

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// KeyAuth holds the optional private-key authentication parameters.
// At most one of Data (inline PEM) or Path (file path) should be set;
// if both are set, Data takes precedence.
type KeyAuth struct {
	// Path is the filesystem path to a PEM-encoded private key file.
	Path string
	// Data is an inline PEM-encoded private key (overrides Path when non-empty).
	Data string
	// Passphrase decrypts an encrypted private key. Leave empty for unencrypted keys.
	Passphrase string
}

// BuildAuthMethods assembles a slice of ssh.AuthMethod from the provided
// credentials. At least one of password or key must be non-empty.
//
//   - password: if non-empty, ssh.Password is appended first.
//   - key.Data: if non-empty, parsed as a PEM private key (inline).
//   - key.Path: if non-empty (and key.Data is empty), the file is read and
//     parsed as a PEM private key.
//   - key.Passphrase: used when decrypting an encrypted private key.
func BuildAuthMethods(password string, key KeyAuth) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if password != "" {
		methods = append(methods, ssh.Password(password))
	}

	pemData, err := resolvePEM(key)
	if err != nil {
		return nil, err
	}

	if len(pemData) > 0 {
		signer, err := parseSigner(pemData, key.Passphrase)
		if err != nil {
			return nil, err
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("at least one auth method (password or ssh key) is required")
	}

	return methods, nil
}

// resolvePEM returns the raw PEM bytes from inline data or a file path.
func resolvePEM(key KeyAuth) ([]byte, error) {
	if key.Data != "" {
		return []byte(key.Data), nil
	}
	if key.Path != "" {
		data, err := os.ReadFile(key.Path) // #nosec G304 — path comes from trusted config file
		if err != nil {
			return nil, fmt.Errorf("reading ssh key file %q: %w", key.Path, err)
		}
		return data, nil
	}
	return nil, nil
}

// parseSigner parses a PEM-encoded private key, decrypting it with passphrase
// if one is provided.
func parseSigner(pemData []byte, passphrase string) (ssh.Signer, error) {
	if passphrase != "" {
		signer, err := ssh.ParsePrivateKeyWithPassphrase(pemData, []byte(passphrase))
		if err != nil {
			return nil, fmt.Errorf("parsing encrypted ssh private key: %w", err)
		}
		return signer, nil
	}
	signer, err := ssh.ParsePrivateKey(pemData)
	if err != nil {
		return nil, fmt.Errorf("parsing ssh private key: %w", err)
	}
	return signer, nil
}
