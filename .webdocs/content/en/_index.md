+++
title = "NOCFoundry Docs"
linkTitle = "Docs"
type = "docs"
+++

# NOCFoundry Docs

NOCFoundry is a network-operations-focused MCP server for device access, validation workflows, protected operational APIs, and agent-facing network tooling.

## Start Here

- [Local quickstart]({{< relref "getting-started/local-quickstart.md" >}})
- [MCP client quickstart]({{< relref "getting-started/mcp-client-quickstart.md" >}})
- [Server config]({{< relref "configuration/server-config.md" >}})
- [Validation runtime]({{< relref "configuration/validation-runtime.md" >}})
- [OIDC endpoint auth]({{< relref "auth/oidc-endpoint-auth.md" >}})
- [UI login with Keycloak]({{< relref "auth/ui-login-keycloak.md" >}})

## Focus Areas

- Vendor-agnostic network tools with protocol-aware routing (SSH, NETCONF, gNMI)
- Native YANG schema integration for schema-driven path resolution
- Deterministic validation workflows for pre-change and post-change checks
- Protected `/api` and `/mcp` surfaces backed by OIDC
- Agent-facing skills and prompts for network operations
- Canonical data normalization across vendors and protocols

## Common Paths

- Operators should start with [Local quickstart]({{< relref "getting-started/local-quickstart.md" >}}).
- Platform engineers should review [Server config]({{< relref "configuration/server-config.md" >}}) and [OIDC endpoint auth]({{< relref "auth/oidc-endpoint-auth.md" >}}).
- Workflow authors should read [Validation runs]({{< relref "workflows/validation-runs.md" >}}) and [Validate]({{< relref "resources/tools/validate.md" >}}).
- Network tool authors should review [Network tools]({{< relref "resources/tools/_index.md" >}}) and [YANG schemas]({{< relref "configuration/yang-schemas.md" >}}).
