+++
title = "network-query"
linkTitle = "network-query"
weight = 65
type = "docs"
description = "Generic profile-driven operation executor for any registered device operation."
+++

# `network-query`

`network-query` is a generic operation executor. Instead of being limited to a
single operation like `network-show-interfaces`, it accepts an `operation`
parameter at runtime and executes any operation defined in the device profile.

## When to use it

Use `network-query` when you want a single tool entry that can run different
operations on demand, rather than configuring a separate tool for each
operation.

Use the specialized tools (`network-show-interfaces`, `network-show-version`)
when you want tighter control over what an agent can invoke.

## Key behavior

- validates the requested operation exists in the device profile
- routes through the best available protocol based on source capabilities
- supports single `source` or fleet `sourceSelector`
- optional `transforms` for per-operation jq post-processing
- returns canonical models when the operation has a canonical mapping
- read-only by default

## Runtime parameters

- `operation` (required): the operation to execute (e.g., `get_interfaces`,
  `get_system_version`)
- `device` (optional, fleet mode): target a specific device in the selector

## Example configurations

### Single source

```yaml
kind: tools
name: query_spine1
type: network-query
source: nokia-yang-lab/spine-1/gnmi
```

### Fleet mode with transforms

```yaml
kind: tools
name: query_all_spines
type: network-query
sourceSelector:
  matchLabels:
    role: spine
  maxConcurrency: 4
transforms:
  get_interfaces:
    jq: '[.[] | select(.OperStatus == "UP")]'
```

## Supported operations

Operations are defined in device profiles. Built-in profiles include:

| Operation | Description |
|---|---|
| `get_interfaces` | Retrieve interface status and counters |
| `get_system_version` | Retrieve hostname, software version, and chassis type |

Custom operations can be added by registering new profiles.

## Related

- [`network-show-interfaces`]({{< relref "network-show-interfaces.md" >}})
- [`network-show-version`]({{< relref "network-show-version.md" >}})
- [`network-show`]({{< relref "network-show.md" >}})
- [YANG schemas]({{< relref "../../configuration/yang-schemas.md" >}})
