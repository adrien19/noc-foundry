+++
title = "network-show"
linkTitle = "network-show"
weight = 62
type = "docs"
description = "Vendor-agnostic ad-hoc CLI command execution on network devices."
+++

# `network-show`

`network-show` is a vendor-agnostic tool for running ad-hoc or predefined CLI
commands on network devices. It replaces the previous vendor-specific show tools
with a single type that works across any device reachable through an SSH source.

## Modes

### Ad-hoc mode

The operator or agent supplies the `command` parameter at invocation time.
The tool validates the command against a read-only safety check
(rejects `configure`, `delete`, `set`, `commit`, `rollback`, `reboot`,
`shutdown`, etc.) and executes it through the source.

### Predefined-command mode

The tool catalog defines a fixed `command` template with `{paramName}`
placeholders. At invocation time, the caller supplies values for each
placeholder. The `command` parameter is not exposed to the caller.

## Key behavior

- read-only by default — annotated as `readOnlyHint: true`
- supports a single `source` or a fleet-oriented `sourceSelector`
- optional runtime `jq` parameter for post-processing
- optional static `transforms` for per-tool jq transforms
- safety validation rejects dangerous CLI commands
- parameter values are sanitized against a character allowlist

## Runtime parameters

- `command` (ad-hoc mode only): the CLI command to run
- `jq` (optional): runtime jq expression applied to the output
- `device` (optional, fleet mode): target a specific device in the selector results
- custom params defined by `extraParams` in predefined-command mode

## Example configurations

### Ad-hoc mode with a single source

```yaml
kind: tools
name: show_spine1_adhoc
type: network-show
source: nokia-yang-lab/spine-1/ssh
```

### Predefined command with parameters

```yaml
kind: tools
name: show_interface_detail
type: network-show
source: nokia-yang-lab/spine-1/ssh
description: "Show interface status for a specific interface"
command: "show interface {interface}"
extraParams:
  - name: interface
    type: string
    description: "Interface name (e.g., ethernet-1/1)"
```

### Fleet mode with label selector

```yaml
kind: tools
name: show_all_spines
type: network-show
sourceSelector:
  matchLabels:
    role: spine
  maxConcurrency: 4
```

## Transport priority

When using `sourceSelector`, if a device is reachable through multiple
transports, the tool selects the best one:

1. gNMI (priority 0)
2. NETCONF (priority 1)
3. SSH (priority 2)

## Related

- [`network-show-interfaces`]({{< relref "network-show-interfaces.md" >}})
- [`network-show-version`]({{< relref "network-show-version.md" >}})
- [`network-query`]({{< relref "network-query.md" >}})
