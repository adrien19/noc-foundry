+++
title = "OIDC Endpoint Auth"
linkTitle = "OIDC Endpoint Auth"
weight = 31
type = "docs"
description = "Protect `/api` and `/mcp` with OIDC-backed bearer tokens."
+++

# OIDC Endpoint Auth

NOCFoundry can protect the HTTP surfaces themselves, not just individual tool calls.

## Protected surfaces

- `/api`
- `/mcp`

These surfaces are configured separately in `--server-config` and can require different audiences.

## Auth service model

- OIDC providers are defined as `authServices` in tool catalog files
- the server config references those services by name
- only auth services selected by server policy can satisfy a protected surface

## Metadata and RFC 9728

NOCFoundry serves protected resource metadata for:

- `/.well-known/oauth-protected-resource/api`
- `/.well-known/oauth-protected-resource/mcp`

This allows MCP and API clients to discover the backing authorization server metadata.

## Audience behavior

The access token must include the exact configured audience for the target surface. In the protected local example:

- `/api` expects `http://127.0.0.1:5000/api`
- `/mcp` expects `http://127.0.0.1:5000/mcp`

## Layering

Endpoint auth answers:

- can this caller use the service surface at all?

Tool auth still answers:

- can this caller invoke this specific tool?
