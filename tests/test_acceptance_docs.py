import copy
import hashlib
import json
import unittest

from scripts.acceptance.build_g0_summary import encode_summary
from scripts.acceptance.check_docs_links import ROOT, check
from scripts.acceptance.verify_g0_signoff import verify


class AcceptanceDocumentationTests(unittest.TestCase):
    def test_local_documentation_links_resolve(self):
        paths = sorted((ROOT / "docs").rglob("*.md"))
        self.assertEqual(check(paths), [])

    def test_current_architecture_docs_reject_retired_descriptions(self):
        readme = (ROOT / "README.md").read_text(encoding="utf-8")
        english = (ROOT / "README.en-US.md").read_text(encoding="utf-8")
        cli = (ROOT / "docs" / "cli.md").read_text(encoding="utf-8")
        plugin = json.loads(
            (ROOT / ".codex-plugin" / "plugin.json").read_text(encoding="utf-8")
        )
        go_runtime = (ROOT / "prototype" / "ocean-watch-go" / "README.md").read_text(
            encoding="utf-8"
        )
        bootstrap = (ROOT / "prototype" / "runtime-bootstrap" / "README.md").read_text(
            encoding="utf-8"
        )

        self.assertNotIn("├── setup", readme)
        self.assertNotIn("├── setup", english)
        self.assertIn("CLI 分组是迁移期兼容合同", readme)
        self.assertIn("CLI groups are a migration compatibility contract", english)
        self.assertIn("它不是领域架构图", cli)
        self.assertNotIn("Qianchuan MCP data", plugin["description"])
        self.assertNotIn("G0/G1 gates pass", go_runtime)
        self.assertNotIn("P0-05 feasibility proof", bootstrap)
        self.assertIn("historical", go_runtime)
        self.assertIn("historical", bootstrap)

    def test_migration_documentation_has_single_authoritative_set(self):
        retained = (
            "docs/architecture.md",
            "docs/go-sdk-migration-matrix.md",
            "docs/releasing.md",
            "docs/adr/0001-platform-bootstrap.md",
            "contracts/README.md",
            "contracts/acceptance/ac-manifest.yaml",
        )
        retired = (
            "docs/go-sdk-target-architecture.md",
            "docs/go-sdk-execution-plan.md",
            "docs/go-sdk-acceptance-plan.md",
            "docs/go-sdk-release-rfc.md",
            "docs/go-sdk-threat-model.md",
        )

        for path in retained:
            with self.subTest(retained=path):
                self.assertTrue((ROOT / path).is_file())
        for path in retired:
            with self.subTest(retired=path):
                self.assertFalse((ROOT / path).exists())

    def test_p0_status_references_existing_tracked_paths(self):
        import yaml

        status = yaml.safe_load((ROOT / "contracts" / "p0-status.yaml").read_text(encoding="utf-8"))
        for task in status["tasks"].values():
            for evidence in task.get("evidence", []):
                if evidence.startswith("artifacts/"):
                    continue
                with self.subTest(evidence=evidence):
                    self.assertTrue((ROOT / evidence).exists())

    def test_p3_shadow_status_references_existing_paths_and_keeps_routes_python(self):
        import yaml

        status = yaml.safe_load((ROOT / "contracts" / "p3-status.yaml").read_text(encoding="utf-8"))
        self.assertFalse(status["production_route_changed"])
        self.assertEqual(status["g3"]["status"], "automated_ready_external_gate_pending")
        self.assertEqual(status["tasks"]["P3-03"]["status"], "shadow_complete_automated")
        self.assertIn("real read-only canary evidence is absent by development-mode policy", status["g3"]["blockers"])
        for task in status["tasks"].values():
            for evidence in task.get("evidence", []):
                with self.subTest(evidence=evidence):
                    self.assertTrue((ROOT / evidence).exists())

    def test_p4_status_records_shadow_evidence_without_claiming_g4(self):
        import yaml

        status = yaml.safe_load((ROOT / "contracts" / "p4-status.yaml").read_text(encoding="utf-8"))
        self.assertFalse(status["production_route_changed"])
        self.assertEqual(status["g4"]["migration_state"], "Shadow")
        self.assertEqual(status["g4"]["status"], "automated_ready_external_gate_pending")
        self.assertEqual(status["automated_acceptance"]["AC-127"], "external_not_run")
        self.assertIn("AC-127 real canary", status["g4"]["blockers"][0])
        self.assertEqual(
            status["tasks"]["P4-05"]["contracts"]["presentation_columns"],
            ["计划ID", "达人昵称", "商品ID", "素材ID", "素材标题"],
        )
        for task in status["tasks"].values():
            for evidence in task.get("evidence", []):
                with self.subTest(evidence=evidence):
                    self.assertTrue((ROOT / evidence).exists())

    def test_g0_signoff_requires_complete_independent_approvals(self):
        example = json.loads(
            (ROOT / "contracts" / "gates" / "g0-signoff.example.json").read_text(
                encoding="utf-8"
            )
        )
        self.assertTrue(verify(example))
        summary = {
            "schema_version": 1,
            "gate": "G0",
            "git_commit": "a" * 40,
            "status": "ready",
            "ready": True,
            "blockers": [],
        }
        summary_bytes = encode_summary(summary)
        example["git_commit"] = summary["git_commit"]
        example["evidence_sha256"] = hashlib.sha256(summary_bytes).hexdigest()
        for index, approval in enumerate(example["approvals"]):
            approval.update(
                {
                    "decision": "approved",
                    "approver": "reviewer-a" if index < 3 else "reviewer-b",
                    "approved_at": "2026-07-24T00:00:00Z",
                }
            )
        self.assertEqual(verify(example, summary_bytes), [])

        tampered = summary_bytes.replace(b'"ready":true', b'"ready":false')
        self.assertIn(
            "evidence_sha256 does not match the supplied summary",
            verify(example, tampered),
        )
        blocked = copy.deepcopy(summary)
        blocked.update({"status": "blocked", "ready": False, "blockers": ["pending review"]})
        blocked_bytes = encode_summary(blocked)
        example["evidence_sha256"] = hashlib.sha256(blocked_bytes).hexdigest()
        self.assertIn("G0 evidence summary is not ready", verify(example, blocked_bytes))

    def test_g0_signoff_rejects_placeholders_and_naive_timestamps(self):
        example = json.loads(
            (ROOT / "contracts" / "gates" / "g0-signoff.example.json").read_text(
                encoding="utf-8"
            )
        )
        for approval in example["approvals"]:
            approval.update(
                {
                    "decision": "approved",
                    "approver": "reviewer",
                    "approved_at": "2026-07-24T00:00:00",
                }
            )
        errors = verify(example, b"{}\n")
        self.assertIn("git_commit cannot use the all-zero placeholder", errors)
        self.assertIn("evidence_sha256 cannot use the all-zero placeholder", errors)
        self.assertTrue(any("RFC3339" in error for error in errors))

    def test_powershell_all_suite_matches_static_acceptance_scope(self):
        script = (ROOT / "scripts" / "acceptance" / "run.ps1").read_text(
            encoding="utf-8"
        )
        self.assertIn("Run-State -Out", script)
        self.assertIn("Run-SkillEval -AllowNotRun -Out", script)
        self.assertIn("scripts/acceptance/scan_evidence.py", script)
        self.assertIn("scripts/acceptance/check_docs_links.py", script)
        self.assertIn("scripts/acceptance/build_g0_summary.py", script)
        self.assertIn("Run-Contracts", script)
        self.assertIn("cmd/contract-runner capture-python", script)
        self.assertIn("cmd/contract-runner compare", script)

    def test_ci_and_release_workflows_stay_focused(self):
        ci = (ROOT / ".github" / "workflows" / "ci.yml").read_text(encoding="utf-8")
        native_candidate = (ROOT / "scripts" / "acceptance" / "native_candidate.py").read_text(
            encoding="utf-8"
        )
        tag = (ROOT / ".github" / "workflows" / "tag.yml").read_text(encoding="utf-8")
        workflow_names = {
            path.name for path in (ROOT / ".github" / "workflows").glob("*.yml")
        }

        self.assertEqual(workflow_names, {"ci.yml", "tag.yml"})
        self.assertIn("branches: [main]", ci)
        self.assertIn('python-version: "3.12"', ci)
        self.assertIn('python-version: "3.9"', ci)
        self.assertIn("ubuntu-latest", ci)
        self.assertIn("windows-latest", ci)
        self.assertIn("macos-latest", ci)
        self.assertIn("go -C prototype/ocean-watch-go test ./...", ci)
        self.assertIn("go -C prototype/runtime-bootstrap test ./...", ci)
        self.assertNotIn("build-test-candidate", ci)
        self.assertNotIn("native-test-candidate", ci)
        self.assertNotIn("actions/upload-artifact", ci)
        self.assertIn("release/ac-124-platform.json", native_candidate)
        self.assertIn("release/ac-126-upgrade-rollback.json", native_candidate)
        self.assertIn("contracts/ac-128-user-journeys.json", native_candidate)
        self.assertIn("name: Release", tag)
        self.assertIn("workflow_dispatch", tag)
        self.assertIn("scripts/version_tag.py notes", tag)
        self.assertIn('gh release create "${VERSION_TAG}"', tag)
        self.assertNotIn("scripts/release/build_candidate.py", tag)
        self.assertNotIn("sealed_release.py", tag)
        controls = json.loads(
            (ROOT / "contracts" / "security" / "gosec-controls.json").read_text(
                encoding="utf-8"
            )
        )
        self.assertTrue(controls["does_not_grant_gate_signoff"])
        self.assertEqual(controls["scanner_install_version"], "v2.22.10")


if __name__ == "__main__":
    unittest.main()
