+++
title = "network-show-interfaces"
linkTitle = "network-show-interfaces"
weight = 63
type = "docs"
description = "Vendor-agnostic interface status and counters retrieval."
+++

# `network-show-interfaces`

`network-show-interfaces` retrieves interface status and counters from any
network device that has a registered profile. It delegates to the query
executor, which routes through the best available protocol and uses the
schema-driven canonical mapper for normalization.

## What it does

- executes the `get_interfaces` operation from the device profile
- routes through gNMI, NETCONF, or CLI based on source capabilities
- returns canonical `InterfaceState` records normalized across vendors
- supports fleet operations via label selectors

## Canonical output fields

Each interface record includes:

| Field | Description |
|---|---|
| `Name` | Interface name |
| `AdminStatus` | Administrative state (`UP` / `DOWN`) |
| `OperStatus` | Operational state (`UP` / `DOWN`) |
| `Description` | Interface description |
| `Speed` | Link speed |
| `MTU` | Maximum transmission unit |
| `LastChange` | Timestamp of last state transition |

Unmapped vendor-specific fields are captured in `VendorExtensions`.

## Key behavior

- read-only by default
- supports single `source` or fleet `sourceSelector`
- optional `transforms` for per-tool jq post-processing
- automatic protocol selection based on source capabilities
- YANG-schema-aware when `--schema-dir` is set

## Runtime parameters

- `device` (optional, fleet mode): target a specific device in the selector

## Example configurations

### Single source

```yaml
kind: tools
name: show_spine1_interfaces
type: network-show-interfaces
source: nokia-yang-lab/spine-1/gnmi
```

### Fleet mode

```yaml
kind: tools
name: show_all_spine_interfaces
type: network-show-interfaces
sourceSelector:
  matchLabels:
    role: spine
  maxConcurrency: 4
```

### With jq transform

```yaml
kind: tools
name: show_spine1_interfaces_filtered
type: network-show-interfaces
source: nokia-yang-lab/spine-1/gnmi
transforms:
  get_interfaces:
    jq: '[.[] | select(.OperStatus == "UP")]'
```

## Protocol routing

The query executor selects the best available protocol for each source:

1. NETCONF native YANG paths (if source supports NETCONF + native YANG)
2. gNMI native YANG paths (if source supports gNMI + native YANG)
3. NETCONF OpenConfig paths
4. gNMI OpenConfig paths
5. CLI fallback

When `--schema-dir` is set, native YANG paths are resolved from compiled
schemas and override hardcoded defaults for gNMI and NETCONF.

## Related

- [`network-show-version`]({{< relref "network-show-version.md" >}})
- [`network-query`]({{< relref "network-query.md" >}})
- [`network-show`]({{< relref "network-show.md" >}})
- [YANG schemas]({{< relref "../../configuration/yang-schemas.md" >}})
