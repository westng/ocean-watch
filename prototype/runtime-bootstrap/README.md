# Ocean Watch Native Runtime Bootstrap Candidate

This isolated Go module is the P5 native bootstrap candidate. It verifies and starts a platform-specific Ocean Watch runtime without moving cryptography or trust decisions into the Python compatibility launcher.

## Current status

As of 2026-07-28, candidate build, signing, checksum, evidence aggregation, signoff, and seal automation exist. Production routing is still disabled: Marketplace installs continue through the repository Tag and execute Python until five-platform native acceptance, real canaries, independent approvals, and all required Gates are complete.

The directory name `prototype/` is historical. This module is beyond the P0 feasibility stage, but it remains a non-production candidate until the release Gate changes the signed routing policy.

## Trust boundary

The bootstrap uses the Go standard library and `crypto/ed25519`. It:

- verifies the detached manifest signature before decoding trusted fields;
- validates product version, full Plugin version, Git commit, SDK version, Tag, route, platform, asset name, size, and SHA-256;
- writes a verified binary into the cache only through atomic rename;
- rehashes cached binaries on every launch;
- rejects unset trust roots and exposes no release URL or trust-root override.

Build identity and the public verification key are injected by the protected release workflow. Ordinary CI uses a public test identity and cannot create a releasable candidate.

## Development

From this module:

```bash
GOTOOLCHAIN=go1.26.5 go test ./...
```

Candidate packaging and native consumption are driven from the repository-level acceptance and release scripts. A locally built or successfully tested bootstrap must never be placed into the production Plugin path manually.
