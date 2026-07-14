from dataclasses import dataclass

from ocean_watch.api import OceanEngineClient
from ocean_watch.core.data import get_path

PROJECT_CREATE_PATH = "/v3.0/project/create/"
PROMOTION_CREATE_PATH = "/v3.0/promotion/create/"


@dataclass(frozen=True)
class PlanExecutionRequest:
    project_payload: dict
    promotion_payload: dict
    submit: bool = False
    project_id: str = None
    promotion_only: bool = False
    blocking_fields: tuple = ()


class PlanExecutor:
    """Execute one project/promotion transaction for every material source."""

    def __init__(self, client, progress_callback=None):
        self.client = client
        self.progress_callback = progress_callback

    @classmethod
    def from_credentials(
        cls,
        base_url,
        access_token,
        progress_callback=None,
        client_factory=OceanEngineClient,
    ):
        return cls(
            client_factory(base_url, access_token),
            progress_callback=progress_callback,
        )

    def execute(self, request):
        result = {
            "project_endpoint": PROJECT_CREATE_PATH,
            "promotion_endpoint": PROMOTION_CREATE_PATH,
            "project_payload": request.project_payload,
            "promotion_payload": request.promotion_payload,
        }
        if not request.submit:
            return result
        if request.blocking_fields:
            return {
                **result,
                "submit_blocked": True,
                "blocking_fields": list(request.blocking_fields),
            }

        project_id = request.project_id
        if request.promotion_only:
            if not project_id or project_id == "{{project_id}}":
                return {
                    **result,
                    "submit_blocked": True,
                    "blocking_fields": ["project_id"],
                }
        else:
            project_response = self.client.post(PROJECT_CREATE_PATH, request.project_payload)
            result["project_response"] = project_response
            project_id = get_path(project_response, "data.project_id")
            if not project_id:
                result.update({"submit_failed": True, "failure_stage": "project_create"})
                self._notify("project_failed", response=project_response)
                return result
            self._notify("project_created", project_id=str(project_id), response=project_response)

        promotion_payload = dict(request.promotion_payload)
        promotion_payload["project_id"] = project_id
        promotion_response = self.client.post(PROMOTION_CREATE_PATH, promotion_payload)
        result["promotion_payload"] = promotion_payload
        result["promotion_response"] = promotion_response
        promotion_id = get_path(promotion_response, "data.promotion_id")
        if not promotion_id:
            result.update({
                "submit_failed": True,
                "failure_stage": "promotion_create",
                "project_id": project_id,
            })
            self._notify(
                "promotion_failed",
                project_id=str(project_id),
                response=promotion_response,
            )
            return result

        result.update({"project_id": project_id, "promotion_id": promotion_id})
        self._notify(
            "completed",
            project_id=str(project_id),
            promotion_id=str(promotion_id),
            response=promotion_response,
        )
        return result

    def _notify(self, status, **details):
        if self.progress_callback:
            self.progress_callback({"status": status, **details})
