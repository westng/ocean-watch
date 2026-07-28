import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def load_workflow(name: str) -> dict:
    value = yaml.load(
        (ROOT / ".github" / "workflows" / name).read_text(encoding="utf-8"),
        Loader=yaml.BaseLoader,
    )
    if not isinstance(value, dict):
        raise AssertionError(f"workflow is not an object: {name}")
    return value


def dispatch_inputs(workflow: dict) -> dict:
    trigger = workflow.get("on")
    if not isinstance(trigger, dict):
        return {}
    dispatch = trigger.get("workflow_dispatch")
    if not isinstance(dispatch, dict):
        return {}
    inputs = dispatch.get("inputs")
    return inputs if isinstance(inputs, dict) else {}


class G5WorkflowTests(unittest.TestCase):
    def test_all_manual_workflows_respect_dispatch_input_limit(self):
        for path in sorted((ROOT / ".github" / "workflows").glob("*.yml")):
            workflow = load_workflow(path.name)
            self.assertLessEqual(len(dispatch_inputs(workflow)), 10, path.name)

    def test_prepare_and_seal_use_explicit_distinct_sources(self):
        workflow = (ROOT / ".github/workflows/g5-seal.yml").read_text(
            encoding="utf-8"
        )
        self.assertEqual(
            set(dispatch_inputs(load_workflow("g5-seal.yml"))),
            {"mode", "source_run_ids_json", "prepared_run_id", "signoff_run_id"},
        )
        self.assertIn("source_artifacts.py", workflow)
        self.assertIn("source_runs.py", workflow)
        self.assertIn("verify_workflow_run.py", workflow)
        self.assertIn("merge_evidence.py", workflow)
        self.assertIn("external_evidence.py verify", workflow)
        self.assertIn("--require-ready", workflow)
        self.assertIn("--reject-tracked-signoff", workflow)
        self.assertIn("sealed_release.py seal", workflow)
        self.assertIn("sealed_release.py verify", workflow)
        self.assertIn("g5-prepared-${{ github.sha }}-${{ github.run_id }}", workflow)
        self.assertIn("g5-sealed-${{ github.sha }}-${{ github.run_id }}", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_SIGNING_KEY", workflow)
        self.assertNotIn("G5_SIGNOFF_JSON_BASE64", workflow)
        self.assertNotIn('"approvals"', workflow)
        self.assertNotIn("source_artifacts_json", workflow)

    def test_external_intake_is_the_only_external_role_producer(self):
        workflow = (ROOT / ".github/workflows/g5-external-evidence.yml").read_text(
            encoding="utf-8"
        )
        self.assertEqual(
            set(dispatch_inputs(load_workflow("g5-external-evidence.yml"))),
            {"role", "formal_run_id", "source_run_id"},
        )
        role = dispatch_inputs(load_workflow("g5-external-evidence.yml"))["role"]
        self.assertEqual(
            role["options"],
            ["model", "canary", "marketplace", "rollback", "rollout"],
        )
        self.assertIn("environment: g5-external-evidence", workflow)
        self.assertIn("external_evidence.py attest", workflow)
        self.assertIn('SOURCE_ARTIFACT_NAME="g5-${EVIDENCE_ROLE}-source-${SOURCE_RUN_ID}"', workflow)
        self.assertIn("--expected-workflow-path .github/workflows/g5-evidence.yml", workflow)
        self.assertIn("g5-${{ inputs.role }}-evidence-${{ github.run_id }}", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_SIGNING_KEY", workflow)

    def test_signoff_workflow_only_consumes_external_restricted_approvals(self):
        workflow = (ROOT / ".github/workflows/g5-signoff.yml").read_text(
            encoding="utf-8"
        )
        self.assertEqual(
            set(dispatch_inputs(load_workflow("g5-signoff.yml"))),
            {"prepared_run_id"},
        )
        self.assertIn("environment: g5-independent-signoff", workflow)
        self.assertIn("secrets.G5_SIGNOFF_JSON_BASE64", workflow)
        self.assertIn("verify_gate_signoff.py", workflow)
        self.assertIn("--reject-tracked-signoff", workflow)
        self.assertIn("g5-signoff-${{ github.sha }}-${{ github.run_id }}", workflow)
        self.assertNotIn("OCEAN_WATCH_RELEASE_SIGNING_KEY", workflow)
        self.assertNotIn('"decision": "approved"', workflow)
        self.assertNotIn('"role":', workflow)

    def test_signing_secret_exists_only_in_formal_candidate_build(self):
        workflow = (ROOT / ".github/workflows/g5-evidence.yml").read_text(
            encoding="utf-8"
        )
        build_job, remaining = workflow.split("\n  native-formal-candidate:\n", 1)
        self.assertIn("environment: g5-release-candidate", build_job)
        self.assertEqual(build_job.count("OCEAN_WATCH_RELEASE_SIGNING_KEY"), 2)
        self.assertNotIn("OCEAN_WATCH_RELEASE_SIGNING_KEY", remaining)


if __name__ == "__main__":
    unittest.main()
