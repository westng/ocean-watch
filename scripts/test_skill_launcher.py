"""Exercise Skill launchers from a foreign cwd with a synthetic runtime only."""

import json
import os
import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


@unittest.skipIf(os.name == "nt", "POSIX Skill launcher; Windows has its own CI entry")
class SkillLauncherTests(unittest.TestCase):
    def test_packaged_cli_preserves_caller_directory_for_relative_input(self):
        source = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix="caller with spaces ") as directory:
            project = Path(directory)
            request = project / "template request.json"
            request.write_text("{}", encoding="utf-8")
            env = dict(os.environ, OCEAN_WATCH_HOME=str(project / "isolated state"))
            for arguments in (["templates", "create"], ["runtime", "exec", "--", "templates", "create"]):
                with self.subTest(arguments=arguments):
                    result = subprocess.run(
                        [str(source / "skills/ads-plan-monitor/run"), *arguments, "--input-file", request.name],
                        cwd=project, env=env, capture_output=True, text=True, timeout=45,
                    )
                    self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                    self.assertEqual(json.loads(result.stdout)["status"], "needs_input")

    def test_loaded_skill_location_routes_all_arguments_from_a_foreign_project(self):
        source = Path(__file__).resolve().parents[1]
        with tempfile.TemporaryDirectory(prefix="skill launcher fixture ") as directory:
            root = Path(directory) / "plugin with spaces"
            project = Path(directory) / "unrelated project"
            project.mkdir()
            launcher = root / "bin/ocean-watch-launcher"
            launcher.parent.mkdir(parents=True)
            launcher.write_text(
                f"#!{sys.executable}\nimport json, sys\nprint(json.dumps(sys.argv[1:]))\n",
                encoding="utf-8",
            )
            launcher.chmod(0o700)
            for name in ("ads-plan-monitor", "qc-plan-monitor", "xt-plan-monitor"):
                with self.subTest(skill=name):
                    skill = root / "skills" / name
                    skill.mkdir(parents=True)
                    shutil.copy2(source / "skills" / name / "run", skill / "run")
                    arguments = ["templates", "create", "--input-json", '{"literal":"$() `x` spaces"}']
                    result = subprocess.run(
                        [str(skill / "run"), *arguments],
                        cwd=project, capture_output=True, text=True, check=True, timeout=5,
                    )
                    self.assertEqual(json.loads(result.stdout), arguments)
                    self.assertFalse((project / "run").exists())


if __name__ == "__main__":
    unittest.main()
