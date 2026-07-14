import unittest

from ocean_watch.auth import oauth_local_authorize


class OAuthStateTests(unittest.TestCase):
    def test_marketing_state_uses_ad_prefix(self):
        state = oauth_local_authorize.build_oauth_state("marketing", nonce="nonce-1")
        self.assertEqual(state, "AD.nonce-1")
        self.assertEqual(oauth_local_authorize.channel_from_oauth_state(state), "marketing")

    def test_qianchuan_state_uses_qc_prefix(self):
        state = oauth_local_authorize.build_oauth_state("qianchuan", nonce="nonce-2")
        self.assertEqual(state, "QC.nonce-2")
        self.assertEqual(oauth_local_authorize.channel_from_oauth_state(state), "qianchuan")

    def test_static_channel_code_is_not_a_valid_state(self):
        with self.assertRaisesRegex(ValueError, "random value"):
            oauth_local_authorize.channel_from_oauth_state("AD")

    def test_unknown_channel_code_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "unknown OAuth state channel code"):
            oauth_local_authorize.channel_from_oauth_state("OTHER.nonce")

    def test_callback_state_must_match_current_channel_session(self):
        with self.assertRaises(oauth_local_authorize.OAuthStateError) as raised:
            oauth_local_authorize.validate_callback_state(
                "QC.nonce-2",
                expected_state="QC.nonce-2",
                expected_channel="marketing",
            )
        self.assertEqual(raised.exception.code, "state_channel_mismatch")

    def test_callback_state_must_match_expected_random_value(self):
        with self.assertRaises(oauth_local_authorize.OAuthStateError) as raised:
            oauth_local_authorize.validate_callback_state(
                "AD.other",
                expected_state="AD.nonce-1",
                expected_channel="marketing",
            )
        self.assertEqual(raised.exception.code, "state_mismatch")


if __name__ == "__main__":
    unittest.main()
