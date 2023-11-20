import asyncio
import logging
from .config import Config
from .telegram import create_telegram_bot
from .cached_localization import Localization
from .assistant import Assistant
from fluent.runtime import FluentResourceLoader
from motor.motor_asyncio import AsyncIOMotorClient

logger = logging.getLogger(__name__)

class App(object):
    bot = None
    localization = None
    mongodb = None
    users_collection = None
    assistant = None

async def main(cfg: Config):
    app = App()
    loader = FluentResourceLoader(cfg.localization.path)
    app.localization = Localization(loader, cfg.localization.file, cfg.localization.fallbacks)

    if cfg.mongo_db.address != "":
        logger.info(f"db address {cfg.mongo_db.address}")
        app.mongodb = AsyncIOMotorClient(cfg.mongo_db.address).get_database()
        app.users_collection = app.mongodb[cfg.mongo_db.users_collection]

    app.assistant = Assistant(app, cfg)

    try:
        async with create_telegram_bot(cfg, app) as bot:
            logger.info("running event loop")
            await asyncio.Event().wait()
    except (KeyboardInterrupt, SystemExit):
        pass
    except Exception as e:
        logger.exception(f"got exception {e}")

if __name__ == "__main__":
    cfg = Config()
    logger = logging.getLogger("MAIN")

    logging.basicConfig(level=cfg.logging.level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')

    # Get or create a specific logger
    httpx_logger = logging.getLogger('httpx')
    if cfg.logging.level != "DEBUG":
        httpx_logger.setLevel(logging.WARNING)

    asyncio.run(main(cfg))
