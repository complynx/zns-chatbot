from typing import Any, Optional
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
from .photo_task import get_by_uuid, real_frame_size
import logging

logger = logging.getLogger(__name__)

class FitFrameHandler(tornado.web.RequestHandler):
    async def get(self):
        id_str = self.get_query_argument("id", default="")

        try:
            task = get_by_uuid(id_str)
            self.render(
                "fit_frame.html",
                task=task,
                id=id_str,
                real_frame_size=real_frame_size,
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)



class PhotoHandler(tornado.web.StaticFileHandler):
    def __init__(self, application: tornado.web.Application, request: HTTPServerRequest, **kwargs: Any) -> None:
        super().__init__(application, request, path="", **kwargs)
    
    @classmethod
    def get_absolute_path(cls, root: str, path: str) -> str:
        try:
            task = get_by_uuid(path)
            if task.file is None:
                return ""
            return task.file
        except (KeyError, ValueError):
            return ""
    
    def validate_absolute_path(self, root: str, absolute_path: str) -> Optional[str]:
        if absolute_path == "" or not os.path.isfile(absolute_path):
            raise tornado.web.HTTPError(404)
        return absolute_path



async def create_server(config: Config):
    tornado.platform.asyncio.AsyncIOMainLoop().install()
    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server_port)

    return app
