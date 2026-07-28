import json
import tempfile
import unittest
from pathlib import Path

from scripts.acceptance import external_evidence, g5_source_policy
from scripts.acceptance.ac import canonical_json, load_manifest

COMMIT = "a" * 40
REPOSITORY = "westng/ocean-watch"
CANDIDATE_IDENTITY = {
    "schema_version": 1,
    "git_sha": COMMIT,
    "product_version": "0.9.1",
    "plugin_version": "0.9.1+codex.test",
    "sdk_version": "v1.1.92",
    "source_tree_sha256": "1" * 64,
    "candidate_checksums_sha256": "2" * 64,
    "release_public_key_sha256": "3" * 64,
    "release": True,
}


class ExternalEvidenceTests(unittest.TestCase):
    def write(self, path: Path, value: dict) -> None:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(canonical_json(value))

    def evidence_for(self, requirement: dict) -> dict:
        kind = requirement["kind"]
        result = {
            "schema_version": 1,
            "git_sha": COMMIT,
            "status": "passed",
            "candidate_identity": CANDIDATE_IDENTITY,
        }
        if kind == "model_eval":
            trials = int(requirement["minimum_trials"])
            result.update(
                trials=trials,
                summary={
                    "total": trials,
                    "passed": trials,
                    "failed": 0,
                    "blocked": 0,
                    "not_run": 0,
                },
            )
        return result

    def create_role_bundle(self, root: Path, role: str) -> None:
        expected = set(g5_source_policy.REQUIRED_EVIDENCE_BY_ROLE[role])
        for contract in load_manifest()["acceptance"].values():
            for requirement in contract.get("external_requirements", []):
                if requirement["evidence"] in expected:
                    self.write(
                        root / requirement["evidence"],
                        self.evidence_for(requirement),
                    )

    def source_run(self, path: Path) -> None:
        self.write(
            path,
            {
                "schema_version": 1,
                "status": "passed",
                "run_id": 41,
                "repository": REPOSITORY,
                "workflow_path": ".github/workflows/model-producer.yml",
                "head_sha": COMMIT,
                "run_attempt": 1,
            },
        )

    def producer_run(self, path: Path) -> None:
        self.write(
            path,
            {
                "schema_version": 1,
                "status": "passed",
                "run_id": 42,
                "repository": REPOSITORY,
                "workflow_path": g5_source_policy.EXTERNAL_WORKFLOW,
                "head_sha": COMMIT,
                "run_attempt": 1,
            },
        )

    def prepare(self, root: Path) -> tuple[Path, Path, Path, Path]:
        evidence_root = root / "evidence"
        identity_path = root / "candidate-identity.json"
        source_run_path = root / "source-run.json"
        producer_run_path = root / "producer-run.json"
        self.create_role_bundle(evidence_root, "model")
        self.write(identity_path, CANDIDATE_IDENTITY)
        self.source_run(source_run_path)
        self.producer_run(producer_run_path)
        external_evidence.attest(
            role="model",
            evidence_root=evidence_root,
            candidate_identity_path=identity_path,
            expected_git_sha=COMMIT,
            repository=REPOSITORY,
            producer_run_id=42,
            producer_run_attempt=1,
            source_run_metadata=source_run_path,
            source_artifact_name="g5-model-source-41",
        )
        return evidence_root, identity_path, source_run_path, producer_run_path

    def test_role_bound_bundle_round_trip(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence_root, identity, _, producer_run = self.prepare(Path(temporary))
            result = external_evidence.verify(
                role="model",
                evidence_root=evidence_root,
                candidate_identity_path=identity,
                expected_git_sha=COMMIT,
                repository=REPOSITORY,
                producer_run_metadata=producer_run,
            )
        self.assertEqual(result["status"], "passed")
        self.assertEqual(result["source_key"], "model_evidence")
        self.assertEqual(len(result["files"]), 3)

    def test_cross_role_file_injection_is_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evidence_root = root / "evidence"
            identity = root / "candidate-identity.json"
            source_run = root / "source-run.json"
            self.create_role_bundle(evidence_root, "model")
            self.create_role_bundle(evidence_root, "canary")
            self.write(identity, CANDIDATE_IDENTITY)
            self.source_run(source_run)
            with self.assertRaisesRegex(
                external_evidence.ExternalEvidenceError,
                "only its fixed evidence files",
            ):
                external_evidence.attest(
                    role="model",
                    evidence_root=evidence_root,
                    candidate_identity_path=identity,
                    expected_git_sha=COMMIT,
                    repository=REPOSITORY,
                    producer_run_id=42,
                    producer_run_attempt=1,
                    source_run_metadata=source_run,
                    source_artifact_name="g5-model-source-41",
                )

    def test_wrong_role_and_attestation_tampering_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence_root, identity, _, producer_run = self.prepare(Path(temporary))
            with self.assertRaisesRegex(
                external_evidence.ExternalEvidenceError,
                "fixed file set",
            ):
                external_evidence.verify(
                    role="canary",
                    evidence_root=evidence_root,
                    candidate_identity_path=identity,
                    expected_git_sha=COMMIT,
                    repository=REPOSITORY,
                    producer_run_metadata=producer_run,
                )

            attestation_path = evidence_root / g5_source_policy.attestation_path("model")
            attestation = json.loads(attestation_path.read_text(encoding="utf-8"))
            attestation["role"] = "canary"
            self.write(attestation_path, attestation)
            with self.assertRaisesRegex(
                external_evidence.ExternalEvidenceError,
                "attestation role differs",
            ):
                external_evidence.verify(
                    role="model",
                    evidence_root=evidence_root,
                    candidate_identity_path=identity,
                    expected_git_sha=COMMIT,
                    repository=REPOSITORY,
                    producer_run_metadata=producer_run,
                )

    def test_evidence_and_producer_workflow_tampering_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence_root, identity, _, producer_run = self.prepare(Path(temporary))
            evidence_path = evidence_root / "contracts/ac-103-skill-eval.json"
            evidence = json.loads(evidence_path.read_text(encoding="utf-8"))
            evidence["summary"]["failed"] = 1
            self.write(evidence_path, evidence)
            with self.assertRaisesRegex(
                external_evidence.ExternalEvidenceError,
                "failed trials",
            ):
                external_evidence.verify(
                    role="model",
                    evidence_root=evidence_root,
                    candidate_identity_path=identity,
                    expected_git_sha=COMMIT,
                    repository=REPOSITORY,
                    producer_run_metadata=producer_run,
                )

    def test_wrong_source_artifact_and_cross_role_run_reuse_are_rejected(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            evidence_root = root / "evidence"
            identity = root / "candidate-identity.json"
            source_run = root / "source-run.json"
            self.create_role_bundle(evidence_root, "model")
            self.write(identity, CANDIDATE_IDENTITY)
            self.source_run(source_run)
            with self.assertRaisesRegex(
                external_evidence.ExternalEvidenceError,
                "role policy",
            ):
                external_evidence.attest(
                    role="model",
                    evidence_root=evidence_root,
                    candidate_identity_path=identity,
                    expected_git_sha=COMMIT,
                    repository=REPOSITORY,
                    producer_run_id=42,
                    producer_run_attempt=1,
                    source_run_metadata=source_run,
                    source_artifact_name="wrong-artifact",
                )

        verifications = []
        for index, role in enumerate(g5_source_policy.REQUIRED_EVIDENCE_BY_ROLE, 1):
            source_run_id = 41 if index < 3 else 40 + index
            verifications.append(
                {
                    "schema_version": 1,
                    "status": "passed",
                    "source_key": g5_source_policy.key_for_role(role),
                    "role": role,
                    "producer_run_id": 100 + index,
                    "source_run_id": source_run_id,
                    "source_workflow_path": f".github/workflows/{role}-producer.yml",
                    "source_artifact_name": g5_source_policy.expected_source_artifact_name(
                        role,
                        source_run_id,
                    ),
                    "candidate_identity_sha256": "4" * 64,
                    "files": [],
                }
            )
        with self.assertRaisesRegex(
            external_evidence.ExternalEvidenceError,
            "must be distinct",
        ):
            external_evidence.verify_set(verifications)

        with tempfile.TemporaryDirectory() as temporary:
            evidence_root, identity, _, producer_run = self.prepare(Path(temporary))
            metadata = json.loads(producer_run.read_text(encoding="utf-8"))
            metadata["workflow_path"] = ".github/workflows/arbitrary.yml"
            self.write(producer_run, metadata)
            with self.assertRaisesRegex(
                external_evidence.ExternalEvidenceError,
                "trusted producer",
            ):
                external_evidence.verify(
                    role="model",
                    evidence_root=evidence_root,
                    candidate_identity_path=identity,
                    expected_git_sha=COMMIT,
                    repository=REPOSITORY,
                    producer_run_metadata=producer_run,
                )


if __name__ == "__main__":
    unittest.main()
