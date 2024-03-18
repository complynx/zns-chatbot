from typing import Any, Optional
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
import logging

logger = logging.getLogger(__name__)

class FitFrameHandler(tornado.web.RequestHandler):
    def initialize(self, app):
        self.app = app

    async def get(self):
        file = self.get_query_argument("file", default="")
        locale_str = self.get_query_argument("locale", default="en")
        l = lambda s: self.app.localization(s, locale=locale_str)

        try:
            self.render(
                "fit_frame.html",
                file=file,
                real_frame_size=self.app.config.photo.frame_size,
                debug_code="",
                help_desktop=l("frame-mover-help-desktop"),
                help_mobile=l("frame-mover-help-mobile"),
                frame_mover_help_unified=l("frame-mover-help-unified"),
                finish_button_text=l("frame-mover-finish-button-text"),
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)



async def create_server(config: Config, base_app):
    tornado.platform.asyncio.AsyncIOMainLoop().install()

    class PhotoHandler(tornado.web.StaticFileHandler):
        def __init__(self, application: tornado.web.Application, request: HTTPServerRequest, **kwargs: Any) -> None:
            super().__init__(application, request, path="", **kwargs)

        def get_absolute_path(self, root: str, path: str) -> str:
            return self.abs_path
        
        def validate_absolute_path(self, root: str, absolute_path: str) -> Optional[str]:
            logger.debug(f"abs_path {absolute_path}")
            if absolute_path == "" or not os.path.isfile(absolute_path):
                raise tornado.web.HTTPError(404)
            return absolute_path
        
        async def get(self, path: str, include_body: bool = True) -> None:
            path_part = self.parse_url_path(path)
            if path_part == "frame":
                self.abs_path = os.path.abspath(base_app.config.photo.frame_file)
            else:
                self.abs_path = await base_app.avatar.get_file(path_part)
            return await super().get(path, include_body)

    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler, {"app": base_app}),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server.port)
    base_app.server = app

    return app