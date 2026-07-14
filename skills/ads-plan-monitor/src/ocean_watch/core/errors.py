class OceanWatchError(Exception):
    """Base error carrying a stable machine-readable code."""

    def __init__(self, code, message, details=None, exit_code=1):
        super().__init__(message)
        self.code = code
        self.message = message
        self.details = details or {}
        self.exit_code = exit_code

    def as_dict(self):
        return {
            "ok": False,
            "error": {
                "code": self.code,
                "message": self.message,
                "details": self.details,
            },
        }


class ConfigurationError(OceanWatchError):
    def __init__(self, message, details=None):
        super().__init__("configuration_error", message, details, exit_code=2)


class ApiError(OceanWatchError):
    def __init__(self, message, details=None):
        super().__init__("api_error", message, details)
