<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">Ocean Engine delivery and monitoring for Codex</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="https://github.com/westng/ocean-watch/actions/workflows/ci.yml"><img src="https://github.com/westng/ocean-watch/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/westng/ocean-watch/tags"><img src="https://img.shields.io/github/v/tag/westng/ocean-watch?sort=semver" alt="Git Tag"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

[中文](README.md) | English

Ocean Watch uses official Ocean Engine APIs for OAuth, responsible accounts, templates, materials, plans, reports, and delivery analysis. Its two Skills share one bundled Go CLI while keeping channel credentials, authorization, accounts, templates, and creation transactions strictly isolated.

| Skill | Channel | Capabilities |
| --- | --- | --- |
| `ads-plan-monitor` | Ocean Engine Marketing | OAuth, responsible accounts, uploaded/creator materials, templates, plans, reports, strategy |
| `qc-plan-monitor` | Qianchuan | OAuth, responsible accounts, product/live templates, creator works, all-domain plans, and all-domain/Multiplication reports |

Every online create, append, delete, or setting update is dry-run by default. The CLI adds `--submit` only after explicit user confirmation.

## Current Implementation

The Go cutover is complete; Ocean Watch is no longer in a dual-runtime or Shadow migration stage. The repository and Plugin retain one advertising-business implementation: the bundled Go CLI owns OAuth, authorized-advertiser synchronization, responsible accounts, templates, materials, plans, reports, local state, and write reconciliation. The former Python business package, Go prototype, Shadow routing, runtime selection, business fallback, MCP compatibility entry points, and migration Gate/Bootstrap assets are no longer distributed.

Python is not a second business runtime. It only launches pinned F2 `0.0.1.7` for public Douyin metadata used by Qianchuan work-link flows. F2 output is a targeting hint; official Qianchuan APIs under the requested advertiser still determine creator authorization, ownership, product matching, and deliverability.

## Runtime

```text
Codex → Skill → run/run.cmd → bundled Go CLI → official Ocean Engine API
                                      └→ Python 3.10+ → F2 0.0.1.7
                                         Douyin public metadata only
```

- Advertising business logic has one Go runtime, with no runtime selector or business fallback.
- The Plugin ships binaries for macOS Intel and Apple Silicon, Linux x86_64 and ARM64, and Windows x86_64. Users do not need a Go toolchain.
- Python runs only for Qianchuan work-link metadata through pinned F2; it does not own OAuth, accounts, templates, plans, or reports.
- Official Qianchuan APIs always recheck authorization, ownership, product matching, and deliverability.

See [Architecture](docs/architecture.md) for implementation boundaries.

## Install

Requires Codex CLI `0.144.1+`. Qianchuan work-link flows also require Python `3.10+` and F2 `0.0.1.7`.

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

Start a new Codex task after installation or upgrade. Describe the desired outcome naturally; the Skills select the correct channel, ask for missing information, and preview every online write.

Examples:

- “Initialize Ocean Engine Marketing and guide me through authorization.”
- “Make the advertiser list for the current authorized user match the latest official access.”
- “Show today's spend for the Marketing and Qianchuan accounts I manage.”
- “Use these Douyin works with my Qianchuan product template, and preflight first.”

Manual first-use checks:

```bash
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run setup init --home-config
```

Use `run.cmd` on Windows. See the Chinese [getting-started guide](docs/getting-started.md) for the complete flow.

### Versions and releases

Repository version Tags are the Codex Marketplace installation source, and a GitHub Release is created from the matching Chinese Changelog section. Release automation validates a fixed commit, tests, and the five bundled platform binaries; it does not edit repository files or push a version commit back to `main`. Ordinary changes remain under `CHANGELOG.md`'s Unreleased section and never trigger an automatic version bump or Release.

To pin a version, register the Marketplace at its Tag:

```bash
codex plugin marketplace add westng/ocean-watch --ref vX.Y.Z
codex plugin add ocean-watch@ocean-watch
```

See the [release guide](docs/releasing.md) for the maintainer workflow.

## Everyday Q&A

These questions are examples, not fixed commands. Conversational wording, abbreviations, typos, and contextual follow-ups are routed by intended outcome.

### First-time setup

**Q:** This is my first time using Ocean Watch. Can you check the environment and guide me through Marketing authorization?

**Ocean Watch:** It checks Codex, the bundled Go CLI, the secure credential backend, and the OAuth callback port before guiding channel-specific App ID and Secret setup. Marketing and Qianchuan require separate authorization. Only Qianchuan work-link parsing additionally requires Python `3.10+` and pinned F2.

### Refresh authorized advertisers

**Q:** This authorized user received access to new advertisers. Bring my local authorization snapshot up to date with the official account.

**Ocean Watch:** It understands the outcome as refreshing the official advertiser coverage of the current Marketing OAuth user without requiring a fixed phrase. The local snapshot is atomically replaced only after complete official pagination and validation; malformed or incomplete official results preserve the previous snapshot and return an explicit error.

### Responsible accounts and spend

**Q:** Add Qianchuan account `1234567890` to the accounts I manage and call it “Flagship Store.”

**Ocean Watch:** It adds the account to the local registry. Later questions such as “Which accounts am I responsible for?” return the enabled local list only, without querying reports, refreshing tokens, or confusing the registry with the official authorized-advertiser snapshot.

**Follow-up:** Now show today's spend for those accounts, summarize by channel, and identify failures.

**Ocean Watch:** It resolves “those accounts” from the conversation, queries account-level reports concurrently, and isolates individual failures. Marketing and Qianchuan GMV and ROI remain separate because their official definitions differ.

### Create Marketing plans from uploaded videos

**Q:** Find today's videos for my sunscreen account, group five per unit, and create plans with my mixed-material template. Let me review them first.

**Ocean Watch:** It previews the advertiser, template, groups, budget, ROI, and plan count. Nothing is submitted until you explicitly confirm the preview; resumable project IDs remain visible if unit creation fails after project creation.

### Use creator-authorized materials

**Q:** Find videos from creator `DOUYIN_SHOW_ID` that are still authorized for this Marketing advertiser, then preflight a native-material plan.

**Ocean Watch:** It distinguishes public videos from materials this advertiser can actually deliver and uses only a valid authorization snapshot. Batch preflight separates ready, completed, resumable, and blocked jobs before confirmation.

### Process Qianchuan plans from Douyin links

**Q:** Create a sunscreen product template for Qianchuan account `1234567890`, product `987654321`, budget 5000, and target ROI 1.7.

**Ocean Watch:** It asks for both the full product name and the short name used in plan naming, then validates channel, advertiser, and product bindings. The default plan name is `date-creator-product-short-name-type-business-owner`; type and business owner belong to each potential new-plan run, not the template.

**Follow-up:** Use that template for these Douyin works. Create a plan when the creator has no matching plan; otherwise append only missing materials. Preflight first.

**Ocean Watch:** It first resolves public metadata with F2 in a batch, then uses the numeric UID or a valid 30-day identity cache to run targeted official checks under the requested advertiser. Missing, expired, or mismatched works are skipped quickly; it never scans every authorized creator. If a plan is needed, it collects the run-specific type and business owner. Existing plans receive only missing materials and keep their names. After confirmation, successful rows always contain `Plan ID | Creator | Product ID | Material ID | Material title`; skips, incomplete official queries, and failures are explained separately.

### Remove a Qianchuan work safely

**Q:** Remove this Douyin work from plan `AD_ID`, but first show the exact materials and linked impact without executing.

**Ocean Watch:** It maps the work to exact plan materials and only permits removal of custom-selected materials. Submission requires another confirmation and is followed by an official state check.

### Create a Qianchuan live plan

**Q:** Create a live all-domain template for Qianchuan account `1234567890` and preview a live plan with default settings.

**Ocean Watch:** It uses a dedicated live template without product-work fields, then previews budget, bidding, schedule, and intelligent material settings before submission.

### Review performance

**Q:** Show the last seven days of Marketing material performance, list the top ten by spend and high-spend low-conversion materials, then recommend actions.

**Ocean Watch:** It returns a read-only report with pause, observe, or scale recommendations and never changes plans automatically.

**Follow-up:** What about today's product all-domain plans for Qianchuan account `1234567890`?

**Ocean Watch:** It queries Qianchuan plan reporting, summarizes spend, GMV, orders, and weighted ROI, and preserves Qianchuan-specific metric definitions.

### Query Qianchuan by business object

**Q:** Show yesterday's combined all-domain and Multiplication spend and GMV for Qianchuan account `1234567890`.

**Ocean Watch:** It recognizes a single-advertiser aggregate and uses the official Multiplication account report. “All-domain only” selects the all-domain account report instead, while “my managed accounts” still refers to the local responsible-account set.

**Follow-up:** What were this week's spend, GMV, and ROI for product `987654321`? Then show hourly performance for room `ROOM_ID` last night.

**Ocean Watch:** It selects product reporting first and room-by-hour reporting second. A request to find the product itself routes to product asset discovery instead. Creator, plan, material, and available-field questions similarly route by object and context without requiring CLI vocabulary.

## Confirmation Rules

- Account, material, template, plan-detail, and report queries are read-only.
- Create, append, remove, and plan-setting workflows preview first and require explicit confirmation for online writes.
- Templates, tokens, and materials cannot cross channel or advertiser boundaries.
- “Accounts I manage” reads the local registry; refreshing advertisers available to the authorized user is a separate official authorization sync.
- Spend, GMV, ROI, orders, or dated performance invoke official reports; cross-channel summaries combine only comparable spend.
- Batch jobs preserve successful results and separately explain skips, failures, and incomplete official queries.
- Required Presentation output uses the CLI's `rendered_markdown` without dropping columns, changing metric definitions, or hiding empty table headers.

## Automation and Diagnostics

Daily users should express delivery goals in natural language without learning internal command groups. Use the stable CLI only for scripting, exact arguments, or diagnostics; see the [CLI reference](docs/cli.md). macOS and Linux use `skills/*/run`, while Windows uses `skills\\*\\run.cmd`; both launch the same Go CLI and JSON/Presentation contract.

Use `setup doctor`, `auth status`, `auth mappings`, and `runs` for environment, authorization, advertiser mapping, and local execution-history diagnostics. For a Qianchuan work link, `qc-materials inspect-work` exposes the F2 mapping, while official Qianchuan APIs under the target advertiser remain authoritative for authorization, ownership, product matching, and deliverability.

## Security and Privacy

- App secrets, access tokens, refresh tokens, and authorization codes never belong in Git or ordinary project configuration.
- Credentials use Keychain on macOS, DPAPI on Windows, and Secret Service on Linux.
- User configuration, authorization snapshots, caches, and execution history live under `$CODEX_HOME/ads-plan-monitor/`.
- Official business traffic is restricted to approved Ocean Engine HTTPS hosts.
- F2 reads public Douyin work metadata only. It does not download media, create a database, or automatically read browser cookies; product hints never replace official Qianchuan checks.
- Reports, caches, logs, job files, and journals are not open-source repository content.

See [Security](SECURITY.md) and [Configuration](docs/configuration.md).

## For Developers

Run the bundled CLI without installing a Go toolchain or Python business package:

```bash
skills/ads-plan-monitor/run --help
skills/qc-plan-monitor/run --help
```

Use the matching `run.cmd` on Windows. Source development and tests require Go `1.26.5`; only F2-related flows require Python `3.10+` and F2 `0.0.1.7`.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md) (Chinese)
- [CLI reference](docs/cli.md) (Chinese)
- [Configuration](docs/configuration.md) (Chinese)
- [Architecture](docs/architecture.md) (Chinese)
- [Release guide](docs/releasing.md) (Chinese)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Quality Checks

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go test ./...
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go vet ./...
python3 -m unittest discover -s f2 -p 'test_resolve.py' -v
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all --verify
git diff --check
```

CI runs Go tests and static checks and validates the matching bundled CLI on Linux, macOS, and Windows. Linux also runs F2 mapping, version, and distribution-contract checks. Release automation performs deterministic five-platform build verification; it never edits files or pushes a version-bump commit back to `main`.

## Acknowledgements

- [F2](https://github.com/Johnserf-Seed/f2) provides public Douyin work-metadata resolution for Qianchuan work-link flows. Ocean Watch pins F2 `0.0.1.7`, maps its output into a stable contract, and rechecks results through official Qianchuan APIs.
- [Ocean Engine Open Platform Go SDK](https://github.com/oceanengine/ad_open_sdk_go) provides Go SDK support, request models, and response models for official Ocean Engine Marketing and Qianchuan API integrations.

Thank you to the maintainers and contributors of these projects.

## License

MIT
