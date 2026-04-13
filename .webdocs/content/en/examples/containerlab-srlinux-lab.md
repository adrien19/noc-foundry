+++
title = "Containerlab SR Linux Lab"
linkTitle = "Containerlab SR Linux Lab"
weight = 82
type = "docs"
description = "Deploy a local SR Linux fabric with containerlab for realistic NOCFoundry testing."
+++

# Containerlab SR Linux Lab

Use this example when you want a realistic local network topology for NOCFoundry
development and validation testing.

Main files:

- [`examples/containerlab/noc-foundry-lab.clab.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/containerlab/noc-foundry-lab.clab.yaml)
- [`examples/containerlab/install-containerlab.sh`](https://github.com/adrien19/noc-foundry/blob/main/examples/containerlab/install-containerlab.sh)
- [`examples/containerlab/configs/noc-foundry-lab/`](https://github.com/adrien19/noc-foundry/tree/main/examples/containerlab/configs/noc-foundry-lab)

## Topology

This lab deploys:

- 2 SR Linux spines: `srl-b`, `srl-c`
- 3 SR Linux leaves: `srl-a`, `srl-d`, `srl-e`

Each node is configured with:

- ISIS on the underlay links
- a loopback address on `system0`
- NETCONF enabled on the management plane
- native SR Linux gNMI enabled on the management plane

The lab also uses a dedicated management network:

- network name: `noc-foundry-mgmt`
- subnet: `172.31.250.0/24`
- default node management IPs:
  - `srl-a`: `172.31.250.11`
  - `srl-b`: `172.31.250.12`
  - `srl-c`: `172.31.250.13`
  - `srl-d`: `172.31.250.14`
  - `srl-e`: `172.31.250.15`

The shipped SR Linux tool catalog in
[`examples/tools-configs/nokia-srlinux-lab.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/nokia-srlinux-lab.yaml)
uses those management IPs by default.

## Install containerlab

NOCFoundry does not install containerlab by default in the devcontainer.
Install it only when you want the topology:

```bash
./examples/containerlab/install-containerlab.sh
```

## Deploy the lab

```bash
sudo containerlab deploy -t examples/containerlab/noc-foundry-lab.clab.yaml
```

If `172.31.250.0/24` conflicts with local networking or a VPN, change the
management subnet in the topology and override the matching `*_HOST`
environment variables when you start NOCFoundry.

## Destroy the lab

```bash
sudo containerlab destroy -t examples/containerlab/noc-foundry-lab.clab.yaml
```

## Using with YANG schemas

To test YANG-schema-enhanced tools against this lab, clone the
[Nokia SR Linux YANG models](https://github.com/nokia/srlinux-yang-models) and
start NOCFoundry with `--schema-dir`:

```bash
./nocfoundry \
  --tools-file examples/tools-configs/nokia-srlinux-yang-schema.yaml \
  --schema-dir yang-models
```

This enables schema-derived gNMI and NETCONF paths for `network-show-interfaces`,
`network-show-version`, and `network-query` tools. See
[YANG schemas]({{< relref "../configuration/yang-schemas.md" >}}) for the
required directory layout.

## When to use this lab

Use this topology when you want to test:

- multi-node reachability and path diversity
- SR Linux source connectivity
- NETCONF and OpenConfig/gRPC access against multiple nodes
- YANG-schema-aware protocol routing with native YANG models
- validation runs against a more realistic network fabric
