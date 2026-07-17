#!/usr/bin/env python3
import argparse

from ocean_watch.auth import authorization_store, channels
from ocean_watch.templates import (
    manage_plan_templates,
    manage_qianchuan_templates,
    template_advertiser_binding,
)

CHANNEL_ORDER = ("marketing", "qianchuan")
CHANNEL_HANDLERS = {
    "marketing": (manage_plan_templates, "create-wizard"),
    "qianchuan": (manage_qianchuan_templates, "create-wizard"),
}
MARKETING_MATERIAL_SOURCES = (
    ("ACCOUNT_UPLOAD", "混剪素材（账户上传）"),
    ("CREATOR_AUTHORIZED", "原生素材（达人授权）"),
)


def authorization_label(channel):
    status = template_advertiser_binding.authorization_summary(
        authorization_store.load_channel_state(channel)
    )
    authorization_count = status["authorization_count"]
    advertiser_count = len(status["authorized_advertiser_ids"])
    if authorization_count:
        return f"已授权，{advertiser_count} 个广告主"
    return "未授权，可先创建模板，投放前需授权"


def select_channel(input_fn=input, output_fn=print):
    output_fn("创建投放模板渠道：")
    for index, channel in enumerate(CHANNEL_ORDER):
        display_name = channels.CHANNELS[channel]["display_name"]
        output_fn(f"  {index}. {display_name}（{authorization_label(channel)}）")
    while True:
        selected = input_fn("请选择渠道编号: ").strip()
        if selected.isdigit() and 0 <= int(selected) < len(CHANNEL_ORDER):
            return CHANNEL_ORDER[int(selected)]


def select_marketing_material_source(input_fn=input, output_fn=print):
    output_fn("巨量营销素材模式：")
    for index, (_, label) in enumerate(MARKETING_MATERIAL_SOURCES):
        output_fn(f"  {index}. {label}")
    while True:
        selected = input_fn("请选择素材模式编号: ").strip()
        if selected.isdigit() and 0 <= int(selected) < len(MARKETING_MATERIAL_SOURCES):
            return MARKETING_MATERIAL_SOURCES[int(selected)][0]


def main(argv=None, input_fn=input, output_fn=print):
    parser = argparse.ArgumentParser(description="Route template creation by business channel.")
    parser.add_argument("action", choices=("create",))
    parser.add_argument("--channel", choices=CHANNEL_ORDER)
    parser.add_argument(
        "--material-source-type",
        choices=tuple(source for source, _ in MARKETING_MATERIAL_SOURCES),
    )
    args, forwarded = parser.parse_known_args(argv)

    channel = args.channel or select_channel(input_fn=input_fn, output_fn=output_fn)
    if channel == "qianchuan" and args.material_source_type:
        parser.error("--material-source-type is only valid for the marketing channel")
    material_source_type = args.material_source_type
    if channel == "marketing" and material_source_type is None:
        material_source_type = select_marketing_material_source(
            input_fn=input_fn,
            output_fn=output_fn,
        )
    handler_module, handler_action = CHANNEL_HANDLERS[channel]
    handler_args = [handler_action, *forwarded]
    if material_source_type:
        handler_args.extend(["--material-source-type", material_source_type])
    return handler_module.main(handler_args)


if __name__ == "__main__":
    raise SystemExit(main())
