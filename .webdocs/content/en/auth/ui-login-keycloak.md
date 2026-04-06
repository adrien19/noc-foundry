+++
title = "UI Login with Keycloak"
linkTitle = "UI Login with Keycloak"
weight = 32
type = "docs"
description = "Use browser OIDC login with PKCE to access the protected NOCFoundry UI."
+++

# UI Login with Keycloak

NOCFoundry’s browser UI acts as an OIDC public client and uses Authorization Code + PKCE to access the protected `/api` surface.

## Local demo flow

1. Start Keycloak:

```bash
docker compose -f examples/keycloak/docker-compose.keycloak.yaml up -d
```

2. Bootstrap the demo realm and clients:

```bash
./examples/keycloak/keycloak-setup.sh
```

3. Start NOCFoundry with:

- [`examples/tools-configs/keycloak-protected-validation.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/keycloak-protected-validation.yaml)
- [`examples/server-configs/protected-api-mcp-ui.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/server-configs/protected-api-mcp-ui.yaml)

## Important client details

The setup script creates:

- `noc-foundry` for the resource-side auth service
- `noc-foundry-ui` for the browser PKCE client

The UI client must:

- allow `/ui/auth/callback`
- allow `/ui/` for post-logout redirect
- include the API audience expected by endpoint auth

## Logout behavior

The browser logout flow sends:

- `client_id`
- `post_logout_redirect_uri`
- `id_token_hint`

That allows Keycloak to complete logout cleanly and return the operator to the UI without the extra confirmation screen.
