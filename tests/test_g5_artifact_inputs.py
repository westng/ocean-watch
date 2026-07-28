import tempfile
import unittest
from pathlib import Path

from scripts.acceptance import (
    g5_source_policy,
    merge_evidence,
    source_runs,
    verify_workflow_run,
)

COMMIT = "a" * 40


class G5ArtifactInputTests(unittest.TestCase):
    def test_evidence_merge_preserves_unique_files_and_identical_duplicates(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = root / "first"
            second = root / "second"
            destination = root / "destination"
            (first / "contracts").mkdir(parents=True)
            (second / "contracts").mkdir(parents=True)
            (first / "contracts" / "a.json").write_bytes(b"{}\n")
            (second / "contracts" / "a.json").write_bytes(b"{}\n")
            (second / "contracts" / "b.json").write_bytes(b"{\"status\":\"passed\"}\n")
            result = merge_evidence.merge([first, second], destination)
            self.assertEqual(result["copied_files"], 2)
            self.assertEqual(result["identical_duplicates"], 1)
            self.assertEqual(result["combined_files"], 2)

    def test_evidence_merge_rejects_conflicts_and_symlinks(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = root / "first"
            second = root / "second"
            first.mkdir()
            second.mkdir()
            (first / "evidence.json").write_bytes(b"first")
            (second / "evidence.json").write_bytes(b"second")
            with self.assertRaisesRegex(merge_evidence.EvidenceMergeError, "conflict"):
                merge_evidence.merge([first, second], root / "destination")
            link_source = root / "link-source"
            link_source.mkdir()
            (link_source / "target").write_bytes(b"target")
            (link_source / "link").symlink_to("target")
            with self.assertRaisesRegex(merge_evidence.EvidenceMergeError, "symlink"):
                merge_evidence.merge([link_source], root / "other-destination")

    def test_source_run_manifest_requires_every_positive_explicit_run(self):
        runs = {
            key: {
                "run_id": index + 1,
                "workflow_path": g5_source_policy.expected_workflow_path(key),
                "head_sha": COMMIT,
                "run_attempt": 1,
            }
            for index, key in enumerate(source_runs.RUN_KEYS)
        }
        result = source_runs.build(COMMIT, "westng/ocean-watch", runs)
        self.assertEqual(list(result["runs"]), list(source_runs.RUN_KEYS))
        runs["formal_evidence"].update(schema_version=1, status="passed")
        normalized = source_runs.build(COMMIT, "westng/ocean-watch", runs)
        self.assertEqual(
            set(normalized["runs"]["formal_evidence"]),
            source_runs.RUN_FIELDS,
        )
        runs["canary_evidence"]["run_id"] = 0
        with self.assertRaisesRegex(source_runs.SourceRunError, "canary_evidence"):
            source_runs.build(COMMIT, "westng/ocean-watch", runs)
        runs["canary_evidence"]["run_id"] = runs["model_evidence"]["run_id"]
        with self.assertRaisesRegex(source_runs.SourceRunError, "must be distinct"):
            source_runs.build(COMMIT, "westng/ocean-watch", runs)

        runs["canary_evidence"]["run_id"] = 3
        runs["canary_evidence"]["workflow_path"] = ".github/workflows/arbitrary.yml"
        with self.assertRaisesRegex(source_runs.SourceRunError, "trusted source policy"):
            source_runs.build(COMMIT, "westng/ocean-watch", runs)

    def test_workflow_run_requires_successful_explicit_same_repository_source(self):
        metadata = {
            "id": 123,
            "repository": {"full_name": "westng/ocean-watch"},
            "event": "workflow_dispatch",
            "status": "completed",
            "conclusion": "success",
            "head_sha": COMMIT,
            "path": ".github/workflows/g5-evidence.yml@refs/heads/main",
            "run_attempt": 1,
        }
        result = verify_workflow_run.verify(
            metadata,
            expected_run_id=123,
            expected_repository="westng/ocean-watch",
            expected_workflow_path=".github/workflows/g5-evidence.yml",
            expected_head_sha=COMMIT,
        )
        self.assertEqual(result["status"], "passed")
        changed = dict(metadata, conclusion="failure")
        with self.assertRaisesRegex(verify_workflow_run.WorkflowRunError, "successfully"):
            verify_workflow_run.verify(
                changed,
                expected_run_id=123,
                expected_repository="westng/ocean-watch",
            )
        changed = dict(metadata, run_attempt=0)
        with self.assertRaisesRegex(verify_workflow_run.WorkflowRunError, "attempt"):
            verify_workflow_run.verify(
                changed,
                expected_run_id=123,
                expected_repository="westng/ocean-watch",
            )


if __name__ == "__main__":
    unittest.main()
