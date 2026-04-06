# NOCFoundry Examples

This directory contains stable, documentation-friendly example configuration
files for NOCFoundry.

Use these files as reference examples in docs and walkthroughs instead of
`local-configs/`, which is free to evolve as a development scratch area.

Example sets:

- `tools-configs/nokia-srlinux-lab.yaml`
  Read-only Nokia SR Linux device-group and tool catalog example.
- `tools-configs/keycloak-protected-validation.yaml`
  Protected validation workflow, prompts, and toolset example.
- `server-configs/protected-api-mcp-ui.yaml`
  Server-scoped auth policy for protected `/api`, `/mcp`, and browser UI login.
- `validation-runtime-configs/durable-validation-sqlite.yaml`
  Durable validation runtime using SQLite and the durabletask backend.
- `containerlab/noc-foundry-lab.clab.yaml`
  Local SR Linux fabric topology for contributors who want a realistic network lab.
- `containerlab/install-containerlab.sh`
  Optional installer for containerlab when you want to run the local topology.
