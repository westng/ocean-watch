#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PYTHON="${PYTHON:-$ROOT/.venv/bin/python}"
if [[ ! -x "$PYTHON" ]]; then
  PYTHON="python3"
fi
GO_MODULE="$ROOT/prototype/ocean-watch-go"

SUITE="all"
EXTRA=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --suite)
      SUITE="$2"
      shift 2
      ;;
    *)
      EXTRA+=("$1")
      shift
      ;;
  esac
done

run_python_contracts() {
  PYTHONPATH="$ROOT/skills/ads-plan-monitor/src" "$PYTHON" -m unittest -q \
    tests.test_migration_contracts \
    tests.test_state_compatibility \
    tests.test_skill_eval_contracts \
    tests.test_evidence_redaction \
    tests.test_g0_evidence \
    tests.test_ac_runner \
    tests.test_final_acceptance \
    tests.test_acceptance_docs \
    tests.test_p5_acceptance \
    tests.test_plugin_metadata
}

run_bootstrap() {
  GOTOOLCHAIN=go1.26.5 go -C "$ROOT/prototype/runtime-bootstrap" test ./...
  GOTOOLCHAIN=go1.26.5 go -C "$ROOT/prototype/runtime-bootstrap" test -race ./...
  "$PYTHON" "$ROOT/scripts/acceptance/build_bootstrap_matrix.py"
}

run_p5() {
  local command="$1"
  shift
  "$PYTHON" "$ROOT/scripts/acceptance/p5.py" "$command" "$@"
}

run_contracts() {
  local git_sha evidence_root candidate
  git_sha="$(git -C "$ROOT" rev-parse HEAD)"
  evidence_root="${OCEAN_WATCH_EVIDENCE_DIR:-$ROOT/artifacts/go-sdk-acceptance/$git_sha/contracts}"
  candidate="$evidence_root/bin/ocean-watch"
  if [[ "${OS:-}" == "Windows_NT" ]]; then
    candidate="${candidate}.exe"
  fi
  mkdir -p "$evidence_root/bin"
  GOTOOLCHAIN=go1.26.5 go -C "$GO_MODULE" test ./internal/contractrunner ./internal/cli ./internal/adapters/filesystem
  GOTOOLCHAIN=go1.26.5 go -C "$GO_MODULE" build -o "$candidate" ./cmd/ocean-watch
  GOTOOLCHAIN=go1.26.5 go -C "$GO_MODULE" run ./cmd/contract-runner capture-python \
    --manifest "$ROOT/contracts/commands.yaml" \
    --fixtures "$ROOT/testdata/contracts/python" \
    --out "$evidence_root/python" \
    --git-sha "$git_sha" \
    --python "$PYTHON"
  GOTOOLCHAIN=go1.26.5 go -C "$GO_MODULE" run ./cmd/contract-runner compare \
    --manifest "$ROOT/contracts/commands.yaml" \
    --baseline "$evidence_root/python" \
    --candidate "$candidate" \
    --out "$evidence_root/go" \
    --git-sha "$git_sha" \
    --python "$PYTHON"
  "$PYTHON" "$ROOT/scripts/acceptance/scan_evidence.py" "$evidence_root"
}

case "$SUITE" in
  baseline)
    run_python_contracts
    ;;
  state)
    "$PYTHON" "$ROOT/scripts/acceptance/probe_state_compatibility.py" "${EXTRA[@]}"
    ;;
  skill-eval)
    "$PYTHON" "$ROOT/scripts/acceptance/run_skill_eval.py" "${EXTRA[@]}"
    ;;
  bootstrap)
    run_bootstrap
    ;;
  launcher)
    run_p5 launcher "${EXTRA[@]}"
    ;;
  contracts)
    run_contracts
    ;;
  native-candidate)
    "$PYTHON" "$ROOT/scripts/acceptance/native_candidate.py" "${EXTRA[@]}"
    ;;
  verify-candidate)
    run_p5 verify-candidate "${EXTRA[@]}"
    ;;
  supply-chain)
    run_p5 supply-chain "${EXTRA[@]}"
    ;;
  upgrade-rollback)
    run_p5 upgrade-rollback "${EXTRA[@]}"
    ;;
  user-journey)
    run_p5 user-journey "${EXTRA[@]}"
    ;;
  canary)
    run_p5 canary "${EXTRA[@]}"
    ;;
  ac)
    "$PYTHON" "$ROOT/scripts/acceptance/ac.py" "${EXTRA[@]}"
    ;;
  final-summary)
    "$PYTHON" "$ROOT/scripts/acceptance/build_final_summary.py" "${EXTRA[@]}"
    ;;
  verify-gate)
    "$PYTHON" "$ROOT/scripts/acceptance/verify_gate_signoff.py" "${EXTRA[@]}"
    ;;
  all)
    run_python_contracts
    "$PYTHON" "$ROOT/scripts/acceptance/probe_state_compatibility.py" \
      --out "$ROOT/artifacts/go-sdk-acceptance/p0/state-probe.json"
    "$PYTHON" "$ROOT/scripts/acceptance/run_skill_eval.py" \
      --allow-not-run \
      --out "$ROOT/artifacts/go-sdk-acceptance/p0/skill-contract.json"
    "$PYTHON" "$ROOT/scripts/acceptance/check_docs_links.py"
    run_bootstrap
    run_contracts
    "$PYTHON" "$ROOT/scripts/acceptance/build_g0_summary.py" \
      --out "$ROOT/artifacts/go-sdk-acceptance/p0/summary.json"
    "$PYTHON" "$ROOT/scripts/acceptance/scan_evidence.py" \
      "$ROOT/artifacts/go-sdk-acceptance/p0"
    ;;
  *)
    echo "unknown suite: $SUITE" >&2
    exit 2
    ;;
esac
