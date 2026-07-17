#!/usr/bin/env python3
from ocean_watch.auth import authorization_store, channels


def authorization_summary(channel_state):
    return {
        "authorization_count": len(channel_state.get("authorizations") or {}),
        "authorized_advertiser_ids": tuple(
            sorted(
                (channel_state.get("advertiser_index") or {}).keys(),
                key=int,
            )
        ),
    }


def normalized_candidate(values):
    for value in values:
        text = str(value or "").strip()
        if not text or text.startswith("REPLACE_WITH"):
            continue
        try:
            return authorization_store.normalize_id(text, "advertiser_id")
        except ValueError:
            continue
    return None


def prompt_advertiser_id(
    channel,
    candidate_values,
    input_fn=input,
    output_fn=print,
    channel_state=None,
):
    summary = authorization_summary(channel_state or {})
    authorization_count = summary["authorization_count"]
    authorized_ids = summary["authorized_advertiser_ids"]
    authorized_set = set(authorized_ids)
    display_name = channels.CHANNELS[channel]["display_name"]

    default = normalized_candidate(candidate_values)
    if authorized_set and default not in authorized_set:
        default = None
    if default is None and len(authorized_ids) == 1:
        default = authorized_ids[0]

    if authorized_ids:
        output_fn(
            f"当前{display_name}授权覆盖 {len(authorized_ids)} 个广告主，"
            "请输入模板要绑定的广告主 ID。"
        )
    elif authorization_count:
        output_fn(
            f"当前{display_name}授权尚未同步出可用广告主；"
            "可以先创建模板，真实投放前必须重新校验。"
        )
    else:
        output_fn(
            f"当前{display_name}尚未授权；可以先创建模板，"
            "真实投放前必须完成授权并校验广告主。"
        )

    suffix = f" [{default}]" if default else ""
    while True:
        raw = input_fn(f"广告主 ID{suffix}: ").strip()
        value = raw or default
        try:
            advertiser_id = authorization_store.normalize_id(value, "advertiser_id")
        except ValueError:
            output_fn("广告主 ID 必须是正整数，请重新输入。")
            continue
        if authorized_set and advertiser_id not in authorized_set:
            output_fn(
                f"广告主 {advertiser_id} 不在当前{display_name}授权范围内，请重新输入。"
            )
            continue
        return advertiser_id, {
            "channel": channel,
            "status": "VERIFIED" if authorized_set else "UNVERIFIED",
            "authorized_advertiser_count": len(authorized_ids),
            "reason": (
                None
                if authorized_set
                else "ADVERTISER_SYNC_EMPTY"
                if authorization_count
                else "CHANNEL_NOT_AUTHORIZED"
            ),
        }
