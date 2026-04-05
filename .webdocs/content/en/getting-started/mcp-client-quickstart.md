+++
title = "MCP Client Quickstart"
linkTitle = "MCP Client Quickstart"
weight = 12
type = "docs"
description = "Connect agent clients to the NOCFoundry MCP endpoint."
+++

# MCP Client Quickstart

NOCFoundry exposes the Model Context Protocol at:

```text
http://127.0.0.1:5000/mcp
```

With the protected validation example, `/mcp` is protected by OIDC endpoint auth.

## What clients need

- the MCP endpoint URL
- an access token whose `aud` matches the configured `/mcp` audience
- support for bearer tokens on HTTP-based MCP transports

The protected resource metadata is served from:

```text
/.well-known/oauth-protected-resource/mcp
```

## Minimal connection model

1. Start NOCFoundry with `examples/tools-configs/keycloak-protected-validation.yaml` and `examples/server-configs/protected-api-mcp-ui.yaml`.
2. Obtain an access token from Keycloak.
3. Configure your MCP client to connect to `http://127.0.0.1:5000/mcp`.
4. Supply `Authorization: Bearer <token>` on every HTTP request.

## Audience reminder

The access token must match the `/mcp` audience configured in:

- [`examples/server-configs/protected-api-mcp-ui.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/server-configs/protected-api-mcp-ui.yaml)

By default that is:

```text
http://127.0.0.1:5000/mcp
```

## Related docs

- [OIDC endpoint auth]({{< relref "../auth/oidc-endpoint-auth.md" >}})
- [UI login with Keycloak]({{< relref "../auth/ui-login-keycloak.md" >}})
