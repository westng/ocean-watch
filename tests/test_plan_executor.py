import unittest

from ocean_watch.plans.executor import PlanExecutionRequest, PlanExecutor


class FakeClient:
    def __init__(self, responses):
        self.responses = iter(responses)
        self.calls = []

    def post(self, path, payload):
        self.calls.append((path, payload))
        return next(self.responses)


class PlanExecutorTests(unittest.TestCase):
    def request(self, **overrides):
        values = {
            "project_payload": {"name": "project"},
            "promotion_payload": {"name": "promotion", "project_id": "{{project_id}}"},
            "submit": True,
        }
        values.update(overrides)
        return PlanExecutionRequest(**values)

    def test_dry_run_does_not_call_api(self):
        client = FakeClient([])
        result = PlanExecutor(client).execute(self.request(submit=False))
        self.assertEqual(client.calls, [])
        self.assertEqual(result["project_payload"]["name"], "project")

    def test_successful_transaction_uses_created_project(self):
        client = FakeClient([
            {"code": 0, "data": {"project_id": "p1"}},
            {"code": 0, "data": {"promotion_id": "u1"}},
        ])
        result = PlanExecutor(client).execute(self.request())
        self.assertEqual(result["project_id"], "p1")
        self.assertEqual(result["promotion_id"], "u1")
        self.assertEqual(client.calls[1][1]["project_id"], "p1")

    def test_promotion_failure_returns_resumable_project(self):
        client = FakeClient([
            {"code": 0, "data": {"project_id": "p1"}},
            {"code": 400, "message": "invalid promotion"},
        ])
        result = PlanExecutor(client).execute(self.request())
        self.assertTrue(result["submit_failed"])
        self.assertEqual(result["failure_stage"], "promotion_create")
        self.assertEqual(result["project_id"], "p1")

    def test_promotion_only_skips_project_api(self):
        client = FakeClient([{"code": 0, "data": {"promotion_id": "u1"}}])
        result = PlanExecutor(client).execute(self.request(
            promotion_only=True,
            project_id="p1",
        ))
        self.assertEqual(len(client.calls), 1)
        self.assertEqual(client.calls[0][1]["project_id"], "p1")
        self.assertEqual(result["promotion_id"], "u1")

    def test_blocking_fields_prevent_api_calls(self):
        client = FakeClient([])
        result = PlanExecutor(client).execute(self.request(blocking_fields=("titles",)))
        self.assertTrue(result["submit_blocked"])
        self.assertEqual(result["blocking_fields"], ["titles"])
        self.assertEqual(client.calls, [])


if __name__ == "__main__":
    unittest.main()
