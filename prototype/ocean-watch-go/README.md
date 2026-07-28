# Ocean Watch Go Runtime (isolated migration module)

This module is the implementation workbench for the P1-P5 migration. It is
intentionally isolated from the production Python launchers until the documented
G0/G1 gates pass.

The module owns the stable CLI envelope, local state compatibility, immutable
route manifest, contract runner, and the official SDK adapter boundary. Commands
without an approved Go implementation are delegated to the existing Python
entrypoint without changing arguments, environment, or exit status.

Run the prototype with:

```text
go run ./cmd/ocean-watch accounts list --config /path/to/config.json
```

Set `OCEAN_WATCH_PYTHON_ENTRYPOINT` when testing the fallback from a checkout
whose working directory is not the repository root.

Run the Python-to-Go contract suite from the repository root:

```text
scripts/acceptance/run.sh --suite contracts
```

The suite uses only synthetic fixtures, blocks proxy-based network access, and
compares process output, mandatory Presentation Markdown, exit status, and local
filesystem side effects before scanning its evidence for credential material.
