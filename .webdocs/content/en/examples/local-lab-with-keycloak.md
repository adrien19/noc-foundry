+++
title = "Local Lab with Keycloak"
linkTitle = "Local Lab with Keycloak"
weight = 81
type = "docs"
description = "Run the full protected local demo with Keycloak, UI login, and validation workflows."
+++

# Local Lab with Keycloak

Use this protected local lab stack when you want to test:

- Keycloak-backed OIDC auth services
- endpoint auth on `/api` and `/mcp`
- browser PKCE login for the UI
- durable validation runs

Main files:

- [`examples/tools-configs/keycloak-protected-validation.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/keycloak-protected-validation.yaml)
- [`examples/server-configs/protected-api-mcp-ui.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/server-configs/protected-api-mcp-ui.yaml)
- [`examples/validation-runtime-configs/durable-validation-sqlite.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/validation-runtime-configs/durable-validation-sqlite.yaml)
- [`examples/keycloak/docker-compose.keycloak.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/keycloak/docker-compose.keycloak.yaml)
- [`examples/keycloak/keycloak-setup.sh`](https://github.com/adrien19/noc-foundry/blob/main/examples/keycloak/keycloak-setup.sh)
- [`examples/containerlab/noc-foundry-lab.clab.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/containerlab/noc-foundry-lab.clab.yaml)
- [`examples/containerlab/install-containerlab.sh`](https://github.com/adrien19/noc-foundry/blob/main/examples/containerlab/install-containerlab.sh)

For contributors who want a realistic local network instead of static sample targets,
pair this protected stack with the SR Linux containerlab fabric.

- install containerlab on demand with `./examples/containerlab/install-containerlab.sh`
- deploy the lab with `sudo containerlab deploy -t examples/containerlab/noc-foundry-lab.clab.yaml`
- destroy it with `sudo containerlab destroy -t examples/containerlab/noc-foundry-lab.clab.yaml`

## Start Keycloak

Start the local Keycloak example:

```bash
docker compose -f examples/keycloak/docker-compose.keycloak.yaml up -d
```

Bootstrap the demo realm, clients, and test user:

```bash
./examples/keycloak/keycloak-setup.sh
```

This creates:

- the `network-ops` realm
- the `noc-foundry` resource-side client
- the `noc-foundry-ui` browser PKCE client
- the `noc-operator` test user

## Start NOCFoundry

```bash
./nocfoundry \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```

Then open:

```text
http://127.0.0.1:5000/ui/
```

and sign in with the local Keycloak test account created by
`./examples/keycloak/keycloak-setup.sh`.
