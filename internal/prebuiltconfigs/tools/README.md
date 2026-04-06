# Prebuilt Configurations

This directory contains bundled prebuilt YAML catalogs for NOCFoundry.

NOCFoundry intentionally ships a small curated prebuilt set focused on
generic operational capabilities instead of broad vendor- or database-specific
bundles.

Current bundled prebuilt catalogs:

- `validation-runs`
  Lifecycle tools for starting, polling, retrieving, and cancelling
  validation runs.

Richer end-to-end workflows, vendor lab setups, and local Keycloak examples
should live in `examples/`, not in the bundled prebuilt catalog set.
