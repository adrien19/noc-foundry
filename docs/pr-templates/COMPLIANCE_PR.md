# Compliance PR Template

## Scope

Describe what compliance checks were performed.

## Apache Checklist

- [ ] LICENSE present and correct.
- [ ] File-level headers aligned with policy.

## Attribution

- [ ] New NOCFoundry-authored files use an approved repository attribution form consistently.
- [ ] Third-party attributions preserved where required.
- [ ] Modification notices present where required.

## Trademark / Proprietary Exclusion

- [ ] Denylist scan run.
- [ ] No prohibited assets present.

## Evidence

Commands:

- `grep -RIn "Licensed under the Apache License" .`
- `grep -RIn "logo\|trademark\|brand asset" .`
