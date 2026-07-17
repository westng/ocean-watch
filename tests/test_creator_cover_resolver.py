import copy
import unittest

from ocean_watch.materials import creator_cover_resolver, creator_materials


def candidate(item_id="8101", material_id="8201", cover_id=None):
    reasons = [] if cover_id else ["missing_video_cover_id"]
    return {
        "owner_advertiser_id": "1234567890123456",
        "creator_id": "creator-one",
        "item_id": item_id,
        "material_id": material_id,
        "video_id": "video-one",
        "video_cover_id": cover_id,
        "usable": not reasons,
        "unusable_reasons": reasons,
    }


def promotion(cover_id, *, item_id="8101", material_id="8201", status="MATERIAL_STATUS_OK"):
    return {
        "advertiser_id": 1234567890123456,
        "project_id": 8301,
        "promotion_id": 8401,
        "promotion_materials": {
            "video_material_list": [{
                "item_id": int(item_id),
                "material_id": int(material_id),
                "video_cover_id": cover_id,
                "material_status": status,
            }],
        },
    }


class FakeClient:
    def __init__(self, pages):
        self.pages = pages
        self.calls = []

    def get(self, path, params=None):
        self.calls.append((path, copy.deepcopy(params)))
        page = int(params["page"])
        return {
            "code": 0,
            "request_id": f"request-{page}",
            "data": {
                "list": copy.deepcopy(self.pages[page - 1]),
                "page_info": {"page": page, "total_page": len(self.pages)},
            },
        }


class CreatorCoverResolverTests(unittest.TestCase):
    def test_existing_cover_does_not_query_history(self):
        rows = [candidate(cover_id="cover-current")]
        resolved, diagnostics = creator_cover_resolver.resolve_missing_covers(
            rows,
            {},
            client=FakeClient([]),
        )
        self.assertEqual(resolved, rows)
        self.assertEqual(diagnostics, {"status": "not_required"})

    def test_unique_same_material_cover_is_resolved_without_mutating_input(self):
        rows = [candidate()]
        original = copy.deepcopy(rows)
        client = FakeClient([[promotion("cover-history")]])

        resolved, diagnostics = creator_cover_resolver.resolve_missing_covers(
            rows,
            {},
            client=client,
        )

        self.assertEqual(rows, original)
        self.assertEqual(resolved[0]["video_cover_id"], "cover-history")
        self.assertTrue(resolved[0]["usable"])
        self.assertEqual(resolved[0]["unusable_reasons"], [])
        self.assertEqual(diagnostics["status"], "resolved")
        self.assertEqual(diagnostics["source"], "matching_official_promotion")

    def test_different_material_or_invalid_status_is_not_reused(self):
        client = FakeClient([[
            promotion("wrong-material", material_id="9999"),
            promotion("invalid-status", status="MATERIAL_STATUS_ERROR"),
        ]])
        resolved, diagnostics = creator_cover_resolver.resolve_missing_covers(
            [candidate()],
            {},
            client=client,
        )
        self.assertIsNone(resolved[0]["video_cover_id"])
        self.assertFalse(resolved[0]["usable"])
        self.assertEqual(diagnostics["unresolved_item_ids"], ["8101"])

    def test_distinct_historical_covers_require_selection(self):
        client = FakeClient([[
            promotion("cover-one"),
            promotion("cover-two"),
        ]])
        with self.assertRaises(creator_materials.CreatorMaterialError) as raised:
            creator_cover_resolver.resolve_missing_covers(
                [candidate()],
                {},
                client=client,
            )
        self.assertEqual(raised.exception.code, "creator_cover_selection_required")

    def test_paginates_before_resolving_cover(self):
        client = FakeClient([[], [promotion("cover-page-two")]])
        resolved, diagnostics = creator_cover_resolver.resolve_missing_covers(
            [candidate()],
            {},
            client=client,
        )
        self.assertEqual(resolved[0]["video_cover_id"], "cover-page-two")
        self.assertEqual(diagnostics["request_ids"], ["request-1", "request-2"])
