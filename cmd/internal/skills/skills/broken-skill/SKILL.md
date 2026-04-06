---
name: broken-skill
description: Network operations agent skill for the inspection-workflow workflow.
---

## Overview

This agent skill packages the `inspection-workflow` network operations workflow for NOCFoundry.

## Preconditions

- `nocfoundry` must be available in `PATH`.
- Run commands from the generated skill directory so relative `assets/` paths resolve correctly.
- Base config arguments: `--tools-file assets/tools.yaml`

## Workflow

1. Operation: `hello-http`
   hello tool

## Tools

### hello-http

hello tool

Safety: No explicit safety hints are declared.

Invoke:

```bash
nocfoundry --tools-file assets/tools.yaml invoke hello-http '{"param_name":"param_value"}'
```

