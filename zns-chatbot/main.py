import asyncio
import logging
from .config import Config
from .telegram_bot import create_telegram_bot
from .server import create_server
from .photo_task import init_photo_tasker
from .food import checker

cfg = Config()

logging.basicConfig(level=cfg.logging_level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger("main")

class App(object):
    bot = None
    server = None


async def main():
    app = App()
    init_photo_tasker(cfg)
    await create_server(cfg, app)
    try:
        async with create_telegram_bot(cfg, app) as bot:
            logger.info("running event loop")
            await checker(app)
    except (KeyboardInterrupt, SystemExit):
        pass
    except Exception as e:
        logger.exception(f"got exception {e}")

if __name__ == "__main__":
    asyncio.run(main())
