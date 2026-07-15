#!/usr/bin/env python3
from dataclasses import dataclass
from typing import Optional

import ocean_watch.auth.channels as channels

DEFAULT_TOKEN_BASE_URL = "https://ad.oceanengine.com/open_api"
DEFAULT_BUSINESS_BASE_URL = "https://api.oceanengine.com/open_api"
ACCESS_TOKEN_PATH = "/oauth2/access_token/"
REFRESH_TOKEN_PATH = "/oauth2/refresh_token/"
AUTHORIZED_ACCOUNT_PATH = "/oauth2/advertiser/get/"
CUSTOMER_CENTER_ADVERTISER_PATH = "/2/customer_center/advertiser/list/"
EBP_ADVERTISER_PATH = "/2/ebp/advertiser/list/"
ADVERTISER_INFO_PATH = "/2/advertiser/info/"
QIANCHUAN_SHOP_ADVERTISER_PATH = "/v1.0/qianchuan/shop/advertiser/list/"
AGENT_ADVERTISER_PATH = "/2/agent/advertiser/select/"


@dataclass(frozen=True)
class RoleExpansion:
    path: str
    base_params: dict
    list_key: str
    id_keys: tuple
    base_url: Optional[str] = None
    optional_permission_codes: tuple = ()


class ChannelAdapter:
    channel = None
    authorize_url = None
    token_base_url = DEFAULT_TOKEN_BASE_URL
    business_base_url = DEFAULT_BUSINESS_BASE_URL
    access_token_path = ACCESS_TOKEN_PATH
    refresh_token_path = REFRESH_TOKEN_PATH
    authorized_account_path = AUTHORIZED_ACCOUNT_PATH
    advertiser_info_path = ADVERTISER_INFO_PATH

    def authorize_params(self, app_id, state, redirect_uri):
        return {
            "app_id": app_id,
            "state": state,
            "redirect_uri": redirect_uri,
        }

    def direct_advertiser_id(self, account):
        return first_positive_id(account, ("advertiser_id", "account_id", "account_string_id"))

    def role_expansion(self, account):
        raise NotImplementedError


class MarketingChannelAdapter(ChannelAdapter):
    channel = "marketing"
    authorize_url = "https://ad.oceanengine.com/openapi/audit/oauth.html"

    def role_expansion(self, account):
        role = account_role(account)
        if role in {"CUSTOMER_ADMIN", "CUSTOMER_OPERATOR"}:
            source_id = first_positive_id(account, ("account_id", "account_string_id"))
            if source_id is None:
                return None
            return RoleExpansion(
                path=CUSTOMER_CENTER_ADVERTISER_PATH,
                base_params={"cc_account_id": source_id, "account_source": "AD"},
                list_key="list",
                id_keys=("advertiser_id", "account_id"),
            )
        if role in {
            "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN",
            "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR",
        }:
            source_id = first_positive_id(account, ("account_id", "account_string_id"))
            if source_id is None:
                return None
            return RoleExpansion(
                path=EBP_ADVERTISER_PATH,
                base_params={
                    "enterprise_organization_id": source_id,
                    "account_source": "AD",
                },
                list_key="account_list",
                id_keys=("account_id", "advertiser_id"),
            )
        return None


class QianchuanChannelAdapter(ChannelAdapter):
    channel = "qianchuan"
    authorize_url = "https://qianchuan.jinritemai.com/openapi/qc/audit/oauth.html"

    def authorize_params(self, app_id, state, redirect_uri):
        return {
            **super().authorize_params(app_id, state, redirect_uri),
            "material_auth": 1,
        }

    def role_expansion(self, account):
        role = account_role(account)
        if role == "PLATFORM_ROLE_QIANCHUAN_AGENT":
            source_id = first_positive_id(
                account,
                ("account_id", "account_string_id", "advertiser_id"),
            )
            if source_id is None:
                return None
            return RoleExpansion(
                path=AGENT_ADVERTISER_PATH,
                base_params={"advertiser_id": source_id},
                list_key="list",
                id_keys=("advertiser_id", "account_id", "id"),
                base_url=DEFAULT_TOKEN_BASE_URL,
                optional_permission_codes=(40002,),
            )
        if role == "PLATFORM_ROLE_SHOP_ACCOUNT":
            source_id = first_positive_id(
                account,
                ("shop_id", "account_id", "account_string_id"),
            )
            if source_id is None:
                return None
            return RoleExpansion(
                path=QIANCHUAN_SHOP_ADVERTISER_PATH,
                base_params={"shop_id": source_id, "permission": ["QC_AWEME"]},
                list_key="list",
                id_keys=("advertiser_id", "account_id", "id"),
            )
        if role in {"CUSTOMER_ADMIN", "CUSTOMER_OPERATOR"}:
            source_id = first_positive_id(account, ("account_id", "account_string_id"))
            if source_id is None:
                return None
            return RoleExpansion(
                path=CUSTOMER_CENTER_ADVERTISER_PATH,
                base_params={"cc_account_id": source_id, "account_source": "QIANCHUAN"},
                list_key="list",
                id_keys=("advertiser_id", "account_id"),
            )
        if role in {
            "PLATFORM_ROLE_ENTERPRISE_BP_ADMIN",
            "PLATFORM_ROLE_ENTERPRISE_BP_OPERATOR",
        }:
            source_id = first_positive_id(account, ("account_id", "account_string_id"))
            if source_id is None:
                return None
            return RoleExpansion(
                path=EBP_ADVERTISER_PATH,
                base_params={
                    "enterprise_organization_id": source_id,
                    "account_source": "QIANCHUAN",
                },
                list_key="account_list",
                id_keys=("account_id", "advertiser_id"),
            )
        return None


ADAPTERS = {
    "marketing": MarketingChannelAdapter(),
    "qianchuan": QianchuanChannelAdapter(),
}


def account_role(account):
    return account.get("account_role") or account.get("account_type") or "UNKNOWN"


def first_positive_id(account, keys):
    for key in keys:
        value = account.get(key)
        try:
            parsed = int(value)
        except (TypeError, ValueError):
            continue
        if parsed > 0:
            return parsed
    return None


def get_adapter(channel, capability=None):
    normalized, _ = channels.get(channel, capability=capability)
    return ADAPTERS[normalized]
