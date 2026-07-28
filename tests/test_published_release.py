import json
import tempfile
import unittest
from pathlib import Path

from scripts.release.verify_published_release import PublishedReleaseError, verify


class PublishedReleaseTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.candidate = self.root / "candidate"
        self.published = self.root / "published"
        self.candidate.mkdir()
        self.published.mkdir()
        for directory in (self.candidate, self.published):
            (directory / "asset.bin").write_bytes(b"release asset")
            (directory / "checksums.json").write_text("{}", encoding="utf-8")
        self.notes = self.root / "notes.md"
        self.notes.write_text("## 0.9.1 - 2026-07-16\n\n- Released.\n", encoding="utf-8")
        self.metadata = self.root / "metadata.json"
        self.commit = "1" * 40
        self.write_metadata()

    def write_metadata(self, **overrides):
        value = {
            "tagName": "v0.9.1",
            "name": "v0.9.1",
            "targetCommitish": self.commit,
            "isDraft": False,
            "isPrerelease": False,
            "body": "## 0.9.1 - 2026-07-16\n\n- Released.",
            "assets": [{"name": "asset.bin"}, {"name": "checksums.json"}],
        }
        value.update(overrides)
        self.metadata.write_text(json.dumps(value), encoding="utf-8")

    def test_exact_existing_release_is_idempotent(self):
        result = verify(
            self.candidate,
            self.published,
            self.metadata,
            self.notes,
            "v0.9.1",
            self.commit,
        )
        self.assertEqual(result["asset_count"], 2)
        self.assertEqual(result["asset_differences"], [])
        self.assertTrue(result["notes_match"])

    def test_changed_asset_is_rejected_without_overwrite(self):
        (self.published / "asset.bin").write_bytes(b"changed")
        with self.assertRaisesRegex(PublishedReleaseError, "assets differ"):
            verify(
                self.candidate,
                self.published,
                self.metadata,
                self.notes,
                "v0.9.1",
                self.commit,
            )

    def test_changed_identity_or_notes_is_rejected(self):
        self.write_metadata(targetCommitish="2" * 40, body="generated notes")
        with self.assertRaises(PublishedReleaseError):
            verify(
                self.candidate,
                self.published,
                self.metadata,
                self.notes,
                "v0.9.1",
                self.commit,
            )


if __name__ == "__main__":
    unittest.main()
