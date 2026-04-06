+++
title = "Server Config"
linkTitle = "Server Config"
weight = 22
type = "docs"
description = "Server-scoped runtime policy for endpoint auth and browser UI auth."
+++

# Server Config

`--server-config` carries server-wide runtime policy that should not be owned by individual tool catalog files.

## Current focus

Today, the most important server config capabilities are:

- endpoint auth for `/api`
- endpoint auth for `/mcp`
- browser UI auth configuration for PKCE login

## Example

```yaml
auth:
  endpointAuth:
    api:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: ${NOCFOUNDRY_BASE_URL:http://127.0.0.1:5000}/api
    mcp:
      enabled: true
      authServices: ["noc-keycloak"]
      audience: ${NOCFOUNDRY_BASE_URL:http://127.0.0.1:5000}/mcp
  ui:
    enabled: true
    authService: noc-keycloak
    clientId: ${KEYCLOAK_UI_CLIENT_ID:noc-foundry-ui}
    scopes: ["openid", "profile", "email"]
    redirectPath: /ui/auth/callback
```

## Rules to remember

- endpoint auth policy is global to the server, not per tool catalog
- auth services are still defined in tool catalogs and referenced here by name
- UI login depends on API endpoint auth being enabled

## Start command

```bash
./nocfoundry \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```
