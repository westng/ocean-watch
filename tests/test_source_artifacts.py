import copy
import tempfile
import unittest
from pathlib import Path

from scripts.acceptance import g5_source_policy, source_artifacts, source_runs


class SourceArtifactTests(unittest.TestCase):
    def plan(self) -> dict:
        return {
            key: index + 1
            for index, key in enumerate(source_runs.RUN_KEYS)
        }

    def test_plan_requires_six_distinct_explicit_sources(self):
        result = source_artifacts.normalize(self.plan())
        self.assertEqual(list(result), list(source_runs.RUN_KEYS))
        self.assertEqual(
            result["formal_evidence"]["workflow_path"],
            g5_source_policy.FORMAL_WORKFLOW,
        )
        self.assertEqual(result["formal_evidence"]["artifact_name"], "-")
        self.assertEqual(
            result["model_evidence"],
            {
                "run_id": 2,
                "workflow_path": g5_source_policy.EXTERNAL_WORKFLOW,
                "artifact_name": "g5-model-evidence-2",
            },
        )

        changed = self.plan()
        changed["canary_evidence"] = changed["model_evidence"]
        with self.assertRaisesRegex(source_artifacts.SourceArtifactError, "distinct"):
            source_artifacts.normalize(changed)

    def test_plan_rejects_caller_controlled_workflow_and_artifact_fields(self):
        changed = self.plan()
        changed["formal_evidence"] = {
            "run_id": 1,
            "workflow_path": ".github/workflows/arbitrary.yml",
            "artifact_name": "wrong-role",
        }
        with self.assertRaisesRegex(source_artifacts.SourceArtifactError, "run ID"):
            source_artifacts.normalize(changed)

        changed = self.plan()
        changed["canary_evidence"] = True
        with self.assertRaisesRegex(source_artifacts.SourceArtifactError, "run ID"):
            source_artifacts.normalize(changed)

        changed = self.plan()
        changed["marketplace_evidence"] = -1
        with self.assertRaisesRegex(source_artifacts.SourceArtifactError, "run ID"):
            source_artifacts.normalize(changed)

    def test_tsv_is_safe_and_ordered(self):
        plan = source_artifacts.normalize(copy.deepcopy(self.plan()))
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "plan.tsv"
            source_artifacts.write_tsv(path, plan)
            rows = path.read_text(encoding="utf-8").splitlines()
        self.assertEqual(len(rows), 6)
        self.assertTrue(rows[0].startswith("formal_evidence\t1\t"))
        self.assertTrue(rows[-1].startswith("rollout_evidence\t6\t"))


if __name__ == "__main__":
    unittest.main()
