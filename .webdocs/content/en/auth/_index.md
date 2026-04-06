+++
title = "Auth"
linkTitle = "Auth"
weight = 30
type = "docs"
description = "OIDC auth services, endpoint protection, and browser login."
+++

# Auth

NOCFoundry supports layered protection:

- endpoint auth at `/api` and `/mcp`
- tool-level authorization inside the protected service
- browser PKCE login for the UI

Start with:

- [OIDC endpoint auth]({{< relref "oidc-endpoint-auth.md" >}})
- [UI login with Keycloak]({{< relref "ui-login-keycloak.md" >}})

