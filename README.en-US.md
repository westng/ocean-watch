<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">Ocean Engine monitoring assistant</p>

<p align="center">
  An open-source Codex Plugin for OAuth, material discovery, plan creation, reporting, and delivery analysis through official APIs
</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="skills/ads-plan-monitor/SKILL.md"><img src="https://img.shields.io/badge/Skill-ads--plan--monitor-4B5563" alt="Ads Plan Monitor Skill"></a>
  <a href="skills/qc-plan-monitor/SKILL.md"><img src="https://img.shields.io/badge/Skill-qc--plan--monitor-2563EB" alt="QC Plan Monitor Skill"></a>
  <a href="pyproject.toml"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

[中文](./README.md) | English

`ocean-watch` is an installable Codex Plugin with two independent business Skills: `ads-plan-monitor` for Ocean Engine Marketing and `qc-plan-monitor` for Qianchuan. They share one engineered CLI runtime without sharing authorization, advertiser, template, or creation transactions.

Ocean Engine Marketing (`marketing`) supports authorization, materials, plans, reports, and strategy. Qianchuan (`qianchuan`) supports isolated app authorization, token refresh, advertiser discovery, product all-domain templates, product-filtered creator video discovery, plan material creation, append, or removal from Douyin work links, and all-domain plan reporting through the official MCP. Qianchuan strategy and live templates remain unavailable. The channels never share apps, tokens, accounts, templates, endpoints, or creation transactions.

## Capabilities

| Domain | Support |
| --- | --- |
| Marketing | `ads-plan-monitor`: OAuth, templates, uploaded/creator materials, plans, reports, strategy |
| Qianchuan | `qc-plan-monitor`: OAuth, product templates, creator videos, work-link plans, and all-domain plan reports |
| Authorization | Isolated Marketing/Qianchuan OAuth, multiple authorization records, token refresh |
| Responsible accounts | Local cross-channel account registry, enable/disable controls, concurrent spend summaries |
| Templates | Default base, advertiser/product/platform/source bindings, guided creation and migration |
| Materials | Uploaded videos, Marketing creator videos, and product-filtered Qianchuan creator videos |
| Plans | Marketing uploaded/creator plans, Qianchuan create, append, or remove work materials, dry-run and explicit submit |
| Reports | Marketing material/custom reports and Qianchuan all-domain plan spend, GMV, orders, and ROI |
| Strategy | Read-only evidence and operational recommendations |
| Development | Official documentation MCP, OpenAPI schemas, and SDK examples |

All business templates use `Channel-AdvertiserID-ProductName-ProductID-TemplateType`. The Marketing wizard binds the advertiser, product, platform, and material mode. Its creation base uses official DPA product images, so users do not enter image IDs; budget, net-order ROI, gender, and age targeting are shown before confirmation. Before submission, official APIs validate the event asset and DPA fields. Product images may be reused only from an official promotion matching both advertiser and product; ambiguous or missing assets block before project creation.

Default templates are creation skeletons only. Real business templates have no active/default state and every plan-creation command must select one explicitly.

## Install

Requires Codex CLI `0.144.1+` and Python `3.9+`:

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

Start a new Codex task after installation or upgrade, then ask:

```text
Initialize ads-plan-monitor
Show today's top ten materials by spend
Create one unit per five videos uploaded today, dry-run first
Query authorized creator videos and create a creator-material plan
Use qc-plan-monitor to authorize Qianchuan and dry-run a product all-domain plan
Use qc-plan-monitor to find creator videos matching a Qianchuan product template
Use qc-plan-monitor to dry-run removing a work from a Qianchuan plan
Use qc-plan-monitor to query today's all-domain plan spend for a Qianchuan advertiser
Show today's spend for the accounts I am responsible for
```

Installation does not open or listen on the OAuth callback URI. On first authorization, `auth authorize` starts a temporary local server and opens the actual entry URL. `http://127.0.0.1:8787/oauth/callback` is only for official-console registration and the official redirect; users should not open it directly.

Run the environment check before first use:

```bash
ocean-watch setup doctor
```

It verifies Python `3.9+`, Windows/macOS/Linux, Codex CLI availability, the secure credential backend, and the OAuth callback port. When Python is missing, Codex must stop after system-level detection and ask the user to install it; the Plugin does not silently install runtimes.

### Developer requirements

- Register as an Ocean Engine developer before using the SDK. See the [Developer Quick Start](https://open.oceanengine.com/labels/7/docs/1696710498372623).
- Obtain the required API access first. Every SDK capability is limited by the permission groups granted to the application.

## Development

Run the unified CLI without installing the package:

```bash
python3 skills/ads-plan-monitor/run.py --help
python3 skills/ads-plan-monitor/run.py setup init --home-config
```

Or install an editable package:

```bash
python3 -m venv .venv
source .venv/bin/activate              # Windows: .venv\Scripts\activate
python3 -m pip install -e .
ocean-watch --help
```

Commands follow `ocean-watch <domain> <action>`:

```bash
ocean-watch auth status --channel marketing
ocean-watch auth authorize --channel qianchuan
ocean-watch accounts add --channel qianchuan --advertiser-id ADVERTISER_ID --name ACCOUNT_NAME
ocean-watch accounts report
ocean-watch templates create
ocean-watch qc-templates create
ocean-watch qc-materials creator-videos --plan-template TEMPLATE_ID --douyin-id DOUYIN_SHOW_ID
ocean-watch plans batch-qianchuan-works --plan-template TEMPLATE_ID --work-url DOUYIN_WORK_URL
ocean-watch plans remove-qianchuan-work --advertiser-id ADVERTISER_ID --ad-id AD_ID --work-url DOUYIN_WORK_URL
ocean-watch templates list
ocean-watch materials videos --mode library-get --date today --fetch-all
ocean-watch reports materials
ocean-watch qc-reports plans --advertiser-id ADVERTISER_ID
ocean-watch plans create --plan-template TEMPLATE --video-id VIDEO_ID
ocean-watch plans create-qianchuan --payload-file QIANCHUAN_PAYLOAD.json
```

Plan commands are dry-run by default. Creator batches provide a dedicated `--preflight` that combines live validation with the local journal to report completed, pending, resumable, and blocked jobs. Project capacity is confirmed only by the official create endpoint. Online writes require an explicit `--submit`.

## Security

- App secrets, access tokens, refresh tokens, and authorization codes never belong in Git config.
- Credentials use macOS Keychain, Windows DPAPI, or Linux Secret Service.
- User config, authorization state, and fallback credentials share `$CODEX_HOME/ads-plan-monitor/`; `CODEX_HOME` defaults to `~/.codex` when unset.
- Official business API, OAuth, and MCP transports allow only official HTTPS hosts, reject redirects, and bound response sizes.
- `config/`, `runs/`, logs, caches, and runtime batch manifests are not open-source artifacts.
- Plugin development does not read real business state or call real accounts without a separate explicit execution request.

See [Security](SECURITY.md) and [Configuration](docs/configuration.md).

## Architecture

The project uses two business Skills and one shared `src/` CLI runtime. `OceanEngineClient` is the only ordinary business API transport, while channel-specific services keep Marketing and Qianchuan transactions separate.

```text
skills/ads-plan-monitor/
├── SKILL.md
├── assets/
├── references/
├── run.py
└── src/ocean_watch/
    ├── cli/
    ├── core/
    ├── api/
    ├── auth/
    ├── accounts/
    ├── templates/
    ├── materials/
    ├── plans/
    ├── reports/
    └── discovery/
skills/qc-plan-monitor/
├── SKILL.md
├── assets/
├── references/
└── run.py
```

See [Architecture](docs/architecture.md) for module contracts and data flow.

## Documentation

- [Getting started](docs/getting-started.md)
- [CLI reference](docs/cli.md)
- [Configuration and authorization](docs/configuration.md)
- [Architecture](docs/architecture.md)
- [Project structure](docs/project-structure.md)
- [Contributing](CONTRIBUTING.md)
- [Security](SECURITY.md)
- [Changelog](CHANGELOG.md)

## Quality checks

```bash
python3 -m pip install -e ".[dev]"
PYTHONPATH=skills/ads-plan-monitor/src python3 -m compileall -q skills/ads-plan-monitor/src/ocean_watch
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch
python3 -m build
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
git diff --check
```

CI runs on Windows, macOS, and Linux with Python `3.9` and `3.12`. The release gate also builds the sdist and wheel, installs the wheel in isolated Python `3.9` and `3.12` environments, and runs first-time setup under a temporary `CODEX_HOME` to verify packaged resources and the installed CLI.

## License

MIT
