# Go SDK Runtime Release RFC

This document is the P0-05 release and rollback decision for the Go runtime. P5 automation now builds, verifies, aggregates evidence for, seals, and publishes the signed Release candidate described here; the current [release guide](releasing.md) records the executable process. Production routing and the final Marketplace package path remain gated: the repository policy is disabled and every production route stays on Python until G1–G5 and independent approvals pass.

## Decision

Ocean Watch will publish five immutable Go runtime assets from one source Tag. The installed Plugin keeps a Python 3.9+ standard-library entry point during the compatibility period, but Python does not implement cryptographic verification. It selects a bundled, platform-native Go bootstrap. That bootstrap downloads only a signed runtime manifest for the Plugin's product version, validates the complete release identity, downloads the selected asset, verifies SHA-256, and atomically promotes it into a user-private cache.

The P0 feasibility choice is Go's standard-library `crypto/ed25519`, built as a small native bootstrap for each supported platform. The trust root is injected at release build time and is not a CLI option. Python only invokes the bootstrap and remains the production route until G5 and the independent approvals pass. The decision and the boundaries are recorded in [ADR-0001](adr/0001-platform-bootstrap.md). A hand-written cryptographic primitive remains prohibited.

## Version Identity

| Field | Meaning | Rule |
| --- | --- | --- |
| `product_version` | Runtime and source release version | SemVer core; equals `pyproject.toml`, `ocean_watch.__version__`, Plugin version before `+`, and Tag `v<version>` |
| `plugin_version` | Full Codex Plugin version | Includes at most one `+codex.*` cachebuster; bound in manifest |
| `git_commit` | Source identity | Full 40-character commit referenced by the Tag |
| `sdk_version` | Official SDK dependency | Exact `v1.1.92` for this migration baseline |
| `manifest_version` | Manifest schema | Integer; unknown newer versions fail closed |

After runtime assets are enabled, a cachebuster cannot be published independently. Any Plugin or launcher content change requires a new product version, Tag, manifest, and asset set.

## Platform Assets

| Platform | Asset |
| --- | --- |
| macOS arm64 | `ocean-watch_darwin_arm64` |
| macOS amd64 | `ocean-watch_darwin_amd64` |
| Linux amd64 | `ocean-watch_linux_amd64` |
| Linux arm64 | `ocean-watch_linux_arm64` |
| Windows amd64 | `ocean-watch_windows_amd64.exe` |

Each asset is built with the pinned Go toolchain, `CGO_ENABLED=0`, `-trimpath`, and embedded product version/commit metadata. Release jobs also produce an SBOM, provenance, `checksums.json`, signed runtime manifest, and detached signature.

## Evidence And Publication Chain

The release identity is established before publication and cannot be reconstructed by the Tag job:

1. `g5-evidence.yml` builds the formal candidate twice from the protected current `main`, using the release signing Secret only in the candidate-build job. It verifies the approved public trust root, candidate reproducibility, five native platforms, source quality, security, and deterministic acceptance evidence.
2. Formal evidence, model evaluation, real canary, Marketplace installation, previous-version rollback, and rollout observation must be six distinct successful workflow runs for the same repository and commit. The G5 preparation job verifies each run through the GitHub API and downloads only explicitly named artifacts.
3. `build_final_summary.py --require-ready` binds every indexed evidence file, the immutable candidate identity, and the six source runs. Missing, failed, blocking, not-run, or expired-exception results prevent a ready summary.
4. MT, AO, QO, SO, RO, and SCO approve the exact canonical summary after it is generated. Their externally assembled record is consumed only through the protected `g5-independent-signoff` environment; workflows cannot manufacture approval decisions. SO and RO must be different approvers.
5. The seal job binds the original candidate, evidence tree, summary, source-run manifest, restricted signoff, and prepare/signoff producer metadata. `seal.json` records deterministic hashes and provenance sufficient to reverify the sealed artifact.
6. `tag.yml` accepts only one successful seal run ID. Its read-only and write-enabled jobs independently download and verify that exact seal. It never rebuilds the candidate and has no access to the signing Secret. Only the protected publish job receives `contents: write`.

The public Release contains the original 19 candidate files plus `g5-seal.json`. The complete evidence tree and signoff identities remain in the restricted sealed Actions artifact; the public seal summary exposes hashes and workflow provenance, not approver identities.

## Runtime Manifest

The canonical JSON uses sorted keys and compact separators before signing. Minimum fields:

```json
{
  "manifest_version": 1,
  "product_version": "0.0.0",
  "plugin_version": "0.0.0+codex.EXAMPLE",
  "git_commit": "FULL_COMMIT_SHA",
  "sdk_version": "v1.1.92",
  "tag": "v0.0.0",
  "routes": {
    "accounts list": "go"
  },
  "assets": {
    "darwin-amd64": {
      "name": "ocean-watch_darwin_amd64",
      "sha256": "64_HEX_CHARACTERS",
      "size": 1
    }
  }
}
```

Unknown top-level fields are tolerated only within the same known manifest version. Missing identity or platform fields fail closed. Asset URLs are derived from the fixed repository and Tag, never accepted from the manifest.

## Launcher State Machine

1. Read full Plugin version and derive `product_version` in the Python entry point.
2. Select the bundled bootstrap for the current platform; the bootstrap loads the pinned trust root and fixed repository identity.
3. Reuse a cached, signed manifest and asset when both validate; otherwise resolve the immutable `v<product_version>` manifest and signature, never `latest`.
4. Verify the Ed25519 signature before parsing untrusted fields beyond the bounded envelope.
5. Validate product/plugin version, Tag, commit, SDK version, route, platform, asset name, size, and digest format.
6. Otherwise download to bounded temporary files, verify size and SHA-256, set permissions, and atomically rename.
7. Execute without shell interpolation and forward argv/stdin/stdout/stderr and signals.

Cache root:

```text
$CODEX_HOME/ocean-watch/runtime/<product_version>/<goos>-<goarch>/
```

The launcher must not store Token, advertiser IDs, command payloads, or official responses in this cache.

## Offline And Failure Behavior

- Valid cached runtime: execute offline.
- No cache and no network: return one structured installation error with the exact product/platform identity.
- Signature, identity, size, or digest mismatch: delete the temporary download, retain any known-good cache, and return a security error.
- Unsupported platform: fail before network access.
- Go command route unavailable during migration: use the bundled Python business fallback only when the immutable route manifest says `python`.

## Rollback

- R1/R2 command or domain rollback ships a new product version with an immutable route manifest selecting the Python fallback for affected commands.
- R3 publishes a new patch version built from the last approved source or reverted code; it never mutates an existing Release asset.
- Existing user state is not downgraded, rewritten, or deleted by launcher rollback.
- A possibly applied write is reconciled before changing routes; rerunning it is not a rollback mechanism.

## Bootstrap Proof And Remaining Distribution Gate

The bootstrap in `prototype/runtime-bootstrap` must pass synthetic tests for valid release, altered manifest, wrong key, malformed signature, wrong product/plugin version, wrong commit, wrong platform, path-like asset name, oversized file, digest mismatch, interrupted download, concurrent launch, damaged cache, and offline cache reuse. Both `run.py` entry points now use the standard-library launcher; source Marketplace installs keep its runtime policy disabled, while signed candidate ZIPs contain the five bootstraps and an enabled policy whose route manifest still selects Python. Local and synthetic proof does not grant G5: the formal five-platform run, final Marketplace packaging, real canary, previous-version rollback, rollout observation, exact-summary signoff, and sealed publication must all complete before this becomes the default distribution path.
