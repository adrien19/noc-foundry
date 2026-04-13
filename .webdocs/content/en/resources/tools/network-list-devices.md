+++
title = "network-list-devices"
linkTitle = "network-list-devices"
weight = 66
type = "docs"
description = "List all devices in the pool with labels, transports, and vendor metadata."
+++

# `network-list-devices`

`network-list-devices` lists all devices known to the server, including their
labels, available transports, and vendor/platform metadata. It merges sibling
sources (same device, different transports) into unified device entries.

## Key behavior

- read-only
- returns all devices from the device pool
- supports optional filtering by `vendor` or `role`
- merges multiple source transports per device into a single entry
- useful for discovery before running fleet operations

## Runtime parameters

- `vendor` (optional): filter devices by vendor name
- `role` (optional): filter devices by role label

## Example configuration

```yaml
kind: tools
name: list_lab_devices
type: network-list-devices
```

## Example output

Each device entry includes:

| Field | Description |
|---|---|
| `name` | Device name |
| `vendor` | Vendor identifier |
| `platform` | Platform model |
| `version` | Software version |
| `transports` | Available transport protocols (ssh, gnmi, netconf) |
| `labels` | Key-value labels from the device group |

## Related

- [`network-show-interfaces`]({{< relref "network-show-interfaces.md" >}})
- [`network-show-version`]({{< relref "network-show-version.md" >}})
- [`network-query`]({{< relref "network-query.md" >}})
