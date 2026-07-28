# Skill evaluation contract

`cases.json` is a semantic contract, not a keyword allow-list. Each case contains
one or more conversation turns and the command boundary that a model should select.
The evaluator checks the selected route, channel, forbidden side effects, and the
mandatory Presentation protocol. It must not require the user to repeat a canonical
phrase.

## Driver protocol

`run_skill_eval.py --driver-command` starts one process per case and sends one JSON
document on stdin:

```json
{
  "case": {"id": "membership-common-account", "case_set": "responsible-account-membership", "turns": []},
  "skill_roots": ["skills/ads-plan-monitor", "skills/qc-plan-monitor"],
  "model": "model-snapshot",
  "plugin_version": "0.0.0+codex.example"
}
```

The driver must print exactly one JSON object. It may be backed by Codex, another
model gateway, or a deterministic fixture harness. The minimum result is:

```json
{
  "model": "model-snapshot",
  "codex_version": "codex-version",
  "plugin_version": "0.0.0+codex.example",
  "tool_calls": [{"skill": "ads-plan-monitor", "command": "accounts list", "channel": "any"}],
  "assistant_response": "...",
  "presentation": {
    "required": true,
    "source": "rendered_markdown",
    "rendered_markdown": "..."
  }
}
```

`tool_calls` are the observed or planned local tool invocations. A production
Codex driver must capture the actual tool event and redact it before returning it.
The runner records model, Codex, Plugin, repository commit, reasoning settings,
case ID, and a redacted trace. It never writes credentials, official responses, or
dynamic MCP URLs to evidence.

The driver never receives the case's `expected` object. Expected commands and
Presentation assertions remain private to the runner so the model cannot pass by
copying the answer instead of interpreting the conversation.

Without a driver, the runner performs contract/schema validation only. Release
candidate jobs must provide a controlled model driver and run at least three trials.
The bundled Codex driver ignores user configuration by default. A maintainer whose
fixed evaluation provider is defined only in the local Codex config may set
`OCEAN_WATCH_EVAL_USE_USER_CONFIG=1`; that exception and provider identity must be
recorded with the evidence, and the driver still runs in a sealed read-only fixture.
