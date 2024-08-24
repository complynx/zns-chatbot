import hashlib
import hmac
import json
from operator import itemgetter
import tempfile
from typing import Any, ClassVar, Optional
from urllib.parse import parse_qsl, urlparse
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
import logging
from telegram import Bot

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

class RequestHandlerWithApp(tornado.web.RequestHandler):
    def initialize(self, app):
        self.app = app
        self.config: Config = app.config
    
    @property
    def bot(self) -> Bot:
        return self.app.bot.bot
    
    @property
    def user_cookie(self) -> str:
        return self.bot.name[1:]+"_user"
    
    def get_user_id(self, max_age_days: float|None=None):
        if max_age_days is None:
            max_age_days=self.config.server.auth_timeout
        user_id = self.get_signed_cookie(
            self.user_cookie,
            max_age_days=max_age_days,
        )
        if user_id is None:
            return None
        return int(user_id.decode('utf-8'))

class FitFrameHandler(RequestHandlerWithApp):
    async def get(self):
        file = self.get_query_argument("file", default="")
        try:
            compensations_x = float(self.get_query_argument("compensations_x", default=""))
        except ValueError:
            compensations_x = "null"
        try:
            compensations_y = float(self.get_query_argument("compensations_y", default=""))
        except ValueError:
            compensations_y = "null"
        locale_str = self.get_query_argument("locale", default="en")
        l = lambda s: self.app.localization(s, locale=locale_str)

        try:
            self.render(
                "fit_frame.html",
                file=file,
                compensations_x=compensations_x,
                compensations_y=compensations_y,
                real_frame_size=self.config.photo.frame_size,
                quality=self.config.photo.quality,
                debug_code="",
                help_desktop=l("frame-mover-help-desktop"),
                help_mobile=l("frame-mover-help-mobile"),
                help_realign=l("frame-realign-message"),
                frame_mover_help_unified=l("frame-mover-help-unified"),
                finish_button_text=l("frame-mover-finish-button-text"),
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)
    
    async def put(self):
        initData = self.get_argument('initData', default=None, strip=False)
        compensations = self.get_argument('compensations', default=None, strip=False)

        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
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
            await self.bot.send_document(
                user["id"],
                temp_name,
                filename="avatar.jpg",
            )
            os.remove(temp_name)

            self.set_status(200)
            self.write({'message': 'Image uploaded successfully'})

            if compensations:
                from .tg_state import TGState
                compensations = json.loads(compensations)
                state = TGState(user["id"], self.app)
                await state.update_user({
                    "$set": {
                        "avatar_compensations": compensations
                    }
                })

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error saving image: %s",e, exc_info=1)
class GetCompensationsHandler(RequestHandlerWithApp):
    async def get(self):
        initData = self.get_argument('initData', default=None, strip=False)
        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            from .tg_state import TGState
            user = json.loads(initData['user'])
            state = TGState(user["id"], self.app)
            user = await state.get_user()
            if "avatar_compensations" in user:
                self.set_status(200)
                self.write(user["avatar_compensations"])
            else:
                self.set_status(200)
                self.write({})

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error saving image: %s",e, exc_info=1)

class MenuHandler(RequestHandlerWithApp):
    async def get(self):
        order_id = self.get_query_argument("order", default="")
        carts = None
        read_only = False
        if order_id != "":
            order = await self.app.food.get_order(order_id)
            if "carts" in order:
                carts = order["carts"]
            if "proof_received_at" in order:
                read_only = True

        try:
            self.render(
                "menu.html",
                order_id=json.dumps(order_id),
                user_carts=json.dumps(carts),
                read_only=json.dumps(read_only),
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)
    
    async def post(self):
        initData = self.get_argument('initData', default=None, strip=False)

        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            user = json.loads(initData['user'])
            carts = json.loads(self.request.body)
            logger.info(f"user {user['id']} carts {carts}")

            order_id = self.get_query_argument("order", default="")
            autosave = self.get_query_argument("autosave", default="")
            if order_id == "":
                logger.info(f"creating order for {user['id']}")
                await self.app.food.create_order(user['id'], carts)
                return
            order = await self.app.food.get_order(order_id)
            if order is None:
                self.set_status(404)
                logger.info("order not found")
                return
            if order["user_id"] != user["id"]:
                self.set_status(403)
                logger.error(f"user ID doesn't match")
                return
            logger.info(f"updating order {order_id} for {user['id']}")
            await self.app.food.set_carts(order, carts, autosave != "")

            self.set_status(200)
            self.write({'message': 'order saved'})

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error saving menu: %s",e, exc_info=1)

class FoodGetOrders(RequestHandlerWithApp):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", self.request.headers["Origin"])
        self.set_header("Access-Control-Allow-Credentials", "true")
    
    async def get(self):
        try:
            user_id = self.get_user_id()
            if user_id is None or not user_id in self.config.food.admins:
                self.set_status(401)
                self.write({'result': "unauthorized"})
                return
            orders = await self.app.food.get_all_orders()
            self.set_status(200)
            self.write({
                'orders': orders,
                'menu': self.app.food.menu
            })

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error getting orders: %s",e, exc_info=1)
    async def post(self):
        try:
            user_id = self.get_user_id()
            if user_id is None or not user_id in self.config.food.admins:
                self.set_status(401)
                self.write({'result': "unauthorized"})
                return
            out_of_stock = json.loads(self.request.body)
            await self.app.food.out_of_stock(out_of_stock)
            self.set_status(200)

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error getting orders: %s",e, exc_info=1)


class MassageTimetablePageHandler(RequestHandlerWithApp):
    async def get(self):
        try:
            self.render(
                "massage_timetable.html",
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)
class MassageTimetableHandler(RequestHandlerWithApp):
    async def get(self):
        initData = self.get_argument('initData', default=None, strip=False)

        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            from .plugins.massage import MassagePlugin
            massages: MassagePlugin = self.app.massages
            user = json.loads(initData['user'])
            # user = {'id':379278985 }
            specialist = await massages.get_specialist(user["id"])
            if specialist is None and user["id"] not in massages.config.food.admins:
                self.set_status(403)
                logger.info("not a specialist")
                return
            ret = await massages.get_all_massages_for_web()
            self.set_status(200)
            self.write(ret)
        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error saving menu: %s",e, exc_info=1)


def domain_from_uri(origin):
    parsed_origin = urlparse(origin)
    domain:str = parsed_origin.netloc
    if ":" in domain:
        domain = domain.split(":",2)[0]
    if domain.startswith('.'):
        domain = domain[1:]
    return domain

class AuthHandler(RequestHandlerWithApp):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", self.request.headers["Origin"])
        self.set_header("Access-Control-Allow-Credentials", "true")
    async def get(self):
        try:
            check = self.get_argument('check', default=None, strip=True)
            if check is not None:
                user_id = self.get_user_id(self.config.server.auth_timeout*0.7)
                if user_id is not None:
                    self.set_status(200)
                    self.write({'result': "authorized"})
                    return
                else:
                    self.set_status(401)
                    self.write({'result': "unauthorized"})
                    return
            try:
                req_info = {
                    "origin": self.request.headers["Origin"],
                    "useragent": self.request.headers["User-Agent"],
                }
            except KeyError as e:
                self.set_status(400)
                logger.warn(f"auth request without header: {e}")
                return
            try:
                req_info["ip"] = self.request.headers["X-Forwarded-For"]
            except KeyError:
                req_info["ip"] = str(self.request.remote_ip)
            username = self.get_argument('username', default="", strip=True)
            if username == "":
                self.set_status(400)
                logger.warn("auth without username")
                return
            user = await self.app.users_collection.find_one({
                "username": username,
                "bot_id": self.bot.id,
            })
            if user is None:
                self.set_status(404)
                logger.warn(f"auth username {username} not found")
                return
            logger.info(f"auth request for user {user['user_id']} with {username}")
            from .tg_state import TGState
            state = TGState(user["user_id"], self.app)
            await state.get_state()
            req = await self.app.auth.request_auth(state, req_info)
            await req.wait()
            if req.is_cancelled():
                logger.warn(f"auth request for user {user['user_id']} with {username} cancelled")
                self.set_status(401)
                self.write({'result': "cancelled"})
                return
            if req.is_authorized():
                self.set_signed_cookie(
                    self.user_cookie,
                    str(user["user_id"]),
                    expires_days=self.config.server.auth_timeout,
                )
                logger.info(f"auth request for user {user['user_id']} with {username} authorized")
                self.set_status(200)
                self.write({'result': "authorized"})
                return
            else:
                logger.info(f"auth request for user {user['user_id']} with {username} declined")
                self.set_status(401)
                self.write({'result': "unauthorized"})
                return

        except Exception as e:
            self.set_status(500)
            self.write({'error': "internal error"})
            logger.error("error getting orders: %s",e, exc_info=1)



class BotNameHandler(RequestHandlerWithApp):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", "*")
    async def get(self):
        self.set_status(200)
        self.write({'name': self.bot.name[1:]})


class ErrorHandler(RequestHandlerWithApp):
    async def post(self):
        try:
            initData = self.get_argument('initData', default=None, strip=False)
            if initData:
                initData = validate(initData, self.config.telegram.token.get_secret_value())
                logger.error("client error: %s %s", str(self.request.body), initData)
        except Exception:
            pass

class CustomStaticFileHandler(tornado.web.StaticFileHandler):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", "*")
    def get_content_type(self):
        return "application/javascript"

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
                self.abs_path = os.path.abspath(config.photo.frame_file)
            elif path_part == "flare":
                self.abs_path = os.path.abspath(config.photo.flare_file)
            else:
                self.abs_path = await base_app.avatar.get_file(path_part)
            return await super().get(path, include_body)
    
    secret = hashlib.sha256(f"secret {config.telegram.token.get_secret_value()} {config.mongo_db.address}".encode()).hexdigest()

    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler, {"app": base_app}),
        (r"/get_compensations", GetCompensationsHandler, {"app": base_app}),
        # (r"/massage_timetable", MassageTimetablePageHandler, {"app": base_app}),
        # (r"/massage_timetable_data", MassageTimetableHandler, {"app": base_app}),
        (r"/bot_name", BotNameHandler, {"app": base_app}),
        (r"/auth", AuthHandler, {"app": base_app}),
        # (r"/food_get_orders", FoodGetOrders, {"app": base_app}),
        # (r"/menu", MenuHandler, {"app": base_app}),
        (r"/error", ErrorHandler, {"app": base_app}),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/x/(.*)",CustomStaticFileHandler, {"path": "static/x/"}),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/", cookie_secret=secret)
    app.listen(config.server.port)
    base_app.server = app

    return app