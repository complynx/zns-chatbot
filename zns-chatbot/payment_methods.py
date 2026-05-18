from html import escape


IBAN_PRIVATE_VALUE = "private"
IBAN_EMPTY_VALUE = "noiban"


def normalize_iban_profile_value(value: str) -> str | None:
    normalized = " ".join(value.strip().split())
    if normalized == "":
        return None
    lowered = normalized.lower()
    if lowered == IBAN_PRIVATE_VALUE:
        return IBAN_PRIVATE_VALUE
    return normalized.upper()


def payment_iban_to_key(value: object) -> str:
    if not isinstance(value, str):
        return IBAN_EMPTY_VALUE
    normalized = normalize_iban_profile_value(value)
    if normalized is None:
        return IBAN_EMPTY_VALUE
    if normalized == IBAN_PRIVATE_VALUE:
        return IBAN_PRIVATE_VALUE
    return escape(normalized, quote=False)
