import json
import tempfile
import unittest
from pathlib import Path

from ocean_watch.reports import query_managed_accounts_report

from scripts.acceptance.run_skill_eval import CASES, _assert_result, _validate_cases, run_suite

ROOT = Path(__file__).resolve().parents[1]


def managed_accounts_report_golden():
    rows = [
        {
            "channel": "marketing",
            "advertiser_id": "1001",
            "name": "营销账户",
            "enabled": True,
            "channel_name": "巨量营销",
            "query_status": "ok",
            "spend": 10,
            "orders": 2,
            "gmv": 20,
            "roi": 2,
            "net_orders_1h": 1,
            "net_gmv_1h": 12,
            "net_roi_1h": 1.2,
        },
        {
            "channel": "qianchuan",
            "advertiser_id": "1002",
            "name": "千川账户",
            "enabled": True,
            "channel_name": "巨量千川",
            "query_status": "failed",
            "error": {
                "code": "api_error",
                "message": "remote failure",
                "details": {"code": 40100, "message": "系统请求频率超限"},
            },
        },
    ]
    return query_managed_accounts_report.build_result(
        rows,
        "2026-07-20",
        "2026-07-20",
    )["presentation"]["rendered_markdown"]


class SkillEvalContractTests(unittest.TestCase):
    def test_cases_are_semantic_and_cover_required_intents(self):
        document = json.loads(CASES.read_text(encoding="utf-8"))
        self.assertEqual(_validate_cases(document), [])
        cases = {case["id"]: case for case in document["cases"]}
        self.assertEqual(cases["membership-common-account"]["expected"]["command"], "accounts list")
        self.assertEqual(cases["membership-responsible-account"]["expected"]["command"], "accounts list")
        self.assertEqual(cases["performance-account-spend"]["expected"]["command"], "accounts report")
        self.assertEqual(cases["negative-plan-roi"]["expected"]["command"], "qc-reports plans")
        self.assertGreaterEqual(len(cases["membership-context-follow-up"]["turns"]), 3)

    def test_mandatory_presentation_is_checked_verbatim(self):
        case = next(case for case in json.loads(CASES.read_text(encoding="utf-8"))["cases"] if case["id"] == "qianchuan-batch-presentation")
        rendered = (Path(__file__).resolve().parents[1] / case["expected"]["presentation"]["fixture"]).read_text(encoding="utf-8").rstrip("\n")
        result = {
            "tool_calls": [
                {
                    "skill": "qc-plan-monitor",
                    "command": "plans batch-qianchuan-works",
                    "channel": "qianchuan",
                }
            ],
            "assistant_response": rendered,
            "presentation": {"required": True, "source": "rendered_markdown", "rendered_markdown": rendered},
        }
        self.assertEqual(_assert_result(case, result), [])
        result["assistant_response"] = "简化后的表格"
        self.assertTrue(_assert_result(case, result))

    def test_account_performance_presentation_is_complete_and_builder_owned(self):
        document = json.loads(CASES.read_text(encoding="utf-8"))
        cases = {
            case["id"]: case
            for case in document["cases"]
            if case["case_set"] == "responsible-account-performance"
        }
        fixture = (
            ROOT / "contracts" / "presentation" / "managed-accounts-report.md"
        ).read_text(encoding="utf-8").rstrip("\n")
        tool_fixture = json.loads(
            (ROOT / "contracts" / "skill-evals" / "tool-fixtures.json").read_text(
                encoding="utf-8"
            )
        )["commands"]["accounts report"]["presentation"]

        self.assertEqual(fixture, managed_accounts_report_golden())
        self.assertTrue(tool_fixture["required"])
        self.assertEqual(tool_fixture["rendered_markdown"], fixture)
        self.assertEqual(set(cases), {"performance-account-spend", "performance-qianchuan-spend"})
        for case in cases.values():
            presentation = case["expected"]["presentation"]
            self.assertTrue(presentation["required"])
            self.assertTrue(presentation["verbatim_response"])
            self.assertEqual(
                presentation["fixture"],
                "contracts/presentation/managed-accounts-report.md",
            )
            result = {
                "tool_calls": [
                    {
                        "skill": case["expected"]["skill"]
                        if isinstance(case["expected"]["skill"], str)
                        else case["expected"]["skill"][0],
                        "command": "accounts report",
                        "channel": case["expected"]["channel"],
                    }
                ],
                "assistant_response": fixture,
                "presentation": {
                    "required": True,
                    "source": "rendered_markdown",
                    "rendered_markdown": fixture,
                },
            }
            self.assertEqual(_assert_result(case, result), [])

    def test_expected_any_channel_still_requires_observed_channel(self):
        case = {
            "expected": {
                "command": "accounts list",
                "channel": "any",
            }
        }
        result = {"tool_calls": [{"command": "accounts list"}]}
        self.assertIn(
            "selected channel is missing from tool_calls",
            _assert_result(case, result),
        )

    def test_channel_assertion_is_optional_when_contract_omits_it(self):
        case = {"expected": {"command": "accounts list"}}
        result = {"tool_calls": [{"command": "accounts list"}]}
        self.assertEqual(_assert_result(case, result), [])

    def test_runner_records_contract_only_without_model_or_network(self):
        with tempfile.TemporaryDirectory() as temporary:
            evidence = run_suite(
                case_set="responsible-account-membership",
                case_ids=None,
                driver=None,
                jobs=1,
                trials=1,
                trial_start=1,
                timeout=1,
                model="test-model",
                reasoning="test",
                out=Path(temporary) / "evidence.json",
            )
            self.assertEqual(evidence["summary"]["failed"], 0)
            self.assertEqual(evidence["summary"]["blocked"], 0)
            self.assertEqual(evidence["summary"]["not_run"], 7)
            self.assertTrue((Path(temporary) / "evidence.json").exists())

    def test_model_driver_payload_never_contains_expected_answers(self):
        from unittest import mock

        captured = []

        def fake_driver(command, payload, timeout):
            captured.append(payload)
            return {"tool_calls": [{"skill": "ads-plan-monitor", "command": "accounts list"}]}

        with mock.patch("scripts.acceptance.run_skill_eval._run_driver", side_effect=fake_driver):
            run_suite(
                case_set="responsible-account-membership",
                case_ids=None,
                driver=["fake-driver"],
                jobs=2,
                trials=1,
                trial_start=1,
                timeout=1,
                model="test-model",
                reasoning="test",
                out=None,
            )
        self.assertTrue(captured)
        self.assertTrue(all("expected" not in payload["case"] for payload in captured))

    def test_evidence_shards_can_be_merged_by_case(self):
        from scripts.acceptance.merge_skill_evidence import merge

        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            first = root / "first.json"
            second = root / "second.json"
            common = {"model": "m", "plugin_version": "p", "git_commit": "c", "reasoning": "r"}
            first.write_text(json.dumps({**common, "results": [{"case_id": "a", "trial": 1, "status": "blocked"}]}), encoding="utf-8")
            second.write_text(json.dumps({**common, "results": [{"case_id": "a", "trial": 1, "status": "passed"}]}), encoding="utf-8")
            output = root / "merged.json"
            merged = merge([first, second], output)
            self.assertEqual(merged["summary"]["passed"], 1)


if __name__ == "__main__":
    unittest.main()
