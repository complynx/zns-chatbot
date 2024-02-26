PRIORITY_NOT_ACCEPTING = -1000
PRIORITY_BASIC = 0

class BasePlugin():
    name = "_BasePlugin"
    def __init__(self, app) -> None:
        self.app = app

    def test_message(self, message):
        return PRIORITY_NOT_ACCEPTING, None

    async def handle_message(self, updater):
        return
