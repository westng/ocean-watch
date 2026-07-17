from ocean_watch.core.errors import ConfigurationError


def positive_integer(value, field, maximum=None):
    if isinstance(value, bool):
        raise ConfigurationError(f"{field} must be a positive integer")
    text = str(value or "").strip()
    if not text.isdigit() or int(text) <= 0:
        raise ConfigurationError(f"{field} must be a positive integer")
    parsed = int(text)
    if maximum is not None and parsed > maximum:
        raise ConfigurationError(f"{field} must not exceed {maximum}")
    return parsed
