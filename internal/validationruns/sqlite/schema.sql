PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS validation_runs (
  id TEXT PRIMARY KEY,
  run_type TEXT NOT NULL,
  tool_name TEXT NOT NULL,
  tool_type TEXT NOT NULL,
  status TEXT NOT NULL,
  outcome TEXT,
  resource_version INTEGER NOT NULL,
  config_fingerprint TEXT NOT NULL,
  plan_fingerprint TEXT NOT NULL,
  current_stage TEXT,
  current_step TEXT,
  attempt INTEGER NOT NULL DEFAULT 0,
  cancellation_requested INTEGER NOT NULL DEFAULT 0,
  idempotency_key TEXT,
  summary TEXT,
  created_at TEXT NOT NULL,
  started_at TEXT,
  updated_at TEXT NOT NULL,
  completed_at TEXT,
  expires_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_validation_runs_status ON validation_runs(status);
CREATE INDEX IF NOT EXISTS idx_validation_runs_expires_at ON validation_runs(expires_at);
CREATE INDEX IF NOT EXISTS idx_validation_runs_idempotency_key ON validation_runs(idempotency_key);

CREATE TABLE IF NOT EXISTS validation_run_events (
  run_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  timestamp TEXT NOT NULL,
  type TEXT NOT NULL,
  stage TEXT,
  step TEXT,
  attempt INTEGER NOT NULL DEFAULT 0,
  message TEXT,
  payload BLOB,
  PRIMARY KEY (run_id, sequence),
  FOREIGN KEY (run_id) REFERENCES validation_runs(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_validation_run_events_timestamp ON validation_run_events(timestamp);

CREATE TABLE IF NOT EXISTS validation_run_results (
  run_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  outcome TEXT,
  config_fingerprint TEXT NOT NULL,
  plan_fingerprint TEXT NOT NULL,
  result BLOB NOT NULL,
  stored_at TEXT NOT NULL,
  FOREIGN KEY (run_id) REFERENCES validation_runs(id) ON DELETE CASCADE
);
