+++
title = "FAQ"
linkTitle = "FAQ"
weight = 72
type = "docs"
description = "Common questions about NOCFoundry runtime behavior and deployment."
+++

# FAQ

## Does NOCFoundry execute prompts itself?

No. Prompt resources are exposed for clients and agents to consume. NOCFoundry does not execute prompts against an LLM.

## Why is `/api` returning 401?

If endpoint auth is enabled, callers must present a bearer token that matches the configured `/api` audience.

## Does the UI require login?

The UI shell can remain public while its backing `/api` calls are protected. When UI auth is enabled, the browser signs in through OIDC + PKCE.

