<p align="center">
  <img src="https://avatars.githubusercontent.com/u/277389313?s=200&v=4" width="128" height="128" alt="westng">
</p>

<h1 align="center">ocean-watch</h1>

<p align="center">Ocean Engine delivery and monitoring for Codex</p>

<p align="center">
  <a href=".codex-plugin/plugin.json"><img src="https://img.shields.io/badge/Codex-Plugin-111827" alt="Codex Plugin"></a>
  <a href="https://github.com/westng/ocean-watch/actions/workflows/ci.yml"><img src="https://github.com/westng/ocean-watch/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/westng/ocean-watch/releases"><img src="https://img.shields.io/github/v/release/westng/ocean-watch?display_name=tag&sort=semver" alt="GitHub Release"></a>
  <a href="pyproject.toml"><img src="https://img.shields.io/badge/Python-3.9%2B-3776AB?logo=python&logoColor=white" alt="Python 3.9 or newer"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-111827" alt="MIT License"></a>
</p>

[中文](README.md) | English

`ocean-watch` uses official Ocean Engine APIs for OAuth, account management, material discovery, plan creation, reporting, and delivery analysis. Its two Skills share one CLI runtime but never share apps, tokens, accounts, templates, or creation transactions.

| Skill | Channel | Support |
| --- | --- | --- |
| `ads-plan-monitor` | Ocean Engine Marketing | OAuth, responsible accounts, uploaded/creator materials, templates, plans, reports, strategy |
| `qc-plan-monitor` | Qianchuan | OAuth, responsible accounts, product/live templates, creator and product discovery, plan operations, plan/material reports |

Qianchuan live templates and creation are supported. Strategy remains read-only by default. Create, remove, and plan-setting commands require an explicit `--submit` for online writes.

Daily users do not need to memorize commands, parameters, or Skill names. Describe the desired result in Codex; it will select the channel, ask for missing information, and show a preview before any write.

## Install

Requires Codex CLI `0.144.1+` and Python `3.9+`:

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

Start a new Codex task after installation or upgrade. For first use, ask: `Initialize Ocean Engine monitoring and guide me through Marketing authorization.`

Codex checks the local environment during first use. To run the check manually:

```bash
ocean-watch setup doctor
```

See the Chinese [Getting started guide](docs/getting-started.md) for the complete workflow. Installation never starts OAuth; the temporary callback server starts only when `auth authorize` runs.

### Release artifacts

When a maintainer manually runs the `Release` workflow in GitHub Actions and enters a `vMAJOR.MINOR.PATCH` tag, it publishes these assets to [GitHub Releases](https://github.com/westng/ocean-watch/releases):

- `ocean-watch-plugin-X.Y.Z.zip`: complete offline Codex Plugin bundle.
- `ocean_watch-X.Y.Z-py3-none-any.whl`: independently installable Python CLI.
- `ocean_watch-X.Y.Z.tar.gz`: Python source distribution.
- `SHA256SUMS`: SHA-256 checksums for every release asset.
- GitHub build provenance attestations for repository and workflow verification.

Each Release page takes its version notes directly from the matching `CHANGELOG.md` section instead of substituting an automatically generated commit list.

Codex Marketplace remains the recommended online installation path. See the [release guide](docs/releasing.md) for offline installation, checksum verification, and maintainer release procedures. The Plugin bundle and wheel still require Python 3.9+; they are not standalone native executables.

## Everyday examples

Send requests like these directly to Codex, replacing placeholders with your own account, template, product, and work links.

### First-time setup

```text
Check my Qianchuan environment and guide me through authorization.
```

Codex checks the runtime, secure credential backend, and OAuth port, then guides you through the channel-specific app and authorization flow. Marketing and Qianchuan are authorized separately.

### Daily account overview

```text
Show today's spend for all accounts I am responsible for. Summarize by channel and identify accounts that could not be queried.
```

Accounts are queried concurrently and failures are isolated. Marketing and Qianchuan GMV/ROI remain separate because their official metric definitions differ.

### Create Marketing plans from uploaded videos

```text
Find today's uploaded videos for my sunscreen account. Group five videos per unit and use my mixed-material template. Preview only—do not submit.
```

Codex displays the advertiser, template, groups, budget, ROI, and plan count. A later explicit confirmation is required before online creation.

### Use creator-authorized materials

```text
Find currently authorized videos for this creator and advertiser, then preflight a native-material plan.
```

Public homepage visibility is kept separate from advertiser-specific delivery authorization. Batch preflight reports ready, completed, resumable, and blocked jobs before submission.

### Process Qianchuan plans from Douyin links

```text
Check these Douyin work links against my sunscreen Qianchuan template. Create a plan when the creator has none, otherwise append only missing materials. Preflight first.
```

Official APIs verify creator authorization, work ownership, and product matching. Invalid, unauthorized, mismatched, or existing materials are skipped and explained.

### Remove a Qianchuan work safely

```text
Preflight removing this Douyin work from plan AD_ID. Show the exact materials and any linked impact before doing anything.
```

Only custom-selected materials can be removed. Codex waits for confirmation and verifies the official deletion state afterward.

### Review performance

```text
Show the last seven days of Marketing material spend, highlight high-spend low-conversion materials, and recommend pause, observe, or scale actions.
```

Reports stay in the conversation unless an output file is explicitly requested. Recommendations are read-only and never modify plans automatically.

## Confirmation rules

- Account, material, template, and report queries are read-only.
- Create, append, and remove workflows preview first and require explicit confirmation.
- Templates, tokens, and materials cannot cross channel or advertiser boundaries.
- Partial skips or failures are reported without hiding successful results.

## Advanced: CLI overview

Everyday users do not need the CLI. It is available for scripts, precise arguments, and diagnostics:

```text
ocean-watch
├── setup          # Diagnostics and initialization
├── auth           # Marketing/Qianchuan OAuth and tokens
├── accounts       # Responsible accounts and spend
├── templates      # Unified lookup and Marketing templates
├── qc-templates   # Qianchuan product and live templates
├── materials      # Marketing materials
├── qc-materials   # Qianchuan creator materials and work inspection
├── qc-products    # Qianchuan product discovery
├── plans          # Single and batch plan workflows
├── qc-plans       # Qianchuan plan lookup and setting updates
├── runs           # Local execution journals
├── reports        # Marketing reports
├── qc-reports     # Qianchuan plan and material reports
├── discover       # Official asset discovery
└── mcp            # Developer-documentation MCP
```

Every level supports `--help`. The full command reference is in [docs/cli.md](docs/cli.md).

## Security

- App secrets, access tokens, refresh tokens, and authorization codes never belong in Git configuration.
- Credentials use Keychain on macOS, DPAPI on Windows, and Secret Service on Linux.
- User configuration and state live under `$CODEX_HOME/ads-plan-monitor/`.
- Official business traffic is restricted to approved Ocean Engine HTTPS hosts. The optional local Qianchuan metadata service receives public Douyin links only.
- Reports, caches, logs, job files, and journals are not open-source repository content.

See [Security](SECURITY.md) and [Configuration](docs/configuration.md).

## For developers

Run the shared CLI without installing the package:

```bash
python3 skills/ads-plan-monitor/run.py --help
python3 skills/qc-plan-monitor/run.py --help
```

Or install an editable package:

```bash
python3 -m venv .venv
source .venv/bin/activate              # Windows: .venv\Scripts\activate
python3 -m pip install -e ".[dev]"
ocean-watch --help
```

The shared implementation lives in `skills/ads-plan-monitor/src/ocean_watch/`. See [Architecture](docs/architecture.md) for module boundaries.

## Documentation

- [Documentation index](docs/README.md)
- [Getting started](docs/getting-started.md) (Chinese)
- [CLI reference](docs/cli.md) (Chinese)
- [Configuration](docs/configuration.md) (Chinese)
- [Architecture](docs/architecture.md) (Chinese)
- [Release guide](docs/releasing.md) (Chinese)
- [Contributing](CONTRIBUTING.md)
- [Changelog](CHANGELOG.md)

## Quality checks

```bash
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tests -v
ruff check skills/ads-plan-monitor/src tests
bandit -q --severity-level medium -r skills/ads-plan-monitor/src/ocean_watch
python3 -m build
git diff --check
```

CI covers Python `3.9` and `3.12` on Windows, macOS, and Linux, including first-run checks from an installed wheel.

## License

MIT
