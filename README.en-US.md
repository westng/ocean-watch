# ocean-watch

[中文](README.md) | English

Ocean Watch is a Codex Plugin for Ocean Engine OAuth, responsible accounts, templates, materials, plans, reports, and delivery analysis.

| Skill | Channel | Capabilities |
| --- | --- | --- |
| `ads-plan-monitor` | Ocean Engine Marketing | OAuth, responsible accounts, uploaded/creator materials, templates, plans, reports, strategy |
| `qc-plan-monitor` | Qianchuan | OAuth, responsible accounts, product/live templates, creator works, all-domain plans and reports |

## Current Implementation

The Go cutover is complete; Ocean Watch is no longer in a dual-runtime or Shadow migration stage. The repository and Plugin retain one advertising-business implementation: the bundled Go CLI owns OAuth, authorized-advertiser synchronization, responsible accounts, templates, materials, plans, reports, local state, and write reconciliation. The former Python business package, Go prototype, Shadow routing, runtime selection, business fallback, MCP compatibility entry points, and migration Gate/Bootstrap assets are no longer distributed.

Python is not a second business runtime. It only launches pinned F2 `0.0.1.7` for public Douyin metadata used by Qianchuan work-link flows. F2 output is an untrusted targeting hint; official Qianchuan APIs under the requested advertiser still determine creator authorization, ownership, product matching, and deliverability.

## Runtime

```text
Codex → Skill → run/run.cmd → bundled Go CLI → official Ocean Engine API
                                      └→ Python 3.10+ → F2 0.0.1.7
                                         Douyin public metadata only
```

The Plugin ships native binaries for macOS Intel and Apple Silicon, Linux x86_64 and ARM64, and Windows x86_64. Users do not need a Go toolchain. Python is not a business runtime; it is required only for Qianchuan work-link metadata through pinned F2. Official Qianchuan APIs still verify authorization, ownership, product matching, and deliverability.

## Install

Requires Codex CLI `0.144.1+`. F2-dependent work-link flows also require Python `3.10+` and F2 `0.0.1.7`.

```bash
codex plugin marketplace add westng/ocean-watch
codex plugin add ocean-watch@ocean-watch
```

Start a new Codex task after installation or upgrade. Describe the desired outcome naturally; the Skills select the channel and preview every online write before adding `--submit`.

Manual first-use checks:

```bash
skills/ads-plan-monitor/run setup doctor
skills/ads-plan-monitor/run setup init --home-config
```

Use `run.cmd` on Windows. See the Chinese [getting-started guide](docs/getting-started.md), [architecture](docs/architecture.md), and [security policy](SECURITY.md).

## Development

```bash
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go test ./...
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go vet ./...
python3 -m unittest discover -s f2 -p 'test_resolve.py' -v
python3 scripts/version_tag.py check
python3 scripts/validate_distribution.py
GOTOOLCHAIN=go1.26.5 go -C runtime/ocean-watch-go run ./cmd/build-runtime --all --verify
```

Online creates, appends, deletes, and setting updates remain dry-run unless the user explicitly authorizes submission.
