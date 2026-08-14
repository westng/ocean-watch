# Ocean Watch Go Runtime

This module is the only Ocean Watch business runtime. It implements the stable MCP, CLI, state, error, and Presentation contracts around the official Ocean Engine Go SDK and REST endpoints.

## Boundaries

```text
cmd/ocean-watch        production CLI
cmd/mcp-probe          stdio MCP protocol probe
cmd/build-runtime      deterministic multi-platform builder
internal/mcpserver     stdio transport, schemas, presenters, stable errors
internal/cli           parsing, routing, envelopes, Presentation
internal/application   use cases, transactions, orchestration
internal/domain        business types and rules
internal/ports         official API, state, credential, metadata contracts
internal/adapters      official SDK, filesystem, credentials, OAuth callback, F2
internal/platform      pagination, retry, rate limiting, request accounting
```

Generated SDK types stay inside `internal/adapters/oceanengine`. Application and domain packages depend on ports, never directly on generated SDK or CLI packages.

Python discovery under `internal/adapters/python` exists solely to run pinned F2 for public Douyin work metadata. It is not a business runtime or fallback path.

## Development

```bash
GOTOOLCHAIN=go1.26.5 go test ./...
GOTOOLCHAIN=go1.26.5 go vet ./...
GOTOOLCHAIN=go1.26.5 go run ./cmd/ocean-watch --help
GOTOOLCHAIN=go1.26.5 go run ./cmd/mcp-probe
GOTOOLCHAIN=go1.26.5 go run ./cmd/build-runtime --all
GOTOOLCHAIN=go1.26.5 go run ./cmd/build-runtime --all --verify
```

Tests use synthetic fixtures and must not read real credentials, business configuration, or online APIs.
