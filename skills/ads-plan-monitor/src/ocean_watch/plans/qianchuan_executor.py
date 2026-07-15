from dataclasses import dataclass

from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path

QIANCHUAN_CREATE_PATH = "/v1.0/qianchuan/uni_aweme/ad/create/"


@dataclass(frozen=True)
class QianchuanPlanExecutionRequest:
    payload: dict
    submit: bool = False
    blocking_fields: tuple = ()


class QianchuanPlanExecutor:
    def __init__(self, client):
        self.client = client

    @classmethod
    def from_credentials(cls, base_url, access_token, client_factory=OceanEngineClient):
        return cls(client_factory(base_url, access_token))

    def execute(self, request):
        result = {
            "endpoint": QIANCHUAN_CREATE_PATH,
            "payload": request.payload,
        }
        if not request.submit:
            return result
        if request.blocking_fields:
            return {
                **result,
                "submit_blocked": True,
                "blocking_fields": list(request.blocking_fields),
            }

        response = self.client.post(QIANCHUAN_CREATE_PATH, request.payload)
        result["response"] = response
        ad_id = get_path(response, "data.ad_id")
        if response.get("code") != 0 or ad_id is None:
            result.update({
                "submit_failed": True,
                "failure_stage": "qianchuan_plan_create",
            })
            return result
        result["ad_id"] = str(ad_id)
        return result
