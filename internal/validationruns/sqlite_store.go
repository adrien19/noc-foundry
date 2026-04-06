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
	"database/sql"
	_ "embed"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed sqlite/schema.sql
var sqliteSchema string

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open validation run sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db}
	if err := store.init(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) init(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, sqliteSchema); err != nil {
		return fmt.Errorf("initialize validation run sqlite schema: %w", err)
	}
	return nil
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run RunRecord) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO validation_runs (
	id, run_type, tool_name, tool_type, status, outcome, resource_version,
	config_fingerprint, plan_fingerprint, current_stage, current_step, attempt,
	cancellation_requested, idempotency_key, summary, created_at, started_at,
	updated_at, completed_at, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.RunType, run.ToolName, run.ToolType, string(run.Status), nullableString(run.Outcome),
		run.ResourceVersion, run.ConfigFingerprint, run.PlanFingerprint, nullableString(run.CurrentStage),
		nullableString(run.CurrentStep), run.Attempt, boolInt(run.CancellationRequested), nullableString(run.IdempotencyKey),
		nullableString(run.Summary), ts(run.CreatedAt), tsPtr(run.StartedAt), ts(run.UpdatedAt), tsPtr(run.CompletedAt), tsPtr(run.ExpiresAt),
	)
	if err != nil {
		return fmt.Errorf("create validation run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateRun(ctx context.Context, patch RunPatch) error {
	assignments := make([]string, 0, 10)
	args := make([]any, 0, 10)
	if patch.Status != nil {
		assignments = append(assignments, "status = ?")
		args = append(args, string(*patch.Status))
	}
	if patch.Outcome != nil {
		assignments = append(assignments, "outcome = ?")
		args = append(args, nullableString(*patch.Outcome))
	}
	if patch.CurrentStage != nil {
		assignments = append(assignments, "current_stage = ?")
		args = append(args, nullableString(*patch.CurrentStage))
	}
	if patch.CurrentStep != nil {
		assignments = append(assignments, "current_step = ?")
		args = append(args, nullableString(*patch.CurrentStep))
	}
	if patch.Attempt != nil {
		assignments = append(assignments, "attempt = ?")
		args = append(args, *patch.Attempt)
	}
	if patch.CancellationRequested != nil {
		assignments = append(assignments, "cancellation_requested = ?")
		args = append(args, boolInt(*patch.CancellationRequested))
	}
	if patch.IdempotencyKey != nil {
		assignments = append(assignments, "idempotency_key = ?")
		args = append(args, nullableString(*patch.IdempotencyKey))
	}
	if patch.Summary != nil {
		assignments = append(assignments, "summary = ?")
		args = append(args, nullableString(*patch.Summary))
	}
	if patch.StartedAt != nil {
		assignments = append(assignments, "started_at = ?")
		args = append(args, tsPtr(patch.StartedAt))
	}
	if patch.UpdatedAt != nil {
		assignments = append(assignments, "updated_at = ?")
		args = append(args, ts(*patch.UpdatedAt))
	}
	if patch.CompletedAt != nil {
		assignments = append(assignments, "completed_at = ?")
		args = append(args, tsPtr(patch.CompletedAt))
	}
	if patch.ExpiresAt != nil {
		assignments = append(assignments, "expires_at = ?")
		args = append(args, tsPtr(patch.ExpiresAt))
	}
	if len(assignments) == 0 {
		return nil
	}
	args = append(args, patch.ID)
	result, err := s.db.ExecContext(ctx, "UPDATE validation_runs SET "+strings.Join(assignments, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return fmt.Errorf("update validation run: %w", err)
	}
	rows, err := result.RowsAffected()
	if err == nil && rows == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (s *SQLiteStore) AppendEvent(ctx context.Context, event RunEvent) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin validation run event transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var nextSeq int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence), 0) + 1 FROM validation_run_events WHERE run_id = ?", event.RunID).Scan(&nextSeq); err != nil {
		return fmt.Errorf("compute validation run event sequence: %w", err)
	}
	event.Sequence = nextSeq
	if _, err = tx.ExecContext(ctx, `
INSERT INTO validation_run_events (run_id, sequence, timestamp, type, stage, step, attempt, message, payload)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.RunID, event.Sequence, ts(event.Timestamp), event.Type, nullableString(event.Stage), nullableString(event.Step),
		event.Attempt, nullableString(event.Message), []byte(event.Payload),
	); err != nil {
		return fmt.Errorf("insert validation run event: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit validation run event: %w", err)
	}
	return nil
}

func (s *SQLiteStore) PutResult(ctx context.Context, result RunResult) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO validation_run_results (run_id, status, outcome, config_fingerprint, plan_fingerprint, result, stored_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id) DO UPDATE SET
	status = excluded.status,
	outcome = excluded.outcome,
	config_fingerprint = excluded.config_fingerprint,
	plan_fingerprint = excluded.plan_fingerprint,
	result = excluded.result,
	stored_at = excluded.stored_at`,
		result.RunID, string(result.Status), nullableString(result.Outcome), result.ConfigFingerprint, result.PlanFingerprint, []byte(result.Result), ts(result.StoredAt),
	)
	if err != nil {
		return fmt.Errorf("store validation run result: %w", err)
	}
	return nil
}

func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, run_type, tool_name, tool_type, status, outcome, resource_version, config_fingerprint, plan_fingerprint,
	current_stage, current_step, attempt, cancellation_requested, idempotency_key, summary, created_at, started_at,
	updated_at, completed_at, expires_at
FROM validation_runs WHERE id = ?`, runID)
	return scanRun(row)
}

func (s *SQLiteStore) GetResult(ctx context.Context, runID string) (RunResult, error) {
	var result RunResult
	var status string
	var outcome sql.NullString
	var storedAt string
	var raw []byte
	err := s.db.QueryRowContext(ctx, `
SELECT run_id, status, outcome, config_fingerprint, plan_fingerprint, result, stored_at
FROM validation_run_results WHERE run_id = ?`, runID).Scan(
		&result.RunID, &status, &outcome, &result.ConfigFingerprint, &result.PlanFingerprint, &raw, &storedAt,
	)
	if err == sql.ErrNoRows {
		return RunResult{}, ErrRunNotFound
	}
	if err != nil {
		return RunResult{}, fmt.Errorf("get validation run result: %w", err)
	}
	result.Status = RunStatus(status)
	result.Outcome = outcome.String
	result.Result = raw
	result.StoredAt, err = parseTS(storedAt)
	if err != nil {
		return RunResult{}, err
	}
	return result, nil
}

func (s *SQLiteStore) ListEvents(ctx context.Context, runID string, after int64, limit int) ([]RunEvent, error) {
	query := `
SELECT run_id, sequence, timestamp, type, stage, step, attempt, message, payload
FROM validation_run_events
WHERE run_id = ? AND sequence > ?
ORDER BY sequence ASC`
	args := []any{runID, after}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list validation run events: %w", err)
	}
	defer func() { _ = rows.Close() }()
	events := make([]RunEvent, 0)
	for rows.Next() {
		var event RunEvent
		var tsValue string
		var stage, step, message sql.NullString
		var payload []byte
		if err := rows.Scan(&event.RunID, &event.Sequence, &tsValue, &event.Type, &stage, &step, &event.Attempt, &message, &payload); err != nil {
			return nil, fmt.Errorf("scan validation run event: %w", err)
		}
		event.Timestamp, err = parseTS(tsValue)
		if err != nil {
			return nil, err
		}
		event.Stage = stage.String
		event.Step = step.String
		event.Message = message.String
		event.Payload = payload
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *SQLiteStore) FindActiveByIdempotencyKey(ctx context.Context, key string) (RunRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, run_type, tool_name, tool_type, status, outcome, resource_version, config_fingerprint, plan_fingerprint,
	current_stage, current_step, attempt, cancellation_requested, idempotency_key, summary, created_at, started_at,
	updated_at, completed_at, expires_at
FROM validation_runs
WHERE idempotency_key = ? AND status IN (?, ?)
ORDER BY created_at DESC
LIMIT 1`, key, string(StatusAccepted), string(StatusRunning))
	return scanRun(row)
}

func (s *SQLiteStore) MarkRunningInterrupted(ctx context.Context, reason string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
UPDATE validation_runs
SET status = ?, summary = ?, updated_at = ?, completed_at = ?, idempotency_key = NULL
WHERE status = ?`, string(StatusInterrupted), reason, ts(now), ts(now), string(StatusRunning))
	if err != nil {
		return fmt.Errorf("mark validation runs interrupted: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteEventsBefore(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM validation_run_events WHERE timestamp < ?`, ts(before))
	if err != nil {
		return fmt.Errorf("delete expired validation run events: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpired(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `
DELETE FROM validation_runs
WHERE expires_at IS NOT NULL
  AND expires_at < ?
  AND status IN (?, ?, ?, ?)`,
		ts(before), string(StatusCompleted), string(StatusFailed), string(StatusCancelled), string(StatusInterrupted),
	)
	if err != nil {
		return fmt.Errorf("delete expired validation runs: %w", err)
	}
	return nil
}

func (s *SQLiteStore) ListRunsByStatus(ctx context.Context, statuses ...RunStatus) ([]RunRecord, error) {
	if len(statuses) == 0 {
		return nil, nil
	}
	placeholders := make([]string, 0, len(statuses))
	args := make([]any, 0, len(statuses))
	for _, status := range statuses {
		placeholders = append(placeholders, "?")
		args = append(args, string(status))
	}
	query := `
SELECT id, run_type, tool_name, tool_type, status, outcome, resource_version, config_fingerprint, plan_fingerprint,
	current_stage, current_step, attempt, cancellation_requested, idempotency_key, summary, created_at, started_at,
	updated_at, completed_at, expires_at
FROM validation_runs
WHERE status IN (` + strings.Join(placeholders, ",") + `)`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list validation runs by status: %w", err)
	}
	defer func() { _ = rows.Close() }()
	runs := make([]RunRecord, 0)
	for rows.Next() {
		run, err := scanRunRows(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanRun(row *sql.Row) (RunRecord, error) {
	return scanRunScanner(row)
}

func scanRunRows(row *sql.Rows) (RunRecord, error) {
	return scanRunScanner(row)
}

type runScanner interface {
	Scan(dest ...any) error
}

func scanRunScanner(row runScanner) (RunRecord, error) {
	var run RunRecord
	var status string
	var outcome, currentStage, currentStep, idempotencyKey, summary sql.NullString
	var createdAt, updatedAt string
	var startedAt, completedAt, expiresAt sql.NullString
	var cancellationRequested int
	err := row.Scan(
		&run.ID, &run.RunType, &run.ToolName, &run.ToolType, &status, &outcome, &run.ResourceVersion,
		&run.ConfigFingerprint, &run.PlanFingerprint, &currentStage, &currentStep, &run.Attempt,
		&cancellationRequested, &idempotencyKey, &summary, &createdAt, &startedAt, &updatedAt, &completedAt, &expiresAt,
	)
	if err == sql.ErrNoRows {
		return RunRecord{}, ErrRunNotFound
	}
	if err != nil {
		return RunRecord{}, fmt.Errorf("scan validation run: %w", err)
	}
	run.Status = RunStatus(status)
	run.Outcome = outcome.String
	run.CurrentStage = currentStage.String
	run.CurrentStep = currentStep.String
	run.CancellationRequested = cancellationRequested != 0
	run.IdempotencyKey = idempotencyKey.String
	run.Summary = summary.String
	run.CreatedAt, err = parseTS(createdAt)
	if err != nil {
		return RunRecord{}, err
	}
	run.UpdatedAt, err = parseTS(updatedAt)
	if err != nil {
		return RunRecord{}, err
	}
	run.StartedAt, err = parseOptionalTS(startedAt)
	if err != nil {
		return RunRecord{}, err
	}
	run.CompletedAt, err = parseOptionalTS(completedAt)
	if err != nil {
		return RunRecord{}, err
	}
	run.ExpiresAt, err = parseOptionalTS(expiresAt)
	if err != nil {
		return RunRecord{}, err
	}
	return run, nil
}

func ts(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func tsPtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return ts(*t)
}

func parseTS(raw string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse sqlite timestamp %q: %w", raw, err)
	}
	return parsed.UTC(), nil
}

func parseOptionalTS(raw sql.NullString) (*time.Time, error) {
	if !raw.Valid || raw.String == "" {
		return nil, nil
	}
	parsed, err := parseTS(raw.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
