import copy
import json
import tempfile
import unittest
from contextlib import redirect_stdout
from io import StringIO
from pathlib import Path
from unittest import mock

from ocean_watch.auth import channels
from ocean_watch.materials import qianchuan_creator_accounts as creator_accounts
from ocean_watch.materials import query_qianchuan_creator_videos as creator_videos
from ocean_watch.templates import qianchuan_product_templates as product_templates

from tests.support import valid_config


class FakeClient:
    def __init__(self, responses):
        self.responses = list(responses)
        self.calls = []

    def get(self, path, params=None):
        self.calls.append((path, copy.deepcopy(params)))
        return self.responses.pop(0)


def template(product_ids="1001/1002"):
    return product_templates.build_business_template(
        advertiser_id="1234567890123456",
        product_name="示例商品",
        product_short_name="示例",
        product_ids=product_ids,
        template_id="qcpt_test",
        template_name="测试千川模板",
    )


def video(item_id, title="视频"):
    return {
        "aweme_item_id": item_id,
        "image_mode": "VIDEO_VERTICAL",
        "video_id": f"video-{item_id}",
        "material_id": item_id + 100,
        "title": title,
        "video_cover_url": "https://example.test/cover.jpg",
        "url": "https://example.test/video.mp4",
        "width": 1080,
        "height": 1920,
        "duration": 15,
        "is_recommend": 1,
        "view_cnt": 100,
        "like_cnt": 20,
        "share_cnt": 3,
        "comment_cnt": 4,
        "is_ai_create": False,
    }


def authorized_response(
    aweme_id=9001,
    aweme_show_id="creator001",
    aweme_name="测试达人甲",
    request_id="creator-request",
    page=1,
    total_page=1,
):
    return {
        "code": 0,
        "request_id": request_id,
        "data": {
            "aweme_id_list": [{
                "aweme_id": aweme_id,
                "aweme_show_id": aweme_show_id,
                "aweme_name": aweme_name,
                "auth_type": ["COOPERATE"],
            }],
            "page_info": {
                "page": page,
                "page_size": 100,
                "total_page": total_page,
                "total_number": total_page,
            },
        },
    }


class QianchuanCreatorVideoTests(unittest.TestCase):
    def test_queries_each_product_paginates_and_deduplicates(self):
        authorized_client = FakeClient([authorized_response()])
        video_client = FakeClient([
            {
                "code": 0,
                "request_id": "request-1",
                "data": {
                    "video_list": [video(101)],
                    "page_info": {"has_more": 1, "cursor": 20},
                },
            },
            {
                "code": 0,
                "request_id": "request-2",
                "data": {
                    "video_list": [video(102)],
                    "page_info": {"has_more": 0, "cursor": 20},
                },
            },
            {
                "code": 0,
                "request_id": "request-3",
                "data": {
                    "video_list": [video(101)],
                    "page_info": {"has_more": 0, "cursor": 0},
                },
            },
        ])
        result = creator_videos.fetch_template_creator_videos(
            authorized_client,
            video_client,
            template(),
            "creator001",
            creator_name="测试达人甲",
        )
        self.assertEqual(result["material_count"], 2)
        by_id = {row["aweme_item_id"]: row for row in result["materials"]}
        self.assertEqual(by_id["101"]["matched_product_ids"], ["1001", "1002"])
        self.assertEqual(by_id["102"]["matched_product_ids"], ["1001"])
        self.assertEqual([row["page_count"] for row in result["queries"]], [2, 1])
        self.assertEqual(result["aweme_id"], "9001")
        self.assertEqual(
            authorized_client.calls[0][0],
            creator_accounts.QIANCHUAN_UNI_AWEME_AUTHORIZED_PATH,
        )
        self.assertEqual(
            authorized_client.calls[0][1]["filtering"],
            {
                "marketing_goal": "VIDEO_PROM_GOODS",
                "search_key_words": "creator001",
                "scene": "CREATE",
            },
        )
        self.assertEqual(video_client.calls[0][0], creator_videos.QIANCHUAN_AWEME_VIDEO_PATH)
        self.assertEqual(video_client.calls[0][1]["aweme_id"], 9001)
        self.assertEqual(video_client.calls[0][1]["filtering"], {"product_id": 1001})
        self.assertEqual(video_client.calls[1][1]["cursor"], 20)
        self.assertEqual(video_client.calls[2][1]["filtering"], {"product_id": 1002})

    def test_query_error_preserves_official_diagnostics(self):
        authorized_client = FakeClient([authorized_response()])
        video_client = FakeClient([{
            "code": 40000,
            "message": "invalid aweme_id",
            "request_id": "request-error",
        }])
        with self.assertRaises(Exception) as raised:
            creator_videos.fetch_template_creator_videos(
                authorized_client,
                video_client,
                template("1001"),
                "creator001",
            )
        self.assertEqual(raised.exception.details["code"], 40000)
        self.assertEqual(raised.exception.details["request_id"], "request-error")

    def test_invalid_pagination_cursor_is_rejected(self):
        authorized_client = FakeClient([authorized_response()])
        video_client = FakeClient([{
            "code": 0,
            "data": {
                "video_list": [],
                "page_info": {"has_more": 1, "cursor": None},
            },
        }])
        with self.assertRaisesRegex(Exception, "invalid cursor"):
            creator_videos.fetch_template_creator_videos(
                authorized_client,
                video_client,
                template("1001"),
                "creator001",
            )

    def test_resolver_paginates_and_requires_exact_show_id(self):
        first = authorized_response(
            aweme_id=8001,
            aweme_show_id="creator001-other",
            aweme_name="测试达人甲",
            request_id="creator-request-1",
            total_page=2,
        )
        second = authorized_response(
            aweme_id=9001,
            aweme_show_id="creator001",
            request_id="creator-request-2",
            page=2,
            total_page=2,
        )
        client = FakeClient([first, second])
        result = creator_accounts.resolve_authorized_aweme(
            client,
            "1234567890123456",
            "creator001",
            creator_name="测试达人甲",
        )
        self.assertEqual(result["aweme_id"], "9001")
        self.assertEqual(result["match_field"], "aweme_show_id")
        self.assertTrue(result["creator_name_matches"])
        self.assertEqual(result["page_count"], 2)
        self.assertEqual(client.calls[1][1]["page"], 2)

    def test_resolver_rejects_fuzzy_only_match(self):
        client = FakeClient([
            authorized_response(aweme_show_id="creator001-other", aweme_name="测试达人甲")
        ])
        with self.assertRaisesRegex(Exception, "No exact authorized") as raised:
            creator_accounts.resolve_authorized_aweme(
                client,
                "1234567890123456",
                "creator001",
                creator_name="测试达人甲",
            )
        self.assertEqual(raised.exception.details["candidate_count"], 1)
        self.assertEqual(
            raised.exception.details["candidates"][0]["aweme_show_id"],
            "creator001-other",
        )

    def test_resolver_searches_visible_id_but_matches_expected_numeric_uid(self):
        client = FakeClient([
            authorized_response(
                aweme_id=9001,
                aweme_show_id="renamed-visible-id",
                aweme_name="测试达人甲",
            )
        ])
        result = creator_accounts.resolve_authorized_aweme(
            client,
            "1234567890123456",
            "old-visible-id",
            expected_aweme_id="9001",
        )
        self.assertEqual(result["aweme_id"], "9001")
        self.assertEqual(result["aweme_show_id"], "renamed-visible-id")
        self.assertEqual(result["match_field"], "aweme_id")
        self.assertEqual(
            client.calls[0][1]["filtering"]["search_key_words"],
            "old-visible-id",
        )

    def test_resolver_preserves_official_error(self):
        client = FakeClient([{
            "code": 40000,
            "message": "invalid filtering",
            "request_id": "creator-error",
        }])
        with self.assertRaises(Exception) as raised:
            creator_accounts.resolve_authorized_aweme(
                client,
                "1234567890123456",
                "creator001",
            )
        self.assertEqual(raised.exception.details["code"], 40000)
        self.assertEqual(raised.exception.details["request_id"], "creator-error")

    def test_resolver_rejects_ambiguous_exact_matches(self):
        response = authorized_response()
        response["data"]["aweme_id_list"].append({
            "aweme_id": 9002,
            "aweme_show_id": "creator001",
            "aweme_name": "另一个达人",
        })
        with self.assertRaisesRegex(Exception, "multiple authorized"):
            creator_accounts.resolve_authorized_aweme(
                FakeClient([response]),
                "1234567890123456",
                "creator001",
            )

    def test_cli_resolves_only_qianchuan_authorization(self):
        config = channels.migrate_config(valid_config())
        config = product_templates.ensure_config(config)
        config[product_templates.TEMPLATES_KEY] = {"qcpt_test": template("1001")}
        runtime = channels.runtime_config(
            config,
            "qianchuan",
            capability="qianchuan_materials",
        )
        runtime["api"] = {
            "base_url": "https://api.oceanengine.com/open_api",
            "access_token": "test-token",
        }
        query_result = {
            "endpoint": creator_videos.QIANCHUAN_AWEME_VIDEO_PATH,
            "advertiser_id": "1234567890123456",
            "template_id": "qcpt_test",
            "material_count": 0,
            "materials": [],
        }
        with tempfile.TemporaryDirectory() as directory:
            config_path = Path(directory) / "config.json"
            config_path.write_text(json.dumps(config), encoding="utf-8")
            authorized_client = mock.Mock(name="authorized_aweme_client")
            video_client = mock.Mock(name="video_client")
            with mock.patch.object(
                creator_videos.token_manager,
                "ensure_access_token",
                return_value=runtime,
            ) as ensure_token, mock.patch.object(
                creator_videos,
                "OceanEngineClient",
                side_effect=[authorized_client, video_client],
            ) as client_class, mock.patch.object(
                creator_videos,
                "fetch_template_creator_videos",
                return_value=query_result,
            ) as fetch_videos, redirect_stdout(StringIO()):
                exit_code = creator_videos.main([
                    "--config",
                    str(config_path),
                    "--plan-template",
                    "qcpt_test",
                    "--douyin-id",
                    "creator001",
                    "--creator-name",
                    "测试达人甲",
                ])
        self.assertEqual(exit_code, 0)
        self.assertEqual(client_class.call_args_list[0].args[0], "https://api.oceanengine.com/open_api")
        self.assertEqual(client_class.call_args_list[1].args[0], "https://ad.oceanengine.com/open_api")
        self.assertIs(fetch_videos.call_args.args[0], authorized_client)
        self.assertIs(fetch_videos.call_args.args[1], video_client)
        self.assertEqual(ensure_token.call_args.kwargs["channel"], "qianchuan")
        self.assertEqual(
            ensure_token.call_args.kwargs["advertiser_id"],
            "1234567890123456",
        )


if __name__ == "__main__":
    unittest.main()
