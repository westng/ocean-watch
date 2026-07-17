# Changelog

All notable changes to Ocean Watch are documented here.

## Unreleased

### Changed

- Consolidated long-lived documentation around one index and four maintained guides, added task-oriented examples for everyday users, and removed duplicated project-tree documentation and completed implementation design drafts.
- Accelerated Qianchuan work-link preflight with an optional local-only metadata endpoint, public-link identity and product hints, bounded default concurrency, a 30-day advertiser-scoped owner-hint cache, mandatory official revalidation, and stage-level latency metrics. The private endpoint is no longer present in tracked source or documentation.
- Added one fast local `templates list` command that returns compact Marketing and Qianchuan template summaries from a single config read, with optional channel filtering and full details.
- Added a resumable Marketing creator-batch preflight that reports completed, ready, retry, and blocked jobs before submission while treating project capacity as a create-time-only check.
- Updated the Marketing default template to target the 29 top-level regions outside Hong Kong, Macao, Taiwan, Xinjiang, and Tibet.
- Changed the Marketing creation base to official DPA product images so the standard wizard no longer asks users for image IDs; advanced `CUSTOM` image templates retain strict image-ID validation.
- Unified business-template names as `渠道-广告账户ID-商品名-商品ID-模版类型`; Qianchuan Schema v3 migrates existing display names without changing template IDs or bindings.
- Removed active/default business-template pointers. Marketing Schema v5 and Qianchuan Schema v4 require every creation workflow to select a real business template explicitly; default templates remain creation skeletons only.
- Added explicit Marketing wizard fields and previews for daily budget, net-order ROI goal, gender, and age targeting.
- Added validated custom product-selling-point collection so Marketing templates satisfy the official 6–9-position payload rule before activation.
- Changed Codex OAuth guidance to return a temporary local start URL so users can choose the browser profile bound to the intended Ocean Engine account.
- Required Codex to keep polling OAuth after returning the start URL and proactively report account synchronization and Token-mapping results.
- Added a shared template-creation router that asks for Marketing or Qianchuan before entering a channel-specific source-template wizard, independent of authorization state.
- Split Marketing template creation into mixed/account-upload and native/creator-authorized modes before source-template selection, with mode-filtered source lists.
- Added channel-index validation for template advertiser bindings, removed placeholder defaults, and exposed verified or unverified binding status in wizard previews.

### Fixed

- Recovered creator cover IDs at runtime from unique same-advertiser, same-item, same-material official promotions when the current authorization snapshot omits the field, with create-time authorization warnings and guarded promotion-only retries after official authorization rejection.
- Bound project and promotion discovery to an explicit advertiser ID for correct multi-account Token selection and official request parameters.
- Fixed Chromium OAuth setup stalls caused by idle connections, incomplete official redirect origins, and duplicate form submissions.
- Accepted the official empty advertiser-page contract (`total_page: 0`, `total_number: 0`) while retaining strict checks for inconsistent pagination data.
- Resolved missing Marketing event assets from unique same-account, same-product projects and added a guarded DPA-image fallback from matching official promotions, blocking ambiguous or unavailable assets before project creation.
- Corrected Qianchuan plan reconciliation and report metadata queries to use one legal recent-180-day data period while traversing every declared plan page, instead of sending invalid historical data windows.
- Fixed Qianchuan creator-plan reconciliation when plan lists return a visible Douyin ID but plan details return the numeric `aweme_id`, preventing an existing plan from being misclassified as a new plan.

## 0.9.1 - 2026-07-16

### Fixed

- Deferred local OAuth until first use instead of treating the loopback callback as an installation-time authentication page.
- Added clear loopback diagnostics for direct, empty, trailing-slash, and unknown callback requests without terminating a valid authorization session.

### Added

- Added `setup doctor` and first-run environment reporting for Python, operating-system, Codex CLI, secure credential backend, and OAuth callback-port readiness.
- Added explicit Ocean Engine developer registration and API-permission prerequisites.

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
