# NOCFoundry Public Docs

This directory contains the public Hugo documentation site for NOCFoundry.

## Local development

```bash
cd .webdocs
go mod download
npm ci
hugo server
```

## Deployment model

- `main` publishes to `https://docs.nocfoundry.dev/dev/`
- release tags publish to `https://docs.nocfoundry.dev/vX.Y.Z/`
- the newest release is mirrored to `https://docs.nocfoundry.dev/latest/`

The GitHub Actions workflow preserves deployed versions on the `gh-pages` branch.

