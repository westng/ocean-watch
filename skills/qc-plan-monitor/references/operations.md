# Trusted Qianchuan execution

Use only after a successful work preflight and an explicit user request to proceed. Each advertiser/template child is independent.

1. Call `prepare_qianchuan_operation` with that child's `preflight_id`. Core revalidates official ownership/products and target/material facts and saves exact payloads. Show the complete returned `summary` alongside the original required preflight presentation. A preview is not permission to execute.
2. Call `request_operation_confirmation` with the returned `operation_id`, `version`, `snapshot_digest`. The user must approve the separate local Broker dialog. Cancellation, rejection, timeout and unavailable UI never authorize writes. Do not ask the model or shell to approve.
3. Pass those exact references and the returned `approval_id` to `execute_operation`. No payload, new target, provider, nonce, or boolean permission is accepted. The instance must already be in explicit `execute` mode; tools cannot elevate it.
4. Inspect `get_operation` after any error or cancellation. Show each item's applied flag and dispatch state. For `unknown` or interrupted execution, call `reconcile_operation`: it only reads back official facts, persists results and never resubmits. An active lease may require waiting for its bounded expiry; do not retry writes.
5. Undispatched work after partial success needs a new preflight and approval. An unresolved same-group operation blocks a new write; keep the old operation reference. Absence of a plan in one response never proves a timed-out create was not applied.

The preview supports product creation of at most 100 works per new group and append batches of at most 100 per request. Oversized confirmation documents and unsupported batches fail before approval; do not silently split an already approved operation. New plan names include a fixed operation suffix for readback. Budget is displayed as the exact `budget` API value; its display-unit evidence must be checked during real acceptance before declaring production-ready money presentation.

These five tools change the host contract and require reload/new chat. Actual host compatibility and human interaction still require acceptance. Never replace a missing tool with CLI writes. Windows currently has no trusted Broker.
