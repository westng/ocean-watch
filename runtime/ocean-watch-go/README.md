# Ocean Watch Go Runtime

This module is the only Ocean Watch business runtime. It implements the stable MCP, CLI, state, error, and Presentation contracts around the official Ocean Engine Go SDK and REST endpoints.

## Boundaries

```text
cmd/ocean-watch        production CLI and stable MCP proxy entrypoint
cmd/mcp-probe          direct Runtime or stable-proxy stdio protocol probe
cmd/build-runtime      deterministic multi-platform builder and manifest writer
internal/mcpserver     stable proxy, Runtime stdio transport, schemas, presenters, errors
internal/runtimeupdate installed-version discovery, validation, private slots, and safe cleanup
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
GOTOOLCHAIN=go1.27.0 go test ./...
GOTOOLCHAIN=go1.27.0 go vet ./...
GOTOOLCHAIN=go1.27.0 go run ./cmd/ocean-watch --help
GOTOOLCHAIN=go1.27.0 go run ./cmd/mcp-probe
GOTOOLCHAIN=go1.27.0 go run ./cmd/mcp-probe --binary ../../bin/ocean-watch-launcher --proxy-root ../..
GOTOOLCHAIN=go1.27.0 go run ./cmd/build-runtime --all
GOTOOLCHAIN=go1.27.0 go run ./cmd/build-runtime --all --verify
```

Tests use synthetic fixtures and must not read real credentials, business configuration, or online APIs.
