+++
title = "Tool Catalogs"
linkTitle = "Tool Catalogs"
weight = 21
type = "docs"
description = "How NOCFoundry loads and merges tools, sources, auth services, prompts, and toolsets."
+++

# Tool Catalogs

Tool catalogs define the operational resources NOCFoundry loads at startup.

## Supported catalog flags

- `--tools-file`
- `--tools-files`
- `--tools-folder`
- `--prebuilt`

These are mutually exclusive where appropriate:

- `--tools-file`, `--tools-files`, and `--tools-folder` cannot be combined
- `--prebuilt` can be combined with custom tool catalogs when you want bundled capabilities plus your own configs

## What a catalog can contain

Tool catalogs can define:

- sources
- device groups
- auth services
- tools
- toolsets
- prompts
- promptsets
- embedding models

## Prebuilt catalogs

`--prebuilt` takes a bundled catalog name, not an individual tool name.

Current bundled prebuilt catalogs:

- `validation-runs`

For example, this is valid:

```bash
./nocfoundry --prebuilt validation-runs
```

This is not valid:

```bash
./nocfoundry --prebuilt start_validation_run
```

because `start_validation_run` is a tool inside the `validation-runs` prebuilt catalog, not the catalog name itself.

## Merge behavior

When multiple files are loaded through `--tools-files` or `--tools-folder`, NOCFoundry merges the resource sets into one runtime catalog. Resource names must remain unique inside their kind.

Prebuilt catalogs are merged into that same runtime catalog. This means:

- users can start NOCFoundry with `--prebuilt` plus their own tool catalog in one command
- duplicate resource names across prebuilt and custom configs fail with a conflict error
- server-scoped auth policy in `--server-config` still requires matching `authServices` to be present in the loaded tool catalogs

Example: merge the bundled validation lifecycle tools with a custom validation definition:

```bash
./nocfoundry \
  --prebuilt validation-runs \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```

In that combined startup:

- `validation-runs` contributes:
  - `start_validation_run`
  - `validation_run_status`
  - `validation_run_result`
  - `cancel_validation_run`
- your custom tool catalog contributes:
  - `maintenance_validation`
  - `noc-keycloak`
  - any additional prompts, toolsets, or device groups

If the custom tool catalog already defines the same validation-run lifecycle tools, the merge will fail. NOCFoundry does not silently alias or override duplicate resources.

## Recommended practice

- keep lab and production catalogs separate
- keep tool catalogs focused on operational resources, not server policy
- put `/api`, `/mcp`, and UI auth policy in `--server-config`
- use `--prebuilt` for small generic operational bundles, and keep richer end-to-end workflows in `examples/`

## Reference examples

- [`examples/tools-configs/keycloak-protected-validation.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/keycloak-protected-validation.yaml)
- [`examples/tools-configs/nokia-srlinux-lab.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/nokia-srlinux-lab.yaml)
- [`examples/server-configs/protected-api-mcp-ui.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/server-configs/protected-api-mcp-ui.yaml)
