<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">
  Ocean Engine Campaign Monitoring Assistant
</p>

<p align="center">
  An open-source Codex Plugin for campaign creation, material reporting, account authorization, and performance analysis through official APIs
</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="skills/ads-plan-monitor/SKILL.md"><img src="https://img.shields.io/badge/Skill-ads--plan--monitor-4B5563" alt="Ads Plan Monitor Skill"></a>
  <a href="skills/ads-plan-monitor/scripts/"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="skills/ads-plan-monitor/references/official-api-notes.md"><img src="https://img.shields.io/badge/Ocean%20Engine-API%20%2B%20MCP-1677FF" alt="Ocean Engine API and MCP"></a>
  <a href="SECURITY.md"><img src="https://img.shields.io/badge/Credentials-local%20store-6B7280" alt="Local credential store"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

[中文](./README.md) | English

`ocean-watch` is an installable Codex Plugin for automating Ocean Engine advertising workflows. The plugin contains one `ads-plan-monitor` Skill with four internal branches: first-run setup, campaign creation, data queries, and strategy analysis.

Business operations currently use the official Ocean Engine Marketing API under the `marketing` channel. The Plugin now isolates channels; `qianchuan` is reserved for future Ocean Engine Qianchuan support and never reuses Marketing apps, tokens, or accounts. The official developer-documentation MCP provides API documentation, OpenAPI schemas, and SDK examples. Real business configuration and OAuth credentials remain on each user's computer.

## Features

| Scenario | Capability |
| --- | --- |
| First run | Creates local configuration and checks OAuth, tokens, advertiser accounts, and official MCP status |
| Account authorization | Stores independent channel apps and multiple OAuth authorizations, then resolves tokens by official `account_id` and target advertiser |
| Token management | Checks token validity before API calls, refreshes expiring tokens, and saves rotated credentials |
| Campaign creation | Generates projects and promotions from platform and product templates, then submits them after confirmation |
| Batch creation | Retrieves videos uploaded today, groups N videos into each promotion, and supports concurrent multi-account creation |
| Data queries | Queries promotions, active materials, the video library, and material-dimension reports |
| Monitoring strategy | Provides recommendations based on spend, ROI, conversions, and other performance data |
| Official documentation | Queries API documentation, schemas, and SDK examples through the official MCP |

Campaign templates use a shared default base plus advertiser-bound business templates. The default base contains only reusable delivery settings and never participates in real delivery. The wizard clears account assets, product assets, links, and copy according to whether the advertiser or product changes. Incomplete templates may be saved as drafts but cannot be activated. Each business template belongs to one advertiser, and multi-account batches resolve a separate template for every advertiser.

## Installation

Requires Codex CLI 0.144.1 or later and Python 3.9 or later:

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

After installing or upgrading, start a new Codex task to load the latest Plugin. A clean development checkout without private local directories can also be used as a local marketplace:

```bash
codex plugin marketplace add "$(pwd)"
codex plugin add ocean-watch@ocean-watch
```

Do not install locally from a working copy containing local `config/`, `runs/`, or `.venv/` directories. Codex may copy these untracked directories. Prefer installation from the GitHub marketplace for regular use.

## First Run

Ask Codex directly:

```text
Initialize ads-plan-monitor
```

The guide creates `~/.codex/ads-plan-monitor/config.json` and reports the next steps for OAuth, tokens, business templates, and the official MCP. When an active template exists but is incomplete, it asks the user to complete it instead of creating another template. Inside the development repository, it prefers the Git-ignored `config/ads-plan-monitor/config.json` instead.

OAuth App ID, Secret, Access Token, Refresh Token, and MCP `developer_id` are stored in the operating system credential store. They are never written to project configuration and should not be pasted into chat.

When upgrading, migrate existing state once. Existing app credentials, tokens, authorized accounts, and templates become `marketing` state without reauthorization:

```bash
python3 skills/ads-plan-monitor/scripts/migrate_channels.py \
  --config config/ads-plan-monitor/config.json
```

Migration is safely repeatable and resumes from the same journal after interruption. If a legacy token lacks a complete official-account mapping, status reports `pending_account_sync: true`. Run one explicit sync with the reported `authorization_id`:

```bash
python3 skills/ads-plan-monitor/scripts/token_manager.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --authorization-id <AUTHORIZATION_ID> \
  --sync-accounts
```

### OAuth

When developing from the repository, run:

```bash
python3 skills/ads-plan-monitor/scripts/credential_store.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing \
  --set-app

python3 skills/ads-plan-monitor/scripts/oauth_local_authorize.py \
  --config config/ads-plan-monitor/config.json \
  --channel marketing
```

The default callback URL is `http://127.0.0.1:8787/oauth/callback`. It must exactly match the callback configured for the application on the Ocean Engine Open Platform.

The same Marketing app may be authorized multiple times. The Plugin keeps every authorization and automatically selects the one whose official account covers the target `advertiser_id`; use `--auth-account-id` only when resolution is ambiguous. Business commands default to `--channel marketing`.

`qianchuan` currently reserves an isolated channel structure only. OAuth and business APIs are not implemented, and it never reads or reuses Marketing apps, tokens, accounts, or templates.

### Official MCP

The official MCP endpoint requires each user's own `app_id` and `developer_id`. The Plugin does not hard-code personal parameters in its public manifest. Instead, it registers the MCP in the current user's Codex configuration during setup:

```bash
python3 skills/ads-plan-monitor/scripts/configure_official_mcp.py
```

The script reads `app_id` from the system credential store, securely prompts for `developer_id`, performs a handshake with the official service, verifies the tool list, and registers the local SSE-to-stdio bridge as `oceanengine-developer-docs`. Check its status with:

```bash
python3 skills/ads-plan-monitor/scripts/configure_official_mcp.py --status
```

Codex configuration stores only the path to the local bridge script. The official URL containing `app_id` and `developer_id` exists only in the bridge process memory. Status output does not expose the MCP URL or identifiers. Business API features remain available when MCP is not configured; the Skill falls back to sanitized references bundled with the repository.

## Usage Examples

```text
Show the top 10 materials by spend today for the current advertiser account
List videos uploaded today
Create campaigns from today's uploaded videos, five videos per promotion, and dry-run first
Create one campaign from this video using the specified campaign template
Give me monitoring recommendations based on material-level performance
Look up the official fields and OpenAPI schema for promotion/create
```

Creating real campaigns is a write operation. The Plugin performs a read-only query or payload preview by default and calls creation APIs only after the user explicitly requests submission.

During Plugin or Skill development, advertiser, product, and URL details are treated as feature test cases by default. Codex changes only repository source, public examples, documentation, and tests, using isolated temporary configuration for validation. It does not modify real local `config/`, query real accounts, or call business APIs unless the user separately and explicitly requests a business operation.

## Project Structure

```text
.
├── .agents/plugins/marketplace.json       # GitHub and local marketplace entry
├── .codex-plugin/plugin.json              # Codex Plugin manifest
├── skills/ads-plan-monitor/
│   ├── SKILL.md                           # Core instructions for the single Skill
│   ├── agents/                            # Skill UI metadata
│   ├── assets/                            # Sanitized example configuration
│   ├── references/                        # API notes and template rules
│   └── scripts/                           # Authorization, query, creation, and MCP guides
├── tests/                                 # Regression tests
├── docs/                                  # Installation, configuration, commands, and design docs
├── CONTRIBUTING.md
├── SECURITY.md
└── LICENSE
```

Local directories excluded from the repository include `config/`, `runs/`, `.venv/`, caches, logs, and temporary output.

## Documentation

- [Configuration, OAuth, and MCP](docs/configuration.md)
- [Common commands](docs/commands.md)
- [Project structure](docs/project-structure.md)
- [Security policy](SECURITY.md)
- [Contributing guide](CONTRIBUTING.md)

## Development Checks

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m py_compile skills/ads-plan-monitor/scripts/*.py
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
python3 -m json.tool skills/ads-plan-monitor/assets/config.example.json >/dev/null
python3 -m unittest discover -s tests -v
git diff --check
```

## License

MIT
