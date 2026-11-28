"""Exercise Doubao imports and upgrades in a disposable workspace with a fake Runtime."""
import json
import os
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

SOURCE = Path(__file__).resolve().parents[1]
SKILLS = ("qc-plan-monitor", "ads-plan-monitor", "xt-plan-monitor")


@unittest.skipUnless(os.name == "posix" and shutil.which("ruby"), "macOS installer needs Bash and Ruby")
class DoubaoInstallTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory(prefix="doubao install fixture ")
        self.addCleanup(temp.cleanup)
        fixture = Path(temp.name).resolve()
        self.repo = fixture / "repo with ' $pecial 字"
        self.workspace = fixture / "workspace"
        self.skills = self.workspace / ".user_skills"
        self.script = self.repo / "scripts/install-doubao.sh"
        self.script.parent.mkdir(parents=True)
        shutil.copy2(SOURCE / "scripts/install-doubao.sh", self.script)
        self.binary = self.repo / ".codex-plugin/bin/ocean-watch_darwin_arm64"
        self.launcher = self.repo / "bin/ocean-watch-launcher"
        for entry in (self.binary, self.launcher):
            entry.parent.mkdir(parents=True)
            entry.write_text('#!/bin/sh\nprintf "%s\\0" "$0" "$@"\n', encoding="utf-8")
            entry.chmod(0o755)
        for name in SKILLS:
            target = self.skills / name
            target.mkdir(parents=True)
            shutil.copy2(SOURCE / "skills" / name / "run", target / "run")
            (target / "agents").mkdir()
            shutil.copy2(SOURCE / "skills" / name / "agents/openai.yaml", self.config(name))
        self.wrapper = self.workspace / "bin/ocean-watch-launcher"

    def config(self, name):
        return self.skills / name / "agents/openai.yaml"

    def install(self, check=True):
        return subprocess.run(
            ["bash", self.script], cwd=self.workspace, check=check, timeout=30,
            env={**os.environ, "DOUBAO_USER_SKILLS": str(self.skills)}, capture_output=True, text=True,
        )

    def test_imports_preserve_other_dependencies_and_run_from_foreign_cwd(self):
        path = self.config(SKILLS[0])
        other_service = ('    - type: mcp  # a separate connector\n'
                         '      value: my-service\n      transport: "stdio"\n'
                         '      command: "/my/bin"\n      args: ["keep", "me"]\n')
        original = (path.read_text(encoding="utf-8") + '      env: {CUSTOM: keep}  # local setting\n'
                    + '      command: "/old"\n      args:\n        - old\n' + other_service)
        path.write_text(original, encoding="utf-8")
        self.install()
        updated = path.read_text(encoding="utf-8")
        self.assertIn(other_service, updated)
        self.assertIn('env: {CUSTOM: keep}  # local setting', updated)
        self.assertEqual(path.with_suffix(".yaml.bak").read_text(encoding="utf-8"), original)
        for name in SKILLS:
            with self.subTest(skill=name):
                yaml_path = self.config(name)
                parsed = subprocess.check_output([
                    'ruby', '-ryaml', '-rjson', '-e',
                    'puts JSON.generate(YAML.safe_load(File.read(ARGV.first)))', str(yaml_path),
                ], text=True, timeout=30)
                tool = next(tool for tool in json.loads(parsed)['dependencies']['tools'] if tool['value'] == 'ocean-watch')
                self.assertEqual(tool['command'], str(self.binary))
                self.assertEqual(tool['args'], ['mcp', 'proxy', '--stdio', '--plugin-root', str(self.repo)])
                args = ["templates", "create", "--input-json", '{"name":"$literal `space`"}']
                result = subprocess.check_output([self.skills / name / "run", *args], cwd=self.workspace, timeout=5)
                self.assertEqual(result.decode().split('\0'), [str(self.launcher), *args, ''])

        self.install()
        self.assertEqual(path.read_text(encoding="utf-8"), updated)
        self.assertEqual(path.with_suffix(".yaml.bak").read_text(encoding="utf-8"), original)

    def test_unowned_workspace_launcher_is_preserved(self):
        self.wrapper.parent.mkdir()
        self.wrapper.write_text("user launcher", encoding="utf-8")
        originals = {name: self.config(name).read_bytes() for name in SKILLS}
        self.assertNotEqual(self.install(check=False).returncode, 0)
        self.assertEqual(self.wrapper.read_text(encoding="utf-8"), "user launcher")
        self.assertEqual({name: self.config(name).read_bytes() for name in SKILLS}, originals)

    def test_ambiguous_target_fails_before_any_edits(self):
        path = self.config(SKILLS[-1])
        with path.open('a', encoding='utf-8') as stream:
            stream.write('    - {type: mcp, value: ocean-watch, transport: stdio}\n')
        originals = {name: self.config(name).read_bytes() for name in SKILLS}
        self.assertNotEqual(self.install(check=False).returncode, 0)
        self.assertFalse(self.wrapper.exists())
        self.assertEqual({name: self.config(name).read_bytes() for name in SKILLS}, originals)


if __name__ == "__main__":
    unittest.main()
