+++
title = "CLI Reference"
linkTitle = "CLI"
weight = 71
type = "docs"
description = "Core CLI entry points and operator-facing commands."
+++

# CLI Reference

The root command is:

```bash
nocfoundry
```

Core entry points include:

- `nocfoundry invoke`
- `nocfoundry skills-generate`
- `nocfoundry --ui`

## Prebuilt catalogs

The `--prebuilt` flag loads a bundled tool catalog by name.

Current bundled prebuilt:

- `validation-runs`

Example:

```bash
./nocfoundry --prebuilt validation-runs
```

`--prebuilt` is not a boolean flag, and it does not take an individual tool name. This is invalid:

```bash
./nocfoundry --prebuilt start_validation_run
```

because `start_validation_run` is a tool inside the `validation-runs` catalog, not the catalog name itself.

You can merge a prebuilt with your own tool catalog in one startup command:

```bash
./nocfoundry \
  --prebuilt validation-runs \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```

This works only when the prebuilt and the custom catalog do not define conflicting resource names.

This page should be expanded with generated flag reference as the CLI stabilizes.
