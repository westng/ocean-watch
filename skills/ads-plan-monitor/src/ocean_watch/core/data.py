import copy


def get_path(data, dotted, default=None):
    current = data
    for part in dotted.split("."):
        if not isinstance(current, dict) or part not in current:
            return default
        current = current[part]
    return current


def deep_merge(base, override):
    if not isinstance(base, dict) or not isinstance(override, dict):
        return copy.deepcopy(override)
    result = copy.deepcopy(base)
    for key, value in override.items():
        if isinstance(value, dict) and isinstance(result.get(key), dict):
            result[key] = deep_merge(result[key], value)
        else:
            result[key] = copy.deepcopy(value)
    return result


def is_missing(value, placeholder_prefix="REPLACE_WITH"):
    if value is None:
        return True
    if isinstance(value, str):
        stripped = value.strip()
        return not stripped or stripped.startswith(placeholder_prefix)
    if isinstance(value, list):
        return not value
    return False


def split_csv(values):
    result = []
    for value in values or []:
        result.extend(part.strip() for part in str(value).split(",") if part.strip())
    return result
