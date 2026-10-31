# Star Map Setup and Local Accounts

Read this only for explicit setup, credential lifecycle, or local managed-account changes. Ordinary account, authorization, and sync requests use the MCP routes in `SKILL.md`.

## Local CLI

Use this Skill's resolved launcher, always with `--channel star_map` for channel operations. WorkBuddy does not execute these CLI routes.

```bash
./run setup doctor
./run setup init --home-config
./run auth authorize --channel star_map --print-url --no-open
./run auth status --channel star_map
./run auth mappings --channel star_map
./run accounts list --channel star_map
```

Only run initialization or authorization when requested. For missing App configuration, the CLI authorization flow uses a local form and the OS credential store; do not put secrets in model-visible arguments. Keep the CLI running until the official callback and account sync complete. The registered callback URI is `http://127.0.0.1:8787/oauth/callback`.

Use the unique capability route and its `--help` for explicitly requested local-account mutations or credential refresh/revocation. Preserve account identity, channel, and exact grant; do not apply shared template/report capabilities to Star Map. Runtime CLI options may retain the legacy `advertiser_id` name, but the subject is a Star Map account.

## State and Recovery

Use the shared state root resolved from `OCEAN_WATCH_HOME`, `CODEX_HOME`, then `~/.codex`. Do not copy credentials into a separate host state directory or reuse another channel's grant.

Failed discovery after saving credentials retains the grant in pending-sync state. Resume account sync for its exact authorization ID; a new OAuth exchange is unnecessary. The local managed-account registry and the authorization snapshot remain distinct.

OAuth session state is in memory. After Runtime restart create a new session; an already saved grant can still resume account synchronization. No Star Map plan/material/report endpoint is added by this Skill.
