import functools
from typing import Any, Dict, List
from fluent.runtime import FluentLocalization, FluentResourceLoader
import logging

logger = logging.getLogger(__name__)

def locale_unify(locale: str) -> str:
    return locale.lower().replace("_", "-")

class Localization:
    def __init__(
            self,
            loader: FluentResourceLoader,
            base:str = "translations.ftl",
            fallbacks: List[str] = ["en_US", "en"],
        ):
        self.loader = loader
        self.fallbacks = [locale_unify(fb) for fb in fallbacks]
        self.base = base
        logger.debug(f"Creating localization from {self.base} and fallbacks {self.fallbacks}")

    def locale_to_list(self, locale: str) -> List[str]:
        split = locale.split("-", maxsplit=1)
        if len(split) > 0:
            return [locale, split[0]]
        return [locale]

    @functools.cache
    def get_locale(self, locale: List[str]|str|None = None):
        logger.debug(f"Loading locale {locale}")
        locales = self.fallbacks
        if locale is not None:
            if isinstance(locale, str):
                locale = self.locale_to_list(locale)
            if isinstance(locale, List):
                locales = [locale_unify(l) for l in locale]
                locales.extend(self.fallbacks)
        return FluentLocalization(locales, [self.base], self.loader)

    def __call__(self, key, args: Dict[str, Any]|None = None, locale: List[str]|str|None = None):
        ret = self.get_locale(locale).format_value(key, args)
        logger.debug(f"translating {key} with args {args} and locale {locale}, result: {ret}")
        return ret
