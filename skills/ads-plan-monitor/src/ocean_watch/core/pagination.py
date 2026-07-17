from ocean_watch.core.errors import ApiError


def declared_page_count(page_info, *, source, page, row_count, expected=None):
    raw_total = page_info.get("total_page") if isinstance(page_info, dict) else None
    if isinstance(raw_total, bool):
        total = None
    elif isinstance(raw_total, int):
        total = raw_total
    elif isinstance(raw_total, float) and raw_total.is_integer():
        total = int(raw_total)
    elif isinstance(raw_total, str) and raw_total.strip().isdigit():
        total = int(raw_total.strip())
    else:
        total = None
    if total is None or total < 0:
        raise ApiError(
            "API pagination returned an invalid total_page",
            {"source": source, "page": page, "total_page": raw_total},
        )
    if total == 0 and row_count:
        raise ApiError(
            "API pagination contradicts returned rows",
            {"source": source, "page": page, "total_page": total},
        )
    if expected is not None and total != expected:
        raise ApiError(
            "API pagination changed during traversal",
            {
                "source": source,
                "page": page,
                "total_page": total,
                "expected": expected,
            },
        )
    return total
