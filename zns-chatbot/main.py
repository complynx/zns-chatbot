import asyncio
import logging
from .config import Config
from .telegram import create_telegram_bot
from .server import create_server
from .photo_task import init_photo_tasker

cfg = Config()

logging.basicConfig(level=cfg.logging_level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger("main")


async def main():
    init_photo_tasker(cfg)
    server = await create_server(cfg)
    try:
        async with create_telegram_bot(cfg) as bot:
            logger.info("running event loop")
            await asyncio.Event().wait()
    except (KeyboardInterrupt, SystemExit):
        pass
    except Exception as e:
        logger.exception(f"got exception {e}")

if __name__ == "__main__":
    asyncio.run(main())
