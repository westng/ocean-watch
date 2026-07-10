# Ocean Watch Plugin Migration Design

> Organization: westng
> Project: ocean-watch
> Plugin: ocean-watch
> Skill: ads-plan-monitor

## Goal

Turn the repository into one installable Codex Plugin while preserving `ads-plan-monitor` as one Skill with setup, creation, query, and strategy branches.

## Structure

The repository root is the plugin and marketplace root. Runtime Skill files live under `skills/ads-plan-monitor/`; project documentation and tests remain at the repository root. Private `config/`, `runs/`, virtual environments, and credentials remain untracked.

## Configuration

Explicit `--config` and `ADS_PLAN_MONITOR_CONFIG` keep highest priority. A Git development checkout may use `<repo>/config/ads-plan-monitor/config.json`; an installed plugin defaults to `~/.codex/ads-plan-monitor/config.json`. OAuth credentials remain in the operating system credential store.

## Official Documentation MCP

The official MCP URL requires each user's matching `app_id` and `developer_id`, and the endpoint uses legacy SSE while current Codex URL registrations use Streamable HTTP. The first-run workflow verifies `initialize` and `tools/list`, stores `developer_id` with the existing local credential backend, and registers a bundled SSE-to-stdio bridge as `oceanengine-developer-docs`. The sensitive URL exists only in bridge-process memory. Status output never prints the URL or identifiers. The Skill uses the MCP for official documentation, schema, and SDK-example lookups, and falls back to bundled references when unavailable.

## Compatibility And Safety

All scripts use Python standard-library APIs and retain macOS Keychain, Windows DPAPI, and Linux Secret Service support. API write operations still require explicit user intent. Tests cover path resolution, MCP registration behavior, credential redaction, payload generation, token refresh, and advertiser expansion.
