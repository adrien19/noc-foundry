+++
title = "YANG Schemas"
linkTitle = "YANG Schemas"
weight = 24
type = "docs"
description = "Load native YANG models to enable schema-aware protocol routing and canonical data mapping."
+++

# YANG Schemas

NOCFoundry can compile native YANG models at startup and use them to generate
protocol-specific paths for gNMI and NETCONF operations. This replaces
hardcoded path tables with schema-derived paths that stay accurate across
vendor software versions.

## Why use YANG schemas

Without `--schema-dir`, NOCFoundry uses hardcoded profiles that map operations
to protocol paths. These profiles work for the specific software versions they
were written for, but YANG paths can change between releases.

With `--schema-dir`, NOCFoundry:

- compiles YANG models from the vendor's official model repository
- resolves native gNMI and NETCONF paths from the compiled schema
- merges schema-derived paths into the hardcoded profile, replacing gNMI and
  NETCONF entries while preserving CLI fallbacks
- validates all operation paths against the schema and logs warnings for
  missing or deprecated paths (version drift detection)
- selects the best schema version when multiple versions are loaded

## Directory layout

YANG models must follow this directory structure:

```text
<schema-dir>/
  <vendor>/
    <platform>/
      <version>/
        *.yang
```

For example, using the [Nokia SR Linux YANG models](https://github.com/nokia/srlinux-yang-models):

```text
yang-models/
  nokia/
    srlinux/
      v24.10/
        *.yang
      v25.10/
        *.yang
```

NOCFoundry discovers bundles by walking the directory tree. Symlinks are
resolved, so you can clone a vendor's YANG model repository and symlink the
version directories.

## Loading schemas

Pass the `--schema-dir` flag at startup:

```bash
./nocfoundry \
  --tools-file examples/tools-configs/nokia-srlinux-yang-schema.yaml \
  --schema-dir yang-models
```

At startup, NOCFoundry:

1. walks `yang-models/` and discovers `nokia/srlinux/v24.10/`, etc.
2. compiles each bundle using goyang, collecting all `.yang` files and
   resolving cross-module imports
3. for each vendor/platform, picks the best version by path resolution score
4. merges the schema-derived profile with the hardcoded fallback
5. registers the final profile for runtime use

## What schemas affect

Schemas enhance the `network-show-interfaces`, `network-show-version`, and
`network-query` tools. These tools use the query executor, which:

- looks up the device profile by vendor and platform
- selects the best available protocol path from the profile
- when schema-derived paths are present, prefers native YANG paths over
  OpenConfig for gNMI and NETCONF operations

CLI paths are always preserved from the hardcoded profile because YANG models
do not define CLI syntax.

## Canonical data mapping

When schema-derived paths are used, NOCFoundry maps the vendor-specific
response into canonical models:

- **`InterfaceState`**: Name, AdminStatus, OperStatus, Description, Speed, MTU,
  LastChange
- **`SystemVersion`**: Hostname, SoftwareVersion, ChassisType

The canonical mapper handles:

- vendor-namespaced JSON wrappers (e.g., `srl_nokia-interfaces:interface`)
- OpenConfig nested `state`/`config` containers
- enum normalization (`enable`/`inService`/`UP` → `UP`)
- unmapped vendor-specific leaves captured in `VendorExtensions`
- mapping quality metadata (`MappingExact`, `MappingDerived`, `MappingPartial`)

## Version drift detection

At startup, NOCFoundry validates all operation paths against the loaded schema
and reports:

- `PathFound` — path exists in the schema
- `PathNotFound` — path is missing (model may have changed between versions)
- `PathDeprecated` — path is marked deprecated in the YANG model

Drift warnings appear in the server log but do not block startup. The tool
continues to work using the next available protocol path.

## Supported vendors

| Vendor | Platform | Profile operations |
|---|---|---|
| Nokia | SR Linux | `get_interfaces`, `get_system_version` |
| Nokia | SR OS | `get_interfaces`, `get_system_version` |

Additional vendors and operations can be added by registering profiles and
operation mappings.

## Example tool catalog

See [`examples/tools-configs/nokia-srlinux-yang-schema.yaml`](https://github.com/adrien19/noc-foundry/blob/main/examples/tools-configs/nokia-srlinux-yang-schema.yaml)
for a complete example that uses YANG-schema-enhanced tools against an SR Linux
containerlab topology.

## Related

- [Tool catalogs]({{< relref "tool-catalogs.md" >}})
- [`network-show-interfaces`]({{< relref "../resources/tools/network-show-interfaces.md" >}})
- [`network-show-version`]({{< relref "../resources/tools/network-show-version.md" >}})
- [`network-query`]({{< relref "../resources/tools/network-query.md" >}})
- [CLI reference]({{< relref "../reference/cli.md" >}})
