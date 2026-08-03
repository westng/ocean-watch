from pathlib import Path

from ocean_watch.api.client import OceanEngineClient, RequestBudget, RequestThrottle

QIANCHUAN_REQUEST_INTERVAL_SECONDS = 0.25
QIANCHUAN_MAX_IN_FLIGHT = 1


def qianchuan_advertiser_lock_path(state_root, advertiser_id):
    advertiser_id = str(advertiser_id).strip()
    if (
        not advertiser_id.isascii()
        or not advertiser_id.isdigit()
        or advertiser_id.startswith("0")
        or len(advertiser_id) > 64
    ):
        raise ValueError("advertiser_id must be a positive decimal ID")
    return Path(state_root) / "locks" / f"qianchuan-advertiser-{advertiser_id}.lock"


def qianchuan_request_throttle(state_root, advertiser_id):
    advertiser_id = str(advertiser_id).strip()
    qianchuan_advertiser_lock_path(state_root, advertiser_id)
    return RequestThrottle(
        minimum_interval=QIANCHUAN_REQUEST_INTERVAL_SECONDS,
        max_in_flight=QIANCHUAN_MAX_IN_FLIGHT,
        state_path=(
            Path(state_root)
            / "request-control"
            / f"qianchuan-{advertiser_id}.json"
        ),
    )


class QianchuanClientFactory:
    def __init__(
        self,
        state_root,
        advertiser_id,
        *,
        request_limit=None,
        client_class=OceanEngineClient,
    ):
        self.advertiser_id = str(advertiser_id).strip()
        qianchuan_advertiser_lock_path(state_root, self.advertiser_id)
        self.throttle = qianchuan_request_throttle(state_root, self.advertiser_id)
        self.budget = RequestBudget(request_limit) if request_limit is not None else None
        self.client_class = client_class

    def client(self, base_url, access_token):
        return self.client_class(
            base_url,
            access_token,
            request_throttle=self.throttle,
            request_budget=self.budget,
        )

    def budget_snapshot(self):
        return self.budget.snapshot() if self.budget is not None else None
