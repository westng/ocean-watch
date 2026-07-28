# ADR-0001: Platform-Native Runtime Bootstrap

## Status

Accepted. Production remains disabled pending five-platform acceptance,
controlled canaries, independent approvals, and all required Gates.

## Context

The compatibility entry point must remain usable with Python's standard library,
but the standard library has no supported Ed25519 verifier. Shipping a hand-written
signature primitive in `run.py` would create a high-risk supply-chain boundary.
The runtime also has to validate the same release identity on macOS arm64/amd64,
Linux amd64/arm64, and Windows amd64.

## Decision

Ship a small native Go bootstrap for each target platform. It uses only Go's
standard-library `crypto/ed25519`, a build-time pinned public key, bounded HTTPS
downloads, SHA-256, private cache permissions, and atomic promotion. Python
selects the bootstrap and never accepts a trust-root or release-URL override.
The bootstrap source currently lives in `prototype/runtime-bootstrap`; `prototype`
is a historical directory name, not a statement that the candidate is still a P0
feasibility spike. P5 candidate automation builds and validates the platform
assets, while the production `run.py` path remains unwired until release approval.

The release job must produce independent Ed25519 test vectors, a reproducible
cross-build record, SBOM/provenance, and a second security review. The P0 code
contains a published RFC8032 vector and synthetic end-to-end vectors, but that is
not a security sign-off by itself.

## Consequences

- Candidate automation builds five small platform bootstrap assets; the production
  Plugin does not consume them until the native-installation Gate is approved.
- A user still needs Python during the compatibility period.
- Candidate automation covers executable packaging, signature rotation controls,
  and Windows atomic-file behavior, but real installation and rollback evidence is
  still required before production enablement.
- If platform bootstrap packaging cannot be made immutable and verifiable, the
  release plan must stop and return to a vendored audited verifier or a bundled
  runtime distribution model.

## Acceptance evidence

- `prototype/runtime-bootstrap/bootstrap/*_test.go`
- `scripts/acceptance/build_bootstrap_matrix.py`
- `artifacts/go-sdk-acceptance/p0/bootstrap/matrix.json` (CI artifact; ignored by Git)
