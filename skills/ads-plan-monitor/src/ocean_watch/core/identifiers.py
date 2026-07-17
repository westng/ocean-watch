from ocean_watch.core.data import get_path, is_missing
from ocean_watch.core.errors import ConfigurationError


def require_advertiser_id(config, explicit=None):
    value = explicit if explicit is not None else get_path(config, "account.advertiser_id")
    if is_missing(value):
        raise ConfigurationError(
            "advertiser_id is required; pass --advertiser-id for the target account"
        )
    normalized = str(value).strip()
    if not normalized.isdigit():
        raise ConfigurationError("advertiser_id must be a decimal string")
    return normalized
