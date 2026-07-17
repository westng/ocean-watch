CHANNEL_LABELS = {
    "marketing": "巨量营销",
    "qianchuan": "巨量千川",
}
MARKETING_TEMPLATE_TYPE_LABELS = {
    "ACCOUNT_UPLOAD": "混剪素材",
    "CREATOR_AUTHORIZED": "原生素材",
}


def required_segment(value, field):
    text = str(value or "").strip()
    if not text:
        raise ValueError(f"{field} is required for a business template name")
    return text


def product_id_segment(product_ids):
    values = product_ids if isinstance(product_ids, (list, tuple)) else [product_ids]
    normalized = [required_segment(value, "product_id") for value in values]
    if not normalized:
        raise ValueError("product_id is required for a business template name")
    return "/".join(normalized)


def format_business_template_name(
    channel,
    advertiser_id,
    product_name,
    product_ids,
    template_type,
):
    try:
        channel_label = CHANNEL_LABELS[channel]
    except KeyError as exc:
        raise ValueError(f"unsupported business template channel: {channel}") from exc
    return "-".join((
        channel_label,
        required_segment(advertiser_id, "advertiser_id"),
        required_segment(product_name, "product_name"),
        product_id_segment(product_ids),
        required_segment(template_type, "template_type"),
    ))


def format_marketing_template_name(
    advertiser_id,
    product_name,
    product_id,
    material_source_type,
):
    try:
        template_type = MARKETING_TEMPLATE_TYPE_LABELS[material_source_type]
    except KeyError as exc:
        raise ValueError(
            f"unsupported Marketing template type: {material_source_type}"
        ) from exc
    return format_business_template_name(
        "marketing",
        advertiser_id,
        product_name,
        product_id,
        template_type,
    )


def format_qianchuan_live_template_name(advertiser_id, creator_name, aweme_id):
    return "-".join((
        CHANNEL_LABELS["qianchuan"],
        required_segment(advertiser_id, "advertiser_id"),
        required_segment(creator_name, "creator_name"),
        required_segment(aweme_id, "aweme_id"),
        "直播全域",
    ))
