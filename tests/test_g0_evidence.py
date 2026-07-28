import hashlib
import tempfile
import unittest
from pathlib import Path

import yaml

from scripts.acceptance.build_g0_summary import build_summary, encode_summary


class G0EvidenceTests(unittest.TestCase):
    def make_status(self, root, *, evidence=None, blockers=None):
        status = {
            "schema_version": 1,
            "branch": "codex/test",
            "stage": "P0",
            "tasks": {
                "P0-01": {
                    "status": "complete_automated",
                    "evidence": evidence or ["proof.txt"],
                }
            },
            "g0": {"blockers": blockers or []},
        }
        path = root / "contracts" / "p0-status.yaml"
        path.parent.mkdir(parents=True)
        path.write_text(yaml.safe_dump(status, sort_keys=False), encoding="utf-8")
        return path

    def test_summary_is_deterministic_and_changes_with_evidence(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            proof = root / "proof.txt"
            proof.write_text("first\n", encoding="utf-8")
            status = self.make_status(root)
            first = build_summary(root, status, git_commit="a" * 40, dirty=False)
            second = build_summary(root, status, git_commit="a" * 40, dirty=False)
            self.assertTrue(first["ready"])
            self.assertEqual(encode_summary(first), encode_summary(second))
            first_digest = hashlib.sha256(encode_summary(first)).hexdigest()
            proof.write_text("second\n", encoding="utf-8")
            changed = build_summary(root, status, git_commit="a" * 40, dirty=False)
            self.assertNotEqual(
                first_digest,
                hashlib.sha256(encode_summary(changed)).hexdigest(),
            )

    def test_missing_evidence_and_dirty_tree_block_readiness(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            status = self.make_status(root, evidence=["missing.json"])
            summary = build_summary(root, status, git_commit="a" * 40, dirty=True)
            self.assertFalse(summary["ready"])
            self.assertIn(
                "working tree is dirty; evidence is not bound to an immutable commit",
                summary["blockers"],
            )
            self.assertIn("missing evidence: missing.json", summary["blockers"])

    def test_evidence_path_cannot_escape_repository(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            status = self.make_status(root, evidence=["../outside.txt"])
            with self.assertRaisesRegex(ValueError, "escapes repository root"):
                build_summary(root, status, git_commit="a" * 40, dirty=False)


if __name__ == "__main__":
    unittest.main()
