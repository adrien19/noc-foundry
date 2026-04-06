# NOCFoundry Context

This file (should be same file as `CLAUDE.md` and `AGENTS.md`) provides context and guidelines for AI agents working on the NOCFoundry project. It summarizes key information from `CONTRIBUTING.md` and `DEVELOPER.md`.

## Project Overview

**NOCFoundry** is a Go-based project designed to provide Model Context Protocol (MCP) tools for network elements and network-adjacent services. It allows Large Language Models (LLMs) to interact with devices and tools safely and efficiently.

## Tech Stack

- **Language:** Go (1.23+)
- **Documentation:** Hugo (Extended Edition v0.146.0+)
- **Containerization:** Docker
- **CI/CD:** GitHub Actions
- **Linting:** `golangci-lint`

## Key Directories

- `cmd/`: Application entry points.
- `internal/sources/`: Implementations of network-oriented sources (e.g., SSH, gNMI, HTTP).
- `internal/tools/`: Implementations of vendor and network tools for each source.
- `tests/`: Integration tests.
- `docs/`: Project documentation (Hugo site).

## Development Workflow

### Prerequisites

- Go 1.23 or later.
- Docker (for building container images and running some tests).
- Access to necessary Google Cloud resources for integration testing (if applicable).

### Building and Running

1. **Build Binary:** `go build -o nocfoundry`
2. **Run Server:** `go run .` (Listens on port 5000 by default)
3. **Run with Help:** `go run . --help`
4. **Test Endpoint:** `curl http://127.0.0.1:5000`

### Testing

- **Unit Tests:** `go test -race -v ./cmd/... ./internal/...`
- **Integration Tests:**
  - Run specific source tests: `go test -race -v ./tests/<source_dir>`
  - Example: `go test -race -v ./tests/nokia`
  - Add new sources to `.ci/integration.<vendor>.yaml`
- **Linting:** `golangci-lint run --fix`

## Developing Documentation

### Prerequisites

- Hugo (Extended Edition v0.146.0+)
- Node.js (for `npm ci`)

### Running Local Server

1. Navigate to `.webdocs` directory: `cd .webdocs`
2. Install dependencies: `npm ci`
3. Start server: `hugo server`

### Versioning Workflows

1. **Deploy In-development docs**: Merges to main -> `/dev/`.
2. **Deploy Versioned Docs**: New Release -> `/<version>/` and root.
3. **Deploy Previous Version Docs**: Manual workflow for older versions.

## Coding Conventions

### Tool Naming

- **Tool Name:** `snake_case` (e.g., `show_interfaces`, `run_query`).
  - Do *not* include the vendor name (e.g., avoid `nokia_show_interfaces`).
- **Tool Type:** `kebab-case` (e.g., `nokia-show-interfaces`).
  - *Must* include the vendor name.

### Branching and Commits

- **Branch Naming:** `feat/`, `fix/`, `docs/`, `chore/` (e.g., `feat/add-nokia-hardware`).
- **Commit Messages:** [Conventional Commits](https://www.conventionalcommits.org/) format.
  - Format: `<type>(<scope>): <description>`
  - Example: `feat(source/ssh): add new connection option`
  - Types: `feat`, `fix`, `docs`, `chore`, `test`, `ci`, `refactor`, `revert`, `style`.

## Adding New Features

### Adding a New Data Source

1. Create a new directory: `internal/sources/<newprotocol>`.
2. Define `Config` and `Source` structs in `internal/sources/<newprotocol>/<newprotocol>.go`.
3. Implement `SourceConfig` interface (`SourceConfigType`, `Initialize`).
4. Implement `Source` interface (`SourceType`).
5. Implement `init()` to register the source.
6. Add unit tests in `internal/sources/<newprotocol>/<newprotocol>_test.go`.

### Adding a New Tool

1. Create a new directory: `internal/tools/<vendor>/<toolname>`.

- If it's a common tool accross all vendors, create a new directory: `internal/tools/common/<toolname>`.

2. Define `Config` and `Tool` structs.
2. Implement `ToolConfig` interface (`ToolConfigType`, `Initialize`).
3. Implement `Tool` interface (`Invoke`, `ParseParams`, `Manifest`, `McpManifest`, `Authorized`).
4. Implement `init()` to register the tool.
5. Add unit tests.

### Adding Documentation

- Add source documentation to `docs/en/resources/sources/`.
- Add tool documentation to `docs/en/resources/tools/`.
