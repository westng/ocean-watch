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

<p align="center">
  <a href="README.en-US.md">README</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="CONTRIBUTING.md">Contributing</a> ·
  <a href="LICENSE">MIT license</a> ·
  <a href="SECURITY.md">Security</a> ·
  <a href="CHANGELOG.md">Changelog</a>
</p>

[中文](README.md) | English

Ocean Watch lets delivery teams manage Ocean Engine Marketing and Qianchuan from Codex in natural language. It uses official Ocean Engine APIs for authorization, accounts, materials, templates, plans, reports, and delivery analysis, and it previews every online write before asking for explicit confirmation.

## What It Does

| Skill | Channel | Main capabilities |
| --- | --- | --- |
| `ads-plan-monitor` | Ocean Engine Marketing | OAuth, authorized-advertiser sync, responsible accounts, uploaded and creator-authorized materials, templates, plans, reports, and strategy |
| `qc-plan-monitor` | Qianchuan | OAuth, authorized-advertiser sync, responsible accounts, product and live templates, creator works, all-domain plans, and all-domain/Multiplication reports |

The two Skills share one Go business implementation while strictly isolating channel credentials, authorized users, advertisers, templates, materials, and write transactions.

## Why Ocean Watch

- **Natural-language workflows:** Understands business goals, conversational wording, abbreviations, and contextual follow-ups without requiring fixed commands.
- **Official data is authoritative:** Advertising operations use official Ocean Engine APIs; public work metadata never replaces official authorization, ownership, or product checks.
- **Safe by default:** Create, append, remove, and plan-setting operations preview first and submit only after explicit confirmation.
- **Credential isolation:** Secrets and tokens use operating-system secure storage and never belong in ordinary project configuration, output, or logs.
- **Traceable outcomes:** Batch jobs retain successful results and separately explain skips, failures, and incomplete official queries; uncertain writes are reconciled by reading official state first.

## Quick Start

Requires Codex CLI `0.144.1+`. Standard Marketing and Qianchuan workflows use the Plugin's bundled runtime. Only Qianchuan Douyin work-link parsing additionally requires Python `3.10+` and F2 `0.0.1.7`.

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

Start a new Codex task after installation or upgrade and describe the desired outcome. See the Chinese [getting-started guide](docs/getting-started.md) for environment checks, channel configuration, and OAuth.

## Ask It Naturally

- **Set up authorization:** “Check my environment and guide me through Ocean Engine Marketing authorization.”
- **Refresh access:** “This authorized user received new advertisers. Update the local access snapshot to match the official account.”
- **Review business data:** “Show today's spend for the Marketing and Qianchuan accounts I manage, summarize by channel, and identify failures.”
- **Create Marketing plans:** “Find today's uploaded videos, group five per unit, and create plans with my template. Let me review them first.”
- **Process Qianchuan works:** “Use these Douyin works for my product all-domain plans. Create missing plans and append only missing materials to existing plans. Preflight first.”
- **Analyze performance:** “Review the last seven days of material performance, identify high-spend low-conversion materials, and recommend actions.”

These are examples, not fixed commands. Ocean Watch selects the channel and capability from context and asks for missing advertisers, templates, products, date ranges, or write confirmation.

## Operational and Security Boundaries

- Account, material, template, plan-detail, and report queries are read-only.
- Create, append, remove, budget, ROI, and status changes must preview first; no explicit confirmation means no online write.
- “Accounts I manage” reads the local responsible-account registry; refreshing advertisers available to an authorized user is a separate official authorization sync.
- Marketing and Qianchuan GMV and ROI retain their official channel-specific definitions; cross-channel summaries combine only comparable spend.
- F2 reads public Douyin work metadata only. It does not download media, create a database, or automatically read browser cookies; official Qianchuan APIs under the target advertiser always determine deliverability.
- User configuration, authorization snapshots, caches, reports, and execution history live locally under `$CODEX_HOME/ads-plan-monitor/` and are not open-source repository content.

See [Configuration](docs/configuration.md) and [Security](SECURITY.md) for details.

## Runtime and Support

```text
Codex → Skill → local stdio MCP ─┐
             └→ run/run.cmd ─────┴→ Go Application Service → official Ocean Engine API
                                                        └→ Python 3.10+ → F2 0.0.1.7
                                                           Douyin public metadata only
```

- Advertising logic has one Go Application/Domain implementation. MCP and CLI are two transports over that implementation, not separate business runtimes or silent fallback paths.
- Local template lists and exact details use MCP's `list_templates` and `get_template`; capabilities not yet exposed as tools use the bundled Go CLI.
- The Plugin bundles CLI binaries for macOS Intel and Apple Silicon, Linux x86_64 and ARM64, and Windows x86_64. Ordinary users do not need Go.
- The current fixed MCP launch manifest has completed development acceptance only on macOS Apple Silicon; five-platform CLI packaging does not mean five-platform MCP installation acceptance.
- Python participates only in public metadata resolution for Qianchuan work links. It does not own authorization, accounts, templates, plans, or reports.

See [Architecture](docs/architecture.md) for current implementation boundaries.

## Documentation

| Audience | Entry points |
| --- | --- |
| Everyday users | [Documentation](docs/README.md) · [Getting started](docs/getting-started.md) · [Configuration](docs/configuration.md) |
| Scripting and diagnostics | [CLI reference](docs/cli.md) |
| Developers | [Architecture](docs/architecture.md) · [Contributing](CONTRIBUTING.md) |
| Project and security | [Security](SECURITY.md) · [Changelog](CHANGELOG.md) · [Release guide](docs/releasing.md) · [MIT license](LICENSE) |

Detailed guides are currently maintained in Chinese.

## Acknowledgements

- [F2](https://github.com/Johnserf-Seed/f2) provides public Douyin work-metadata resolution for Qianchuan work-link workflows.
- [Ocean Engine Open Platform Go SDK](https://github.com/oceanengine/ad_open_sdk_go) provides Go SDK support for official Marketing and Qianchuan API integrations.

## License

[MIT](LICENSE)
