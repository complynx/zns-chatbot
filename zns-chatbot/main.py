import asyncio
import logging
from .config import config, Config
from .telegram_bot import create_telegram_bot
from .server import create_server
from .photo_task import init_photo_tasker
from .food import FoodStorage

logger = logging.getLogger(__name__)

class App(object):
    bot = None
    server = None


async def main(cfg: Config):
    app = App()
    init_photo_tasker(cfg)
    food_storage = FoodStorage(cfg, app)
    await create_server(cfg, app)
    try:
        async with create_telegram_bot(cfg, app) as bot:
            logger.info("running event loop")
            await food_storage.checker()
    except (KeyboardInterrupt, SystemExit):
        pass
    except Exception as e:
        logger.exception(f"got exception {e}")

if __name__ == "__main__":
    cfg = config()
    logger = logging.getLogger("MAIN")

    logging.basicConfig(level=cfg.logging.level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')

    asyncio.run(main(cfg))
