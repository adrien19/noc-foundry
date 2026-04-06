# NOCFoundry

NOCFoundry is a network-focused MCP server for network elements and adjacent operational systems.

## Scope

- Network-focused MCP tools and sources.
- Validation-run workflows and agent-facing skills.
- Protected `/api` and `/mcp` surfaces with OIDC-based endpoint auth.
- Static public docs in `.webdocs/` and runnable configs in `examples/`.

## Quick Start

Build the CLI:

```bash
go build -o nocfoundry
```

Run a local example:

```bash
./nocfoundry \
  --tools-file examples/tools-configs/nokia-srlinux-lab.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml
```

Run a protected local example with UI and Keycloak-backed auth:

```bash
./nocfoundry \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```

## Documentation

- Public docs source lives in `.webdocs/`.
- Stable runnable sample configs live in `examples/`.
- Engineering and contributor process docs live in `docs/`.

## Agent Skills

NOCFoundry can generate agent-facing workflow bundles with `skills-generate`.

These skills are designed for network operations workflows rather than generic tool wrappers:

- one skill per explicit toolset by default
- direct `nocfoundry invoke ...` guidance instead of generated runtime scripts
- optional promptset guidance packaged alongside the workflow
- copied config assets plus a machine-readable `skill.yaml` bundle manifest

## Legal and Attribution Policy

- Repository license: Apache-2.0.
- New NOCFoundry-authored files should use an approved project attribution header that accurately reflects repository ownership.
- Contributors must preserve license headers, notices, and required attribution when adding or modifying third-party open source code.
- Contributors must not add code, assets, or dependencies whose licenses are incompatible with Apache-2.0 distribution in this repository.
- Contributors must document any material third-party code intake in the relevant PR and keep file-level attribution accurate.

See:

- `docs/compliance/ATTRIBUTION_GATE.md`
- `docs/pr-templates/COMPLIANCE_PR.md`
