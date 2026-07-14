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
  <a href="pyproject.toml"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

[中文](./README.md) | English

`ocean-watch` is an installable Codex Plugin for Ocean Engine advertising operations. It contains one `ads-plan-monitor` Skill that routes first-run setup, authorization, templates, materials, plan creation, reports, and strategy requests.

The current implementation targets Ocean Engine Marketing (`marketing`) through official APIs. Ocean Engine Qianchuan (`qianchuan`) has an isolated channel boundary but is not implemented and never reuses Marketing credentials or accounts.

## Capabilities

| Domain | Support |
| --- | --- |
| Authorization | Local OAuth, multiple authorization records, token refresh, advertiser sync |
| Templates | Default base, advertiser/product/platform/source bindings, guided creation and migration |
| Materials | Uploaded videos, creator homepage videos, authorized cooperation videos |
| Plans | Dry-run, uploaded and creator materials, concurrent batches, resumable failures |
| Reports | Active material joins, material metrics, spend rankings, custom reports |
| Strategy | Read-only evidence and operational recommendations |
| Development | Official documentation MCP, OpenAPI schemas, and SDK examples |

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
```

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
ocean-watch templates list
ocean-watch materials videos --mode library-get --date today --fetch-all
ocean-watch reports materials
ocean-watch plans create --plan-template TEMPLATE --video-id VIDEO_ID
```

Plan commands are dry-run by default. Online writes require an explicit `--submit`.

## Security

- App secrets, access tokens, refresh tokens, and authorization codes never belong in Git config.
- Credentials use macOS Keychain, Windows DPAPI, or Linux Secret Service.
- Business config defaults to `~/.codex/ads-plan-monitor/config.json`.
- `config/`, `runs/`, logs, caches, and runtime batch manifests are not open-source artifacts.
- Plugin development does not read real business state or call real accounts without a separate explicit execution request.

See [Security](SECURITY.md) and [Configuration](docs/configuration.md).

## Architecture

The project uses a standard `src/` package and one CLI. `OceanEngineClient` is the only ordinary business API transport, while `PlanExecutor` owns the shared project/promotion transaction used by single and batch workflows.

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
    ├── templates/
    ├── materials/
    ├── plans/
    ├── reports/
    └── discovery/
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
PYTHONPATH=skills/ads-plan-monitor/src python3 -m compileall -q skills/ads-plan-monitor/src/ocean_watch
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
python3 -m json.tool .codex-plugin/plugin.json >/dev/null
python3 -m json.tool .agents/plugins/marketplace.json >/dev/null
git diff --check
```

CI runs on Windows, macOS, and Linux with Python `3.9` and `3.12`.

## License

MIT
