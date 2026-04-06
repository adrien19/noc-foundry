# Attribution Gate (Release Checklist)

This checklist is mandatory for release-candidate and release PRs.

## License Baseline

- [ ] `LICENSE` exists at repository root and is Apache License 2.0 text.
- [ ] No file in repository claims an incompatible license.

## File Header Rules

- [ ] New NOCFoundry-authored files use an approved project attribution header that accurately reflects repository ownership.
- [ ] Third-party open source files retain required original attribution and license notices.
- [ ] Modified third-party files retain required original notices and add modification attribution when needed.

## Trademark and Proprietary Asset Exclusion

- [ ] No third-party logos, brand marks, or proprietary visuals were ported.
- [ ] Denylist scan performed for trademarked assets.

## Third-Party Intake

- [ ] PR includes source project, license, and commit or release reference for imported code.
- [ ] Added code and assets are compatible with Apache-2.0 distribution in this repository.

## Verification

- [ ] Build/test commands executed and results captured in PR.
- [ ] Any exceptions are listed with owner approval.

## Suggested Header Patterns

### New NOCFoundry-authored file (Apache-2.0)

Use one approved repository form consistently, for example:

- `Copyright 2026 Adrien Ndikumana`
- `Copyright 2026 NOCFoundry Contributors`
- `Copyright 2026 Adrien Ndikumana and NOCFoundry Contributors`

```text
// Copyright 2026 Adrien Ndikumana and NOCFoundry Contributors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
```

### Modified third-party Apache-2.0 file

```text
/*
Copyright <original owner>
Modifications Copyright 2026 Adrien Ndikumana and NOCFoundry Contributors

Licensed under the Apache License, Version 2.0
*/
```
