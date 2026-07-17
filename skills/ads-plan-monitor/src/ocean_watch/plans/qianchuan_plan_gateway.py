import datetime as dt

from ocean_watch.core.data import get_path
from ocean_watch.core.errors import ApiError, ConfigurationError
from ocean_watch.core.pagination import declared_page_count

QIANCHUAN_PLAN_LIST_PATH = "/v1.0/qianchuan/uni_promotion/list/"
QIANCHUAN_PLAN_DETAIL_PATH = "/v1.0/qianchuan/uni_promotion/ad/detail/"
QIANCHUAN_PLAN_MATERIALS_PATH = "/v1.0/qianchuan/uni_promotion/ad/material/get/"
QIANCHUAN_ADD_MATERIALS_PATH = "/v1.0/qianchuan/uni_promotion/ad/material/add/"
QIANCHUAN_DELETE_MATERIALS_PATH = "/v1.0/qianchuan/uni_promotion/ad/material/delete/"
PRODUCT_MARKETING_GOAL = "VIDEO_PROM_GOODS"
ALL_ACTIVE_STATUSES = "ALL"
UNI_PROJECT = "UNI_PROJECT"
MAX_PAGE_SIZE = 100
PLAN_HISTORY_WINDOW_DAYS = 180


def decimal_id(value, field):
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise ConfigurationError(f"{field} must be a positive integer")
    return text


page_count = declared_page_count


def require_success(response, operation, **details):
    if response.get("code") == 0:
        return response
    raise ApiError(
        f"Qianchuan {operation} failed",
        {
            "code": response.get("code"),
            "message": response.get("message"),
            "request_id": response.get("request_id"),
            **details,
        },
    )


class QianchuanPlanGateway:
    def __init__(self, client):
        self.client = client

    def list_product_plans(
        self,
        advertiser_id,
        *,
        today=None,
        max_pages=100,
    ):
        advertiser_id = decimal_id(advertiser_id, "advertiser_id")
        today = today or dt.date.today()
        period_start = today - dt.timedelta(days=PLAN_HISTORY_WINDOW_DAYS - 1)
        plans = []
        seen_plan_ids = set()
        request_ids = []
        pages = 0
        truncated = False
        page = 1
        expected_pages = None
        while page <= max_pages:
            response = require_success(
                self.client.get(
                    QIANCHUAN_PLAN_LIST_PATH,
                    params={
                        "advertiser_id": int(advertiser_id),
                        "start_time": f"{period_start.isoformat()} 00:00:00",
                        "end_time": f"{today.isoformat()} 23:59:59",
                        "marketing_goal": PRODUCT_MARKETING_GOAL,
                        "filtering": {"status": ALL_ACTIVE_STATUSES},
                        "fields": ["stat_cost"],
                        "order_type": "DESC",
                        "order_field": "create_time",
                        "page": page,
                        "page_size": MAX_PAGE_SIZE,
                        "adlab_scene": UNI_PROJECT,
                    },
                ),
                "product plan list query",
                advertiser_id=advertiser_id,
                page=page,
                start_time=period_start.isoformat(),
                end_time=today.isoformat(),
            )
            pages += 1
            if response.get("request_id"):
                request_ids.append(response["request_id"])
            page_rows = get_path(response, "data.ad_list", []) or []
            if not isinstance(page_rows, list):
                raise ApiError(
                    "Qianchuan product plan rows must be a list",
                    {"source": "product_plan_list", "page": page},
                )
            for row in page_rows:
                ad_id = get_path(row, "ad_info.id")
                if ad_id is not None:
                    normalized_ad_id = str(ad_id)
                    if normalized_ad_id in seen_plan_ids:
                        continue
                    seen_plan_ids.add(normalized_ad_id)
                plans.append(row)
            total_pages = page_count(
                get_path(response, "data.page_info"),
                source="product_plan_list",
                page=page,
                row_count=len(page_rows),
                expected=expected_pages,
            )
            expected_pages = total_pages
            if total_pages == 0 or page >= total_pages:
                break
            page += 1
        else:
            truncated = True
        return {
            "plans": plans,
            "page_count": pages,
            "data_period": {
                "start_date": period_start.isoformat(),
                "end_date": today.isoformat(),
            },
            "request_ids": request_ids,
            "truncated": truncated,
        }

    def get_plan_detail(self, advertiser_id, ad_id):
        advertiser_id = decimal_id(advertiser_id, "advertiser_id")
        ad_id = decimal_id(ad_id, "ad_id")
        response = require_success(
            self.client.get(
                QIANCHUAN_PLAN_DETAIL_PATH,
                params={"advertiser_id": int(advertiser_id), "ad_id": int(ad_id)},
            ),
            "plan detail query",
            advertiser_id=advertiser_id,
            ad_id=ad_id,
        )
        return response.get("data") or {}

    def find_creator_plans(
        self,
        advertiser_id,
        aweme_ids,
        *,
        aweme_show_ids=None,
        today=None,
        max_pages=100,
    ):
        targets = {decimal_id(value, "aweme_id") for value in aweme_ids}
        show_ids = {
            target: str((aweme_show_ids or {}).get(target) or "").strip()
            for target in targets
        }
        aliases = {
            target: {value for value in (target, show_ids[target]) if value}
            for target in targets
        }
        listed = self.list_product_plans(
            advertiser_id,
            today=today,
            max_pages=max_pages,
        )
        if listed["truncated"]:
            raise ApiError(
                "Qianchuan product plan list query was truncated",
                {"advertiser_id": str(advertiser_id)},
            )
        candidate_ids = {target: [] for target in targets}
        list_rows = {}
        for row in listed["plans"]:
            ad_info = row.get("ad_info") or {}
            ad_id = ad_info.get("id")
            if ad_id is None:
                continue
            ad_id = str(ad_id)
            list_rows[ad_id] = row
            anchor_ids = {
                str(room.get("anchor_id"))
                for room in (row.get("room_info") or [])
                if room.get("anchor_id") is not None
            }
            for target, target_aliases in aliases.items():
                if target_aliases.intersection(anchor_ids):
                    candidate_ids[target].append(ad_id)

        matches = {target: [] for target in targets}
        for target, ad_ids in candidate_ids.items():
            for ad_id in dict.fromkeys(ad_ids):
                detail = self.get_plan_detail(advertiser_id, ad_id)
                if str(detail.get("aweme_id") or "") != target:
                    continue
                matches[target].append({
                    "ad_id": ad_id,
                    "name": detail.get("name"),
                    "status": detail.get("status"),
                    "opt_status": detail.get("opt_status"),
                    "product_ids": [
                        str(item["product_id"])
                        for item in (detail.get("product_infos") or [])
                        if item.get("product_id") is not None
                    ],
                    "detail": detail,
                    "list_row": list_rows[ad_id],
                })
        return {"matches": matches, "list_query": listed}

    def list_plan_video_materials(self, advertiser_id, ad_id, *, max_pages=100):
        advertiser_id = decimal_id(advertiser_id, "advertiser_id")
        ad_id = decimal_id(ad_id, "ad_id")
        rows = []
        request_ids = []
        page = 1
        pages = 0
        truncated = False
        expected_pages = None
        while page <= max_pages:
            response = require_success(
                self.client.get(
                    QIANCHUAN_PLAN_MATERIALS_PATH,
                    params={
                        "advertiser_id": int(advertiser_id),
                        "ad_id": int(ad_id),
                        "filtering": {
                            "material_type": "VIDEO",
                            "material_status": "ALL",
                        },
                        "fields": ["stat_cost_for_roi2"],
                        "page": page,
                        "page_size": MAX_PAGE_SIZE,
                    },
                ),
                "plan material query",
                advertiser_id=advertiser_id,
                ad_id=ad_id,
                page=page,
            )
            pages += 1
            if response.get("request_id"):
                request_ids.append(response["request_id"])
            page_rows = get_path(response, "data.ad_material_infos", []) or []
            if not isinstance(page_rows, list):
                raise ApiError(
                    "Qianchuan plan material rows must be a list",
                    {"source": "plan_material_list", "page": page},
                )
            rows.extend(page_rows)
            total_pages = page_count(
                get_path(response, "data.page_info"),
                source="plan_material_list",
                page=page,
                row_count=len(page_rows),
                expected=expected_pages,
            )
            expected_pages = total_pages
            if total_pages == 0 or page >= total_pages:
                break
            page += 1
        else:
            truncated = True
        return {
            "materials": rows,
            "page_count": pages,
            "request_ids": request_ids,
            "truncated": truncated,
        }

    def add_materials(self, advertiser_id, ad_id, multi_product_creative_list):
        advertiser_id = decimal_id(advertiser_id, "advertiser_id")
        ad_id = decimal_id(ad_id, "ad_id")
        payload = {
            "advertiser_id": int(advertiser_id),
            "ad_id": int(ad_id),
            "multi_product_creative_list": multi_product_creative_list,
        }
        response = self.client.post(QIANCHUAN_ADD_MATERIALS_PATH, payload)
        return payload, response

    def delete_materials(self, advertiser_id, ad_id, material_ids):
        advertiser_id = decimal_id(advertiser_id, "advertiser_id")
        ad_id = decimal_id(ad_id, "ad_id")
        normalized_ids = list(dict.fromkeys(
            decimal_id(value, f"material_ids[{index}]")
            for index, value in enumerate(material_ids or [])
        ))
        if not normalized_ids or len(normalized_ids) > 100:
            raise ConfigurationError("material_ids must contain between 1 and 100 unique IDs")
        payload = {
            "advertiser_id": int(advertiser_id),
            "ad_id": int(ad_id),
            "material_ids": [int(value) for value in normalized_ids],
        }
        response = self.client.post(QIANCHUAN_DELETE_MATERIALS_PATH, payload)
        return payload, response


def existing_aweme_item_ids(material_rows):
    item_ids = set()
    for row in material_rows:
        if row.get("material_status") == "DELETED":
            continue
        video = get_path(row, "material_info.video_material", {}) or {}
        value = video.get("aweme_item_id")
        if value is not None and str(value) != "0":
            item_ids.add(str(value))
    return item_ids
