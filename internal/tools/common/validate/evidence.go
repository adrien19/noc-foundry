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

// Package validate implements the validate MCP tool, a read-only
// declarative validation engine for network devices and blast-radius checks.

package validate

import (
	"encoding/json"
	"fmt"
)

// EvidenceStore tracks collected evidence by name in insertion order.
type EvidenceStore struct {
	byName map[string]Evidence
	order  []string
}

func newEvidenceStore() *EvidenceStore {
	return &EvidenceStore{byName: make(map[string]Evidence)}
}

func (s *EvidenceStore) Put(name string, evidence Evidence) {
	if _, ok := s.byName[name]; !ok {
		s.order = append(s.order, name)
	}
	s.byName[name] = evidence
}

func (s *EvidenceStore) Get(name string) (Evidence, bool) {
	ev, ok := s.byName[name]
	return ev, ok
}

func (s *EvidenceStore) MustGetMany(names []string) ([]Evidence, error) {
	out := make([]Evidence, 0, len(names))
	for _, name := range names {
		ev, ok := s.byName[name]
		if !ok {
			return nil, fmt.Errorf("evidence %q not found", name)
		}
		if len(ev.Records) == 0 {
			return nil, fmt.Errorf("evidence %q is empty", name)
		}
		out = append(out, ev)
	}
	return out, nil
}

func (s *EvidenceStore) All() []Evidence {
	out := make([]Evidence, 0, len(s.order))
	for _, name := range s.order {
		out = append(out, s.byName[name])
	}
	return out
}

func evidenceStoreFromMap(raw map[string]json.RawMessage) (*EvidenceStore, error) {
	store := newEvidenceStore()
	for name, data := range raw {
		var evidence Evidence
		if err := json.Unmarshal(data, &evidence); err != nil {
			return nil, err
		}
		store.Put(name, evidence)
	}
	return store, nil
}

func mapEvidenceStore(store *EvidenceStore) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage, len(store.byName))
	for name, evidence := range store.byName {
		raw, err := json.Marshal(evidence)
		if err != nil {
			continue
		}
		out[name] = raw
	}
	return out
}

func applyEvidenceDelta(store *EvidenceStore, delta map[string]json.RawMessage) error {
	for name, raw := range delta {
		var evidence Evidence
		if err := json.Unmarshal(raw, &evidence); err != nil {
			return err
		}
		store.Put(name, evidence)
	}
	return nil
}
