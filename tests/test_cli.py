import json
import unittest
from contextlib import redirect_stdout
from io import StringIO
from unittest import mock

from ocean_watch.cli import main as cli


class CliTests(unittest.TestCase):
    def test_forwards_domain_arguments(self):
        handler = mock.Mock(return_value=0)
        with mock.patch.dict(cli.COMMANDS, {
            ("setup", "validate"): (handler, (), "Validate configuration readiness"),
        }):
            code = cli.main(["setup", "validate", "--config", "example.json", "--mode", "query"])
        self.assertEqual(code, 0)
        handler.assert_called_once_with(["--config", "example.json", "--mode", "query"])

    def test_prefixes_action_arguments(self):
        handler = mock.Mock(return_value=0)
        with mock.patch.dict(cli.COMMANDS, {
            ("templates", "list"): (handler, ("list",), "List plan templates"),
        }):
            code = cli.main(["templates", "list", "--config", "example.json"])
        self.assertEqual(code, 0)
        handler.assert_called_once_with(["list", "--config", "example.json"])

    def test_structures_unexpected_errors(self):
        handler = mock.Mock(side_effect=RuntimeError("failed"))
        with mock.patch.dict(cli.COMMANDS, {
            ("setup", "validate"): (handler, (), "Validate configuration readiness"),
        }), redirect_stdout(StringIO()) as output:
            code = cli.main(["setup", "validate"])
        self.assertEqual(code, 1)
        payload = json.loads(output.getvalue())
        self.assertEqual(payload["error"]["code"], "unexpected_error")


if __name__ == "__main__":
    unittest.main()
