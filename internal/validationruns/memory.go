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

package validationruns

import (
	"context"
	"sync"
	"time"
)

type MemoryStore struct {
	mu      sync.RWMutex
	runs    map[string]RunRecord
	results map[string]RunResult
	events  map[string][]RunEvent
	nextSeq map[string]int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		runs:    make(map[string]RunRecord),
		results: make(map[string]RunResult),
		events:  make(map[string][]RunEvent),
		nextSeq: make(map[string]int64),
	}
}

func (s *MemoryStore) CreateRun(_ context.Context, run RunRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = run
	return nil
}

func (s *MemoryStore) UpdateRun(_ context.Context, patch RunPatch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.runs[patch.ID]
	if !ok {
		return ErrRunNotFound
	}
	applyPatch(&run, patch)
	s.runs[patch.ID] = run
	return nil
}

func (s *MemoryStore) AppendEvent(_ context.Context, event RunEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextSeq[event.RunID]++
	event.Sequence = s.nextSeq[event.RunID]
	s.events[event.RunID] = append(s.events[event.RunID], event)
	return nil
}

func (s *MemoryStore) PutResult(_ context.Context, result RunResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.results[result.RunID] = result
	return nil
}

func (s *MemoryStore) GetRun(_ context.Context, runID string) (RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[runID]
	if !ok {
		return RunRecord{}, ErrRunNotFound
	}
	return run, nil
}

func (s *MemoryStore) GetResult(_ context.Context, runID string) (RunResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result, ok := s.results[runID]
	if !ok {
		return RunResult{}, ErrRunNotFound
	}
	return result, nil
}

func (s *MemoryStore) ListEvents(_ context.Context, runID string, after int64, limit int) ([]RunEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	events := s.events[runID]
	out := make([]RunEvent, 0, len(events))
	for _, event := range events {
		if event.Sequence <= after {
			continue
		}
		out = append(out, event)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *MemoryStore) FindActiveByIdempotencyKey(_ context.Context, key string) (RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, run := range s.runs {
		if run.IdempotencyKey != key {
			continue
		}
		if run.Status == StatusAccepted || run.Status == StatusRunning {
			return run, nil
		}
	}
	return RunRecord{}, ErrRunNotFound
}

func (s *MemoryStore) MarkRunningInterrupted(_ context.Context, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for id, run := range s.runs {
		if run.Status != StatusRunning {
			continue
		}
		run.Status = StatusInterrupted
		run.Summary = "validation run interrupted by nocfoundry restart"
		run.UpdatedAt = now
		run.CompletedAt = &now
		run.IdempotencyKey = ""
		s.runs[id] = run
	}
	return nil
}

func (s *MemoryStore) DeleteEventsBefore(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, events := range s.events {
		kept := events[:0]
		for _, event := range events {
			if event.Timestamp.Before(before) {
				continue
			}
			kept = append(kept, event)
		}
		s.events[id] = append([]RunEvent(nil), kept...)
	}
	return nil
}

func (s *MemoryStore) DeleteExpired(_ context.Context, before time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, run := range s.runs {
		if run.ExpiresAt == nil || run.ExpiresAt.After(before) {
			continue
		}
		switch run.Status {
		case StatusCompleted, StatusFailed, StatusCancelled, StatusInterrupted:
			delete(s.runs, id)
			delete(s.results, id)
			delete(s.events, id)
			delete(s.nextSeq, id)
		}
	}
	return nil
}

func (s *MemoryStore) ListRunsByStatus(_ context.Context, statuses ...RunStatus) ([]RunRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allowed := map[RunStatus]struct{}{}
	for _, status := range statuses {
		allowed[status] = struct{}{}
	}
	runs := make([]RunRecord, 0)
	for _, run := range s.runs {
		if _, ok := allowed[run.Status]; ok {
			runs = append(runs, run)
		}
	}
	return runs, nil
}

func applyPatch(run *RunRecord, patch RunPatch) {
	if patch.Status != nil {
		run.Status = *patch.Status
	}
	if patch.Outcome != nil {
		run.Outcome = *patch.Outcome
	}
	if patch.CurrentStage != nil {
		run.CurrentStage = *patch.CurrentStage
	}
	if patch.CurrentStep != nil {
		run.CurrentStep = *patch.CurrentStep
	}
	if patch.Attempt != nil {
		run.Attempt = *patch.Attempt
	}
	if patch.CancellationRequested != nil {
		run.CancellationRequested = *patch.CancellationRequested
	}
	if patch.IdempotencyKey != nil {
		run.IdempotencyKey = *patch.IdempotencyKey
	}
	if patch.Summary != nil {
		run.Summary = *patch.Summary
	}
	if patch.StartedAt != nil {
		run.StartedAt = patch.StartedAt
	}
	if patch.UpdatedAt != nil {
		run.UpdatedAt = *patch.UpdatedAt
	}
	if patch.CompletedAt != nil {
		run.CompletedAt = patch.CompletedAt
	}
	if patch.ExpiresAt != nil {
		run.ExpiresAt = patch.ExpiresAt
	}
}
