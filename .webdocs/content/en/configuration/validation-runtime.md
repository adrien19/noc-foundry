+++
title = "Validation Runtime"
linkTitle = "Validation Runtime"
weight = 23
type = "docs"
description = "Execution backend, persistence, retention, and concurrency for validation runs."
+++

# Validation Runtime

`--validation-config` controls how validation runs are executed and stored.

## Key runtime settings

- `executionBackend`
- `storeBackend`
- `sqlitePath`
- `durableTaskSQLitePath`
- `maxConcurrentRuns`
- `maxConcurrentSteps`
- `resultRetention`
- `eventRetention`

## Example

```yaml
executionBackend: durabletask
storeBackend: sqlite
sqlitePath: /var/lib/nocfoundry/noc-foundry-validation-runs.sqlite
durableTaskSQLitePath: /var/lib/nocfoundry/noc-foundry-validation-taskhub.sqlite
maxConcurrentRuns: 4
maxConcurrentSteps: 4
resultRetention: 24h
eventRetention: 24h
```

## Backend choices

- `local` is simpler and good for lightweight execution
- `durabletask` is better for long-running validations that should survive interruption

## Store choices

- `memory` is ephemeral
- `sqlite` provides persistence for status, results, and events

## Recommended local lab setup

For the protected validation example, use:

- `executionBackend: durabletask`
- `storeBackend: sqlite`

That matches the shipped runtime example in [`examples/validation-runtime-configs/durable-validation-sqlite.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/validation-runtime-configs/durable-validation-sqlite.yaml).
