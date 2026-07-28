# Go SDK Migration Threat Model

This document is the P0-04 security baseline for the Ocean Watch Go migration. It applies to the local Plugin launcher, both Skills, the Go CLI, the Python fallback, local state and credentials, official Ocean Engine API traffic, acceptance evidence, and release assets.

## Assets And Trust Boundaries

Protected assets:

- Marketing and Qianchuan App secrets, access tokens, refresh tokens, OAuth codes, and authorization mappings.
- Advertiser, creator, product, material, project, promotion, and plan identities.
- User-approved write capability, including `--submit` and `--confirm-delete`.
- Configuration, templates, managed-account registry, authorization snapshots, caches, and run journals.
- CLI JSON and mandatory Presentation contracts consumed by the Skills.
- Official SDK source, build inputs, runtime binaries, manifests, signatures, and acceptance evidence.

Trust boundaries:

1. User conversation to Skill intent routing.
2. Skill to local launcher/CLI arguments and stdout JSON.
3. CLI to operating-system credentials and local state.
4. SDK Adapter to official allowlisted Ocean Engine hosts.
5. Optional public work metadata resolver to untrusted public-link data.
6. CI source checkout to dependency resolution, build workers, signing identity, and release assets.
7. Plugin installation to downloaded runtime manifest, cache, and execution.

## Threat Register

| ID | Threat | Impact | Required control | Verification | Owner | Gate |
| --- | --- | --- | --- | --- | --- | --- |
| TM-01 | SDK logging exposes headers or bodies | Token and business-data disclosure | Disable SDK log middleware permanently; central redactor; no raw request/response artifacts | AC-109, AC-125 | SO | G2/G5 |
| TM-02 | Marketing and Qianchuan credentials are mixed | Cross-channel data access or writes | Channel-scoped credential handles and request injection; immutable clients without default token headers | AC-109, AC-110 | AP | G2 |
| TM-03 | OAuth callback state is guessed, replayed, or changes channel | Authorization takeover | Loopback-only callback, exact state match, expiry, single-use state, channel from verified state | AC-108 | AP + SO | G2 |
| TM-04 | Redirect or configurable host sends credentials off-domain | Credential exfiltration | Compile-time host profiles, cross-host redirect rejection, no user-controlled API host | AC-109, AC-114 | SO + AO | G2 |
| TM-05 | HTTP 200 business errors are accepted as success | Incorrect state and unsafe follow-up writes | EnvelopeGuard validates HTTP, SDK error, official `code`, and required response data | AC-114 | API Owners | G2/G3 |
| TM-06 | A read retry restarts pagination | Rate amplification, incomplete or duplicate data | Persist page/cursor state and retry only the failed page; detect stalled or contradictory pagination | AC-112, AC-122 | RL + QO | G3 |
| TM-07 | A timed-out write is replayed | Duplicate projects, plans, or materials | Writes bypass generic retry; classify unknown outcome and reconcile through official reads | AC-118–AC-121 | API Owners + SO | G4 |
| TM-08 | Dry-run or confirmation boundaries are bypassed | Unauthorized official writes | Central write capability guard before credentials and SDK; advertiser-scoped process lock | AC-116 | RL + SO | G4 |
| TM-09 | Concurrent Python/Go state access corrupts files | Lost authorization, templates, or journals | Same lock path and primitive, same atomic replace protocol, crash injection, unknown-field preservation | AC-106, AC-107 | AP + RL | G1/G2 |
| TM-10 | Run ID, symlink, or output path escapes state root | Arbitrary local file read/write | Strict ID grammar, symlink rejection, resolved-root containment, explicit `--out` only | AC-106, AC-109 | RL + SO | G1 |
| TM-11 | Public work resolver supplies false ownership facts | Cross-creator or wrong-product writes | Send only public link; treat resolver as hint; revalidate creator, authorization, product, and material through official API | AC-109, AC-119 | QA-API | G4 |
| TM-12 | Model simplifies mandatory output or misroutes account intent | Wrong user decision or unnecessary API traffic | Structured Presentation contract plus model-in-the-loop semantic and response evaluation | AC-103, AC-105, AC-128 | SCO + QO | G0/G5 |
| TM-13 | Dependency or generated SDK is replaced | Compromised build/runtime | Pin module, tag commit, sums, license; isolated Adapter imports; SBOM and vulnerability gates | AC-114, AC-125 | AO + SO | G0/G5 |
| TM-14 | Launcher executes wrong, modified, or cross-version binary | Local code execution | Signed manifest, pinned trust root, product/plugin/commit identity checks, SHA-256 before atomic cache promotion | AC-124–AC-126 | RO + SO | G5 |
| TM-15 | CI or canary artifacts leak real data | Persistent credential or account disclosure | Synthetic fixtures, redacted traces, hashed object IDs, restricted retention, secret scan | AC-109, AC-125, AC-127 | QO + SO | All |
| TM-16 | Rollback rewrites user state or replays writes | Irrecoverable state or duplicate official objects | Runtime routing only; old schema remains readable; reconcile uncertain writes before rollback | AC-106, AC-118, AC-126 | RO + AP | G4/G5 |

## Security Invariants

- No credential value may enter stdout, stderr, diagnostics, URL query, state, journal, metric label, crash output, or CI artifact.
- A request may use only its channel's authorization and official host profile.
- A write Service invocation requires a validated payload and explicit current-command capability.
- An unknown write result never enters the generic retry path.
- Mandatory Presentation output is a protocol, not model-selected formatting.
- A downloaded executable is untrusted until signature, identity, and digest checks all pass.
- Security tests use synthetic data and cannot relax the production host allowlist.

## P0 Exit Evidence

G0 requires:

1. `contracts/sdk-baseline.yaml` and `contracts/state-compatibility.yaml` reviewed by AO and SO.
2. Static checks proving SDK logging is disabled in the first Adapter implementation.
3. A cross-process lock probe on macOS plus scheduled Linux and Windows jobs before G1.
4. A launcher bootstrap prototype that rejects altered manifest, signature, version, platform, and binary digest.
5. A model-evaluation case schema and baseline run against the current Plugin.

Any high-risk row without an implementation Owner, acceptance case, and rollback condition blocks G0.
