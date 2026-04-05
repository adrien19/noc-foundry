# DEVELOPER.md

This document describes development workflows for NOCFoundry.

## Prerequisites

1. Go 1.23 or later.
2. Docker for container builds and selected tests.
3. Network lab access (real devices, simulators, or network APIs) when needed.

## Local Development

1. Build binary:

```bash
go build -o nocfoundry
```

2. Show CLI flags:

```bash
go run . --help
```

3. Run server:

```bash
go run .
```

4. Verify endpoint:

```bash
curl http://127.0.0.1:5000
```

## Testing

1. Unit tests:

```bash
go test -race -v ./cmd/... ./internal/...
```

2. Targeted integration tests:

```bash
go test -race -v ./tests/<source_dir>
```

3. Lint:

```bash
golangci-lint run --fix
```

## Tool Naming Conventions

Example:

```yaml
kind: tools
name: show_interfaces
type: vendor-show-interfaces
source: my_network_source
```

### Tool Name

- Use snake_case.
- Do not include vendor or product name in the tool name.

### Tool Type

- Use kebab-case.
- Include the vendor/domain family in the type name.
- Treat type changes as breaking.

## Adding a New Source

1. Create a new directory under internal/sources/<newsource>.
2. Define Config and Source structs in <newsource>.go.
3. Implement SourceConfig and Source interfaces.
4. Register in init().
5. Add unit tests in <newsource>_test.go.

## Adding a New Tool

1. Create internal/tools/</vendor-or-domain>/<toolname>.
2. Define Config and Tool structs.
3. Implement ToolConfig and Tool interfaces.
4. Register in init().
5. Add unit tests in <toolname>_test.go.

## Adding Integration Tests

1. Add tests under tests/<source>/.
2. Cover required predefined suites and source-specific tool behavior.
3. Ensure tests clean up resources after execution.

## Documentation Workflow

1. Add source docs to docs/en/resources/sources/.
2. Add tool docs to docs/en/resources/tools/.
3. Add optional samples to docs/en/samples/<source>/.

## Hugo Docs Preview

```bash
cd .hugo
npm ci
hugo server
```

## Contribution Guidance

- Keep PRs small and scoped.
- Include test evidence for each code change.
- Use Conventional Commits for branch and commit hygiene.
- Preserve Apache-2.0 headers and modification notices for upstream-derived files.

## Compliance Expectations

- Do not add proprietary logos, trademarks, or branded art assets without clear redistribution rights.
- Replace or remove external references that are not required for NOCFoundry.
- Use one approved repository attribution form consistently for new NOCFoundry-authored files.
- Preserve required license notices and attribution when bringing third-party code into the repo.
- Record meaningful third-party code intake in the PR description and compliance checklist.
