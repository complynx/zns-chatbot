import hashlib
import hmac
import json
from operator import itemgetter
import tempfile
from typing import Any, ClassVar, Optional
from urllib.parse import parse_qs, parse_qsl, unquote
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
import logging
import random

logger = logging.getLogger(__name__)

class AuthError(Exception):
    status: ClassVar[int] = 403
    detail: str = "unknown auth error"

    @property
    def message(self) -> str:
        return f"Auth error occurred, detail: {self.detail}"

def validate(init_data: str, bot_token: str) -> dict[str, Any]:
    secret_key = hmac.new(key=b"WebAppData", msg=bot_token.encode(), digestmod=hashlib.sha256).digest()
    try:
        parsed_data = dict(parse_qsl(init_data, strict_parsing=True))
    except ValueError as err:
        logger.error("invalid init data: %s", err, exc_info=1)
        raise AuthError(detail="invalid init data") from err
    if "hash" not in parsed_data:
        logger.error(f"missing hash: {parsed_data}")
        raise AuthError(detail="missing hash")
    hash_ = parsed_data.pop("hash")
    data_check_string = "\n".join(f"{k}={v}" for k, v in sorted(parsed_data.items(), key=itemgetter(0)))
    calculated_hash = hmac.new(
        key=secret_key,
        msg=data_check_string.encode(),
        digestmod=hashlib.sha256,
    ).hexdigest()
    if calculated_hash != hash_:
        logger.error(f"invalid hash {calculated_hash} waiting {hash_}")
        raise AuthError(detail="invalid hash")
    logger.debug(f"validated: {parsed_data}")
    return parsed_data

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
                quality=self.app.config.photo.quality,
                debug_code="",
                help_desktop=l("frame-mover-help-desktop"),
                help_mobile=l("frame-mover-help-mobile"),
                frame_mover_help_unified=l("frame-mover-help-unified"),
                finish_button_text=l("frame-mover-finish-button-text"),
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)
    
    async def put(self):
        initData = self.get_argument('initData', default=None, strip=False)

        if initData:
            initData = validate(initData, self.app.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            user = json.loads(initData['user'])
            with tempfile.NamedTemporaryFile(suffix='.jpg', delete=False) as temp_file:
                temp_name = temp_file.name
                temp_file.write(self.request.body)
            await self.app.bot.bot.send_document(
                user["id"],
                temp_name,
                filename="avatar.jpg",
            )
            os.remove(temp_name)

            self.set_status(200)
            self.write({'message': 'Image uploaded successfully'})

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error saving image: %s",e, exc_info=1)

class MenuHandler(tornado.web.RequestHandler):
    def initialize(self, app):
        self.app = app

    async def get(self):
        # file = self.get_query_argument("file", default="")
        # locale_str = self.get_query_argument("locale", default="en")
        # l = lambda s: self.app.localization(s, locale=locale_str)

        try:
            self.render(
                "menu.html",
                user_carts=False,
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)
    
    async def post(self):
        initData = self.get_argument('initData', default=None, strip=False)

        if initData:
            initData = validate(initData, self.app.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            user = json.loads(initData['user'])
            carts = json.loads(self.request.body)
            logger.info(f"user {user["id"]} carts {carts}")
            # with tempfile.NamedTemporaryFile(suffix='.jpg', delete=False) as temp_file:
            #     temp_name = temp_file.name
            #     temp_file.write(self.request.body)
            # await self.app.bot.bot.send_document(
            #     user["id"],
            #     temp_name,
            #     filename="avatar.jpg",
            # )
            # os.remove(temp_name)

            # self.set_status(200)
            # self.write({'message': 'Image uploaded successfully'})

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error saving menu: %s",e, exc_info=1)

class ErrorHandler(tornado.web.RequestHandler):
    def initialize(self, app):
        self.app = app
    async def post(self):
        try:
            initData = self.get_argument('initData', default=None, strip=False)
            if initData:
                initData = validate(initData, self.app.config.telegram.token.get_secret_value())
                logger.error("client error: %s %s", str(self.request.body), initData)
        except Exception:
            pass

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
        (r"/menu", MenuHandler, {"app": base_app}),
        (r"/error", ErrorHandler, {"app": base_app}),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server.port)
    base_app.server = app

    return app