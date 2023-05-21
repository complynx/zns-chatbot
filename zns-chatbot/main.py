import asyncio
import logging
from .config import Config
from .telegram import create_telegram_bot
from .server import create_server
from .photo_task import init_photo_tasker
from .food import MealContext
import threading
import time

cfg = Config()

logging.basicConfig(level=cfg.logging_level, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger("main")

class App(object):
    bot = None
    server = None

    def __init__(self):
        self._meal_sessions = dict()
    #     def cleaner_fn():
    #         while True:
    #             time.sleep(3600) # every hour
    #             self.clean_meal_sessions()
    #     self._cleaner = threading.Thread(target=cleaner_fn, daemon=True)
    #     self._cleaner.start()
    
    # def clean_meal_sessions(self):
    #     for id in self._meal_sessions.keys():
    #         if self._meal_sessions[id].is_old():
    #             del self._meal_sessions[id]

    # def add_meal_session(self, meal_session: MealContext):
    #     self._meal_sessions[meal_session.id] = meal_session

    # def get_meal_session(self, id):
    #     return self._meal_sessions[id]

async def main():
    app = App()
    init_photo_tasker(cfg)
    await create_server(cfg, app)
    try:
        async with create_telegram_bot(cfg, app) as bot:
            logger.info("running event loop")
            await asyncio.Event().wait()
    except (KeyboardInterrupt, SystemExit):
        pass
    except Exception as e:
        logger.exception(f"got exception {e}")

if __name__ == "__main__":
    asyncio.run(main())
