+++
title = "Validation Runs"
linkTitle = "Validation Runs"
weight = 41
type = "docs"
description = "Long-running validation workflows, status polling, and result retrieval."
+++

# Validation Runs

Validation runs provide a long-running execution model for multi-step network validation workflows.

Typical lifecycle:

1. start a run
2. poll status
3. fetch final result
4. cancel if needed

Use the protected validation example tools for this workflow:

- `start_validation_run`
- `validation_run_status`
- `validation_run_result`
- `cancel_validation_run`
