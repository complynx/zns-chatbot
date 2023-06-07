import asyncio
import logging
from .config import Config
from .telegram_bot import create_telegram_bot
from .server import create_server
from .photo_task import init_photo_tasker
from .food import FoodStorage
from .massage import MassageSystem

logger = logging.getLogger(__name__)

class App(object):
    bot = None
    server = None


async def main(cfg: Config):
    app = App()
    init_photo_tasker(cfg)
    food_storage = FoodStorage(cfg, app)
    massage = MassageSystem(cfg, app)
    await create_server(cfg, app)
    try:
        async with create_telegram_bot(cfg, app) as bot:
            logger.info("running event loop")
            await massage.notificator()
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
