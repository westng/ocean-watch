# Ocean Watch Go Shadow Runtime

This module is the isolated Go runtime candidate for the Ocean Watch migration. It is a modular monolith built around the official Ocean Engine Go SDK and the repository's stable CLI, state, error, and Presentation contracts.

## Current status

As of 2026-07-28:

- The repository production policy is disabled, so installed users still execute the Python runtime.
- Most P1-P4 business paths have Go implementations and automated Shadow coverage.
- The production route manifest keeps every command on Python.
- The development manifest enables only wired local handlers; tests use explicit Shadow manifests for implemented network and write handlers.
- `auth set-app/authorize/status/refresh/sync-accounts/mappings` and `qc-materials inspect-work` still lack Go CLI handlers.
- Real canaries, independent Gate approvals, and release acceptance remain outstanding.

The directory name `prototype/` is historical and retained to avoid a migration-only module move. It does not mean this module is still a feasibility spike, and it does not grant production status.

## Boundaries

```text
cmd/ocean-watch        stable candidate CLI
internal/cli           parsing, routing, envelopes, Presentation
internal/application   use cases, transactions, and orchestration
internal/domain        channel-independent business types and rules
internal/ports         official API, state, credential, and metadata contracts
internal/adapters      official SDK, filesystem, credential, browser, and fallback adapters
internal/platform      pagination, retry, rate limiting, and request budgets
internal/contractrunner Python/Go compatibility evidence
```

Generated SDK types stay inside `internal/adapters/oceanengine`. Application and domain packages depend on ports, never directly on the generated SDK or CLI.

## Development

From this module:

```bash
GOTOOLCHAIN=go1.26.5 go test ./...
GOTOOLCHAIN=go1.26.5 go run ./cmd/ocean-watch accounts list --config /path/to/config.json
```

Set `OCEAN_WATCH_PYTHON_ENTRYPOINT` only when testing an explicit Python fallback from a checkout whose working directory is not the repository root.

Run contract acceptance from the repository root:

```bash
scripts/acceptance/run.sh --suite contracts
```

The contract suite uses synthetic fixtures, blocks proxy-based network access, compares output, exit status, Presentation, and filesystem effects, and scans evidence for credentials. Local success is Shadow evidence only; it does not satisfy a production Gate.
