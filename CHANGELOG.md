# Changelog

All notable changes to Ocean Watch are documented here.

## 0.9.0 - 2026-07-15

### Added

- Qianchuan all-domain plan spend reports through the official Streamable HTTP MCP.
- Advertiser-bound Token refresh, restricted `Tool-Range`, paged all-domain report values, plan metadata enrichment, and weighted ROI summaries for `qc-reports plans`.
- Local Marketing/Qianchuan responsible-account registry with channel-safe identity, enable/disable controls, concurrent account spend summaries, partial failures, and bounded retries for rate limits or transient service timeouts.

### Changed

- Unified project config, authorization state, and credential fallbacks under `CODEX_HOME`, with atomic locked writes and optimistic conflict detection for template wizards.
- Restricted official API, OAuth, and MCP transports to approved HTTPS hosts, disabled redirects, bounded responses, and redacted transport errors.
- Added wheel-only CI verification for packaged first-run resources and expanded generated-artifact ignore rules.

## 0.8.0 - 2026-07-14

### Added

- Isolated Qianchuan OAuth, token refresh, authorized-subject expansion, and advertiser discovery.
- Official-payload Qianchuan all-domain plan creation with dry-run validation and explicit submit.
- Dedicated `qc-plan-monitor` Skill for Qianchuan authorization, advertiser discovery, and plan creation.
- Advertiser-bound Qianchuan product all-domain templates with 1–30 products and runtime-only creator materials.
- Qianchuan creator video discovery with exact visible-ID resolution, official product filtering, pagination, and cross-product deduplication.
- Qianchuan product template Schema v2 names templates by advertiser ID and migrates the earlier shop-name prefix.
- Safe Douyin share-link resolution and official creator/product matching in batches of up to 50 works.
- Idempotent Qianchuan work-link batches that create one product all-domain plan per creator or append only missing materials to an existing or paused plan.
- Dry-run-first Qianchuan work-link material removal with custom-material checks, official 100-item batches, and post-delete status verification.
- Advertiser-scoped submit locks, bounded creator concurrency, and final-only skipped/failed batch summaries.
- Channel adapters for authorization URLs, official account endpoints, and role expansion rules.
- Recoverable pending authorization records when advertiser synchronization fails.
- Standard `src/ocean_watch` Python package and `pyproject.toml` metadata.
- Unified `ocean-watch <domain> <action>` CLI and Plugin-local `run.py` launcher.
- Shared `OceanEngineClient`, structured error foundation, common data utilities, and `PlanExecutor` transaction service.
- Dedicated architecture, getting-started, CLI, and contributor documentation.

### Changed

- Consolidated App ID and Secret collection into one local secure form that continues directly to OAuth.
- Routed Marketing and Qianchuan OAuth through one callback URI with validated `AD.<nonce>` and `QC.<nonce>` state values.
- Reorganized authorization, templates, materials, plans, reports, discovery, onboarding, and integrations into explicit domain packages.
- Routed ordinary business API calls through one HTTP client.
- Routed uploaded, creator, single, and batch plan submissions through one project/promotion transaction.
- Replaced all historical script commands with the unified CLI; old script paths are intentionally unsupported.
- Updated tests to import the installed package structure and use explicit client injection.

## 0.7.0 - 2026-07-13

### Added

- Creator-authorized video discovery through the official Aweme authorization relationship API.
- Native promotion payloads using authorized `aweme_id`, `item_id`, `video_id`, and cover IDs.
- Source-bound schema v3 plan templates for account-uploaded and creator-authorized materials.
- Creator material authorization, expiry, advertiser ownership, and same-creator validation.
- Dedicated read-only query and dry-run-first creator creation commands.

### Changed

- New business template names include an explicit material-source suffix.
- Specific video, cover, item, and material IDs are runtime selections instead of schema v3 template fields.
- Legacy fixed material IDs require explicit confirmation before template migration removes them.

### Security

- Creator material development and regression tests use synthetic fixtures and never access local credentials or real advertiser accounts.
