import asyncio
import logging
from .config import Config
from .telegram import create_telegram_bot, bot_starter, bot_stopper
from .server import create_server

cfg = Config()

logging.basicConfig(level=cfg.logging_level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger("main")


async def main():
    bot = create_telegram_bot(cfg)
    server = await create_server(cfg)
    try:
        await bot_starter(bot)
        logger.info("running event loop")
        await asyncio.Event().wait()
    except (KeyboardInterrupt, SystemExit) as e:
        logger.info("terminated gracefully...")
        raise e
    except Exception as e:
        logger.exception(f"got exception during io loop: {e}")
        raise e
    finally:
        await bot_stopper(bot)

if __name__ == "__main__":
    asyncio.run(main())
