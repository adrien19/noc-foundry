+++
title = "Local Quickstart"
linkTitle = "Local Quickstart"
weight = 11
type = "docs"
description = "Build NOCFoundry, run the local server, and execute the first workflow."
+++

# Local Quickstart

This quickstart gets a local NOCFoundry instance running with the validation demo and UI.

## Prerequisites

- Go 1.25 or later
- Docker if you want the local Keycloak flow
- a shell with access to this repository

## Optional: start the local SR Linux topology

If you want to test against a realistic network fabric instead of static examples,
use the containerlab topology in `examples/containerlab/`.

Install containerlab only when you need it:

```bash
./examples/containerlab/install-containerlab.sh
```

Deploy the lab:

```bash
sudo containerlab deploy -t examples/containerlab/noc-foundry-lab.clab.yaml
```

This lab gives you a 5-node SR Linux Clos-style fabric with:

- 2 spines and 3 leaves
- ISIS underlay reachability
- NETCONF enabled on every node
- OpenConfig/gRPC enabled on every node

When you are done:

```bash
sudo containerlab destroy -t examples/containerlab/noc-foundry-lab.clab.yaml
```

## Build the binary

```bash
go build -o nocfoundry
```

## Start the local Keycloak lab

```bash
docker compose -f docker/docker-compose.keycloak.yaml up -d
./docker/keycloak-setup.sh
```

## Start NOCFoundry

```bash
./nocfoundry \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```

This quickstart uses a self-contained example tool catalog, so it does not need `--prebuilt`.

If you want to mix the bundled validation lifecycle tools into a custom catalog, use:

```bash
./nocfoundry \
  --prebuilt validation-runs \
  --tools-file examples/tools-configs/keycloak-protected-validation.yaml \
  --server-config examples/server-configs/protected-api-mcp-ui.yaml \
  --validation-config examples/validation-runtime-configs/durable-validation-sqlite.yaml \
  --ui
```

This demo enables:

- protected `/api` and `/mcp`
- browser UI login with Keycloak
- durable validation runs backed by SQLite

## Open the UI

Visit:

```text
http://127.0.0.1:5000/ui/
```

Sign in with the local Keycloak test account created by `docker/keycloak-setup.sh`.

## Run the first workflow

Start a validation run:

```bash
./nocfoundry invoke start_validation_run \
  '{"validation":"maintenance_validation","params":{"phase":"pre"},"idempotency_key":"maintenance-pre-001"}'
```

Check status:

```bash
./nocfoundry invoke validation_run_status \
  '{"run_id":"<run-id>","after_sequence":0,"limit":20}'
```

Fetch the result:

```bash
./nocfoundry invoke validation_run_result \
  '{"run_id":"<run-id>"}'
```

## Next steps

- [Server config]({{< relref "configuration/server-config.md" >}})
- [Validation runtime]({{< relref "configuration/validation-runtime.md" >}})
- [UI login with Keycloak]({{< relref "auth/ui-login-keycloak.md" >}})
- [Containerlab lab setup]({{< relref "examples/containerlab-srlinux-lab.md" >}})
