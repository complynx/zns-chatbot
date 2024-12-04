import asyncio
import logging

from .config import Config

log = logging.getLogger(__name__)


async def main(cfg: Config) -> None:
    log.debug(cfg)


if __name__ == "__main__":
    cfg = Config()
    log = logging.getLogger("MAIN")

    logging.basicConfig(
        level=cfg.logging.level,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    asyncio.run(main(cfg))
