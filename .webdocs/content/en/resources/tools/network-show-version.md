+++
title = "network-show-version"
linkTitle = "network-show-version"
weight = 64
type = "docs"
description = "Vendor-agnostic system version and chassis information retrieval."
+++

# `network-show-version`

`network-show-version` retrieves system version information from any network
device that has a registered profile. It returns normalized hostname, software
version, and chassis type through the best available protocol.

## Canonical output fields

| Field | Description |
|---|---|
| `Hostname` | Device hostname |
| `SoftwareVersion` | Running software version string |
| `ChassisType` | Hardware chassis / platform type |

Unmapped vendor-specific fields are captured in `VendorExtensions`.

## Key behavior

- executes the `get_system_version` operation from the device profile
- read-only by default
- supports single `source` or fleet `sourceSelector`
- automatic protocol selection based on source capabilities
- optional `transforms` for per-tool jq post-processing
- YANG-schema-aware when `--schema-dir` is set

## Runtime parameters

- `device` (optional, fleet mode): target a specific device in the selector

## Example configurations

### Single source

```yaml
kind: tools
name: show_spine1_version
type: network-show-version
source: nokia-yang-lab/spine-1/gnmi
```

### Fleet mode

```yaml
kind: tools
name: show_all_versions
type: network-show-version
sourceSelector:
  matchLabels:
    role: spine
  maxConcurrency: 4
```

## Related

- [`network-show-interfaces`]({{< relref "network-show-interfaces.md" >}})
- [`network-query`]({{< relref "network-query.md" >}})
- [`network-show`]({{< relref "network-show.md" >}})
- [YANG schemas]({{< relref "../../configuration/yang-schemas.md" >}})
