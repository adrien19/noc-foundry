+++
title = "Validate"
linkTitle = "Validate"
weight = 61
type = "docs"
description = "Read-only, multi-step validation workflows for network devices and blast-radius checks."
+++

# `validate`

`validate` is a read-only validation tool for network devices and blast-radius checks. It collects evidence from one or more devices, evaluates configured assertions, and returns structured pass/fail results for a selected phase such as `pre`, `during`, or `post`.

This tool is intentionally not a change engine. Agents and operators should use it as a deterministic validation primitive inside a larger maintenance workflow.

## What it is good for

- pre-change readiness validation
- post-change verification
- blast-radius checks across multiple devices
- structured result collection for async validation runs

## Key behavior

- supports either a single `source` or a fleet-oriented `sourceSelector`
- runs ordered phases made up of `collect` and `assert` steps
- uses protocol-aware transport selection for network retrieval
- returns structured evidence, step status, and overall validation outcomes
- integrates with durable validation runs through the `validation_run_*` lifecycle tools

## Runtime parameters

- `phase`: required when more than one phase is defined
- `device`: optional when `sourceSelector` is used and you want to narrow execution

## Example operator flow

1. run the `pre` phase before maintenance
2. perform the network change outside the tool
3. run the `post` phase
4. compare results and decide whether rollback is required

## Example configuration

```yaml
kind: tools
name: maintenance_validation
type: validate
authRequired:
  - noc-keycloak
sourceSelector:
  matchLabels:
    validation_demo: "true"
phases:
  - name: pre
    steps:
      - name: collect_control_version
        collect:
          into: control_versions
          targets: ["control_plane"]
          operation: get_system_version
      - name: assert_versions
        assert:
          name: expected_version
          from: ["control_versions"]
          scope: per_record
          expr: '.payload.software_version == "23.10.R1"'
```

## Related examples

- [`examples/tools-configs/keycloak-protected-validation.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/keycloak-protected-validation.yaml)
- [Validation runs]({{< relref "../../workflows/validation-runs.md" >}})
