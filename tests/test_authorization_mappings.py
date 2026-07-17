import unittest
from unittest import mock

from ocean_watch.auth import query_authorization_mappings


class AuthorizationMappingTests(unittest.TestCase):
    def test_mapping_output_contains_token_presence_not_values(self):
        state = {
            "advertiser_index": {"123": ["auth-one"]},
            "authorizations": {
                "auth-one": {
                    "token_revision": 2,
                    "advertiser_ids": ["123"],
                    "authorized_accounts": [{
                        "account_id": "456",
                        "account_name": "Account",
                        "advertiser_ids": ["123"],
                    }],
                },
            },
        }
        with mock.patch.object(
            query_authorization_mappings.authorization_store,
            "load_channel_state",
            return_value=state,
        ), mock.patch.object(
            query_authorization_mappings.credential_store,
            "read_entry",
            return_value={"access_token": "secret-token", "refresh_token": "refresh"},
        ):
            result = query_authorization_mappings.channel_mappings("marketing", "123")
        rendered = str(result)
        self.assertNotIn("secret-token", rendered)
        self.assertNotIn("refresh'", rendered)
        self.assertTrue(result["authorizations"][0]["has_access_token"])
        self.assertEqual(result["mappings"][0]["authorization_ids"], ["auth-one"])


if __name__ == "__main__":
    unittest.main()
