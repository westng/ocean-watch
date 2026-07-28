import unittest
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]


def load_workflow(path: Path) -> dict:
    value = yaml.load(path.read_text(encoding="utf-8"), Loader=yaml.BaseLoader)
    if not isinstance(value, dict):
        raise AssertionError(f"workflow is not an object: {path.name}")
    return value


class WorkflowInventoryTests(unittest.TestCase):
    def test_workflow_inventory_remains_minimal(self):
        paths = sorted((ROOT / ".github" / "workflows").glob("*.yml"))
        self.assertEqual([path.name for path in paths], ["ci.yml", "tag.yml"])

    def test_manual_workflows_respect_dispatch_input_limit(self):
        for path in sorted((ROOT / ".github" / "workflows").glob("*.yml")):
            workflow = load_workflow(path)
            dispatch = (workflow.get("on") or {}).get("workflow_dispatch")
            inputs = dispatch.get("inputs", {}) if isinstance(dispatch, dict) else {}
            self.assertLessEqual(len(inputs), 10, path.name)

    def test_release_has_no_unconfigured_g5_dependencies(self):
        workflow = (ROOT / ".github" / "workflows" / "tag.yml").read_text(
            encoding="utf-8"
        )
        self.assertNotIn("environment:", workflow)
        self.assertNotIn("secrets.", workflow)
        self.assertNotIn("g5-", workflow.lower())
        self.assertNotIn("sealed", workflow.lower())


if __name__ == "__main__":
    unittest.main()
