import asyncio
import logging
from .config import Config
from .telegram import create_telegram_bot
from .server import create_server

cfg = Config()

logging.basicConfig(level=cfg.logging_level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger("main")


async def main():
    # bot = await create_telegram_bot(cfg)
    server = await create_server(cfg)
    async with create_telegram_bot(cfg) as bot:
        try:
            await asyncio.Event().wait()
        except (KeyboardInterrupt, SystemExit):
            pass

if __name__ == "__main__":
    asyncio.run(main())
