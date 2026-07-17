import unittest

from ocean_watch.cli import main as cli


class OperationalCliCommandTests(unittest.TestCase):
    def test_exposes_stable_read_and_write_commands(self):
        expected = {
            ("auth", "mappings"),
            ("templates", "validate"),
            ("templates", "delete"),
            ("qc-materials", "inspect-work"),
            ("qc-materials", "authorized-creators"),
            ("qc-products", "list"),
            ("qc-products", "search"),
            ("qc-plans", "list"),
            ("qc-plans", "show"),
            ("qc-plans", "materials"),
            ("qc-plans", "update-status"),
            ("qc-plans", "update-budget"),
            ("qc-plans", "update-roi"),
            ("runs", "list"),
            ("runs", "show"),
            ("reports", "plans"),
            ("qc-reports", "materials"),
            ("plans", "update-project-status"),
            ("plans", "update-promotion-status"),
            ("plans", "update-budget"),
            ("plans", "update-bid"),
            ("plans", "update-roi"),
            ("qc-templates", "list-live"),
            ("qc-templates", "create-live"),
            ("qc-templates", "migrate-live"),
        }
        self.assertTrue(expected.issubset(cli.COMMANDS))


if __name__ == "__main__":
    unittest.main()
