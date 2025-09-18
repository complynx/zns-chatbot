import hashlib
import hmac
import json
import logging
import os
import tempfile
from operator import itemgetter
from typing import Any, ClassVar, Optional
from urllib.parse import parse_qsl, urlparse

import tornado.platform.asyncio
import tornado.web
from bson import ObjectId
from telegram import Bot
from tornado.httputil import HTTPServerRequest

from .config import Config
from .plugins.pass_keys import PASS_KEY  # For food.save_order
from .plugins.massage import now_msk
from .plugins.orders import DEADLINE

logger = logging.getLogger(__name__)


class AuthError(Exception):
    status: ClassVar[int] = 403
    detail: str = "unknown auth error"

    @property
    def message(self) -> str:
        return f"Auth error occurred, detail: {self.detail}"


def validate(init_data: str, bot_token: str) -> dict[str, Any]:
    secret_key = hmac.new(
        key=b"WebAppData", msg=bot_token.encode(), digestmod=hashlib.sha256
    ).digest()
    try:
        parsed_data = dict(parse_qsl(init_data, strict_parsing=True))
    except ValueError as err:
        logger.error("invalid init data: %s", err, exc_info=1)
        raise AuthError(detail="invalid init data") from err
    if "hash" not in parsed_data:
        logger.error(f"missing hash: {parsed_data}")
        raise AuthError(detail="missing hash")
    hash_ = parsed_data.pop("hash")
    data_check_string = "\n".join(
        f"{k}={v}" for k, v in sorted(parsed_data.items(), key=itemgetter(0))
    )
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
        return self.bot.name[1:] + "_user"

    def get_user_id(self, max_age_days: float | None = None):
        if max_age_days is None:
            max_age_days = self.config.server.auth_timeout
        user_id = self.get_signed_cookie(
            self.user_cookie,
            max_age_days=max_age_days,
        )
        if user_id is None:
            return None
        return int(user_id.decode("utf-8"))


class FitFrameHandler(RequestHandlerWithApp):
    async def get(self):
        file = self.get_query_argument("file", default="")
        locale_str = self.get_query_argument("locale", default="en")

        def localize(s):
            return self.app.localization(s, locale=locale_str)

        try:
            self.render(
                "fit_frame.html",
                file=file,
                real_frame_size=self.config.photo.frame_size,
                quality=self.config.photo.quality,
                debug_code="",
                help_desktop=localize("frame-mover-help-desktop"),
                help_mobile=localize("frame-mover-help-mobile"),
                help_realign=localize("frame-realign-message"),
                frame_mover_help_unified=localize("frame-mover-help-unified"),
                finish_button_text=localize("frame-mover-finish-button-text"),
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)

    async def put(self):
        initData = self.get_argument("initData", default=None, strip=False)
        compensations = self.get_argument("compensations", default=None, strip=False)

        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            user = json.loads(initData["user"])
            with tempfile.NamedTemporaryFile(suffix=".jpg", delete=False) as temp_file:
                temp_name = temp_file.name
                temp_file.write(self.request.body)
            await self.bot.send_document(
                user["id"],
                temp_name,
                filename="avatar.jpg",
            )
            os.remove(temp_name)

            self.set_status(200)
            self.write({"message": "Image uploaded successfully"})

            if compensations:
                user_agent = self.request.headers.get("User-Agent", "Unknown")
                import hashlib

                from .tg_state import TGState

                ua_sha = hashlib.sha256(user_agent.encode()).hexdigest()
                compensations = json.loads(compensations)
                compensations["user_agent"] = user_agent
                state = TGState(user["id"], self.app)
                await state.update_user(
                    {"$set": {"avatar_compensations_map." + ua_sha: compensations}},
                    upsert=True,
                )

        except Exception as e:
            self.set_status(500)
            self.write({"error": "internal error"})
            logger.error("error saving image: %s", e, exc_info=1)


class OrdersHandler(RequestHandlerWithApp):
    async def get(self):
        from .plugins.orders import Orders
        orders: Orders = self.app.orders
        order_id = self.get_query_argument("order_id", default="")
        debug_id = self.get_query_argument("debug_id", default="")
        locale_str = self.get_query_argument("locale", default="en")

        def localize(s):
            return self.app.localization(s, locale=locale_str)

        choice = None
        read_only = False
        if order_id != "":
            order = await orders.order_by_id(order_id)
            if "choice" in order:
                choice = order["choice"]
            read_only="proof_file" in order
        # After deadline, force read-only regardless of order state
        if now_msk() > DEADLINE:
            read_only = True
        lang = "en"
        if locale_str.startswith("ru"):
            lang = "ru"

        try:
            self.render(
                "orders.html",
                read_only=read_only,
                user_order=choice,
                user_order_id=order_id,
                debug_id=debug_id,
                lang=lang,
                finish_button_text=localize("orders-finish-button-text"),
                next_button_text=localize("orders-next-button-text"),
                placeholder_first_name=localize("orders-placeholder-first-name"),
                placeholder_last_name=localize("orders-placeholder-last-name"),
                placeholder_patronymus=localize("orders-placeholder-patronymus"),
                validity_error_first_name=localize("orders-validity-error-first-name"),
                validity_error_last_name=localize("orders-validity-error-last-name"),
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)

    async def post(self):
        from .plugins.orders import Orders
        orders: Orders = self.app.orders
        initData = self.get_argument("initData", default=None, strip=False)

        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            user = json.loads(initData["user"])
            order_id = self.get_query_argument("order_id", default="")
            choice = json.loads(self.request.body)
            logger.info(f"user {user['id']} order {order_id} choice {choice}")

            # Block all modifications and creations after deadline
            if now_msk() > DEADLINE:
                self.set_status(403)
                self.write({"error": "orders closed by deadline"})
                return

            if order_id == "":
                logger.info(f"creating order for {user['id']}")
                await orders.create_order(user['id'], choice)
                return
            order = await orders.order_by_id(order_id)
            if order is None:
                self.set_status(404)
                logger.info("order not found")
                return
            if order["user_id"] != user["id"]:
                self.set_status(403)
                logger.error("user ID doesn't match")
                return
            logger.info(f"updating order {order_id} for {user['id']}")
            await orders.set_choice(order, choice)

            self.set_status(200)
            self.write({"message": "order saved"})

        except Exception as e:
            self.set_status(500)
            self.write({"error": "internal error"})
            logger.error("error saving image: %s", e, exc_info=1)


class GetCompensationsHandler(RequestHandlerWithApp):
    async def get(self):
        initData = self.get_argument("initData", default=None, strip=False)
        if initData:
            initData = validate(initData, self.config.telegram.token.get_secret_value())
        else:
            # initData not found in the request, reject the request
            self.set_status(400)
            logger.info("initData parameter is missing")
            return
        try:
            user_agent = self.request.headers.get("User-Agent", "Unknown")
            import hashlib

            from .tg_state import TGState

            ua_sha = hashlib.sha256(user_agent.encode()).hexdigest()
            user = json.loads(initData["user"])
            state = TGState(user["id"], self.app)
            user = await state.get_user()
            if (
                "avatar_compensations_map" in user
                and ua_sha in user["avatar_compensations_map"]
            ):
                self.set_status(200)
                self.write(user["avatar_compensations_map"][ua_sha])
            else:
                self.set_status(200)
                self.write({})

        except Exception as e:
            self.set_status(500)
            self.write({"error": "internal error"})
            logger.error("error saving image: %s", e, exc_info=1)


class MenuHandler(RequestHandlerWithApp):
    async def get(self):
        debug_id = self.get_query_argument("debug_id", default="")
        locale_str = self.get_query_argument("lang", default="en")

        original_message_id_for_template = None
        chat_id_for_template = None
        try:
            original_message_id_str = self.get_query_argument(
                "orig_msg_id", default=None
            )
            chat_id_str = self.get_query_argument("orig_chat_id", default=None)
            if original_message_id_str:
                original_message_id_for_template = int(original_message_id_str)
            if chat_id_str:
                chat_id_for_template = int(chat_id_str)
        except ValueError:
            logger.info(
                "Menu GET: orig_msg_id or orig_chat_id had invalid format in GET request query."
            )
        # No MissingArgumentError check needed due to default=None in get_query_argument

        def localize(s):
            return self.app.localization(s, locale=locale_str)

        lang = "en"
        if locale_str.startswith("ru"):
            lang = "ru"

        current_order_details = None
        user_order_id_for_template = ""
        read_only = True

        order_to_load = None
        # pass_key should be present in the URL if order_id is, as constructed by food.py
        pass_key_from_query = self.get_query_argument("pass_key", default=PASS_KEY)
        order_id_str_from_query = self.get_query_argument("order_id", default=None)

        if order_id_str_from_query:
            try:
                order_oid_query = ObjectId(order_id_str_from_query)
                # Assuming self.app.food.get_order_by_id(order_id_obj, pass_key) exists and returns order or None
                order_to_load = await self.app.food.get_order_by_id(
                    order_oid_query, pass_key_from_query
                )
            except (
                Exception
            ) as e:  # Includes bson.errors.InvalidId from ObjectId conversion
                logger.warning(
                    f"Menu GET: Invalid order_id format '{order_id_str_from_query}' or error fetching by order_id: {e}. Fallback to general get_order."
                )

        if order_to_load:
            current_order_details = order_to_load.get("order_details")
            user_order_id_for_template = str(order_to_load.get("_id"))
            read_only = order_to_load.get("payment_status") in [
                "paid",
                "proof_submitted",
            ]
            logger.info(
                f"Menu GET: Loaded order {user_order_id_for_template}. Read_only: {read_only}, Details: {current_order_details is not None}"
            )
        else:
            # User is valid (from initData), but no existing order found for them (neither specific nor general).
            read_only = False  # Allow creation of a new order.
            logger.info(
                f"Menu GET: No order found. Read_only: {read_only} (new order mode). Details: None."
            )
        # else: user_id_from_init_data is None, read_only remains True (default)

        from random import (
            randbytes,
        )  # Ensure randbytes is imported if not at top of file

        random_hex = randbytes(16).hex()
        try:
            self.render(
                "menu.html",
                read_only=read_only,
                user_order=current_order_details,
                user_order_id=user_order_id_for_template,
                debug_id=debug_id,
                random=random_hex,
                lang=lang,
                finish_button_text=localize("orders-finish-button-text"),
                alert_lunch_incomplete_text=localize("menu-alert-lunch-incomplete"),
                confirm_dinner_empty_text=localize("menu-confirm-dinner-empty"),
                orig_msg_id=original_message_id_for_template,
                orig_chat_id=chat_id_for_template,
            )
        except (KeyError, ValueError) as e:
            logger.error(f"Error rendering menu.html: {e}", exc_info=True)
            raise tornado.web.HTTPError(404)

    async def post(self):
        initData_str = self.get_argument("initData", default=None, strip=False)
        user_id_from_init_data = None

        if not initData_str:
            self.set_status(400)
            logger.info("Menu POST: initData parameter is missing")
            self.write({"error": "initData parameter is missing"})
            return

        try:
            initData_validated = validate(
                initData_str, self.config.telegram.token.get_secret_value()
            )
            user_json_str = initData_validated.get("user")
            if user_json_str:
                user_data = json.loads(user_json_str)
                user_id_from_init_data = user_data.get("id")
            if not user_id_from_init_data:
                self.set_status(
                    401
                )  # Or 400 if 'user' field is simply missing but initData is otherwise "valid"
                logger.info(
                    "Menu POST: 'user' field or 'id' missing in validated initData."
                )
                self.write(
                    {
                        "error": "Authentication failed: User could not be identified from initData."
                    }
                )
                return
        except AuthError:
            self.set_status(401)
            logger.info("Menu POST: initData validation failed.")
            self.write({"error": "Authentication failed: Invalid initData."})
            return
        except json.JSONDecodeError:
            self.set_status(400)  # Error parsing user JSON from initData
            logger.info("Menu POST: Failed to parse user JSON from initData.")
            self.write({"error": "Invalid initData format."})
            return
        except Exception as e:
            self.set_status(500)
            logger.error(f"Menu POST: Error processing initData: {e}", exc_info=True)
            self.write({"error": "Internal server error during initData processing."})
            return

        # Extract orig_msg_id and orig_chat_id from query params of the POST request
        try:
            original_message_id_str = self.get_query_argument(
                "orig_msg_id", default=None
            )
            chat_id_str = self.get_query_argument("orig_chat_id", default=None)

            original_message_id = (
                int(original_message_id_str) if original_message_id_str else None
            )
            chat_id = int(chat_id_str) if chat_id_str else None

            if not original_message_id or not chat_id:
                logger.info(
                    "Menu POST: orig_msg_id or orig_chat_id missing or not provided in POST request query."
                )
                # These are optional for save_order, will proceed with None if not found

        except ValueError:
            original_message_id = None
            chat_id = None
            logger.info(
                "Menu POST: orig_msg_id or orig_chat_id had invalid format in POST request query."
            )
        except (
            tornado.web.MissingArgumentError
        ):  # Should be caught by default=None, but as a safeguard
            original_message_id = None
            chat_id = None
            logger.info(
                "Menu POST: orig_msg_id or orig_chat_id arguments completely missing."
            )

        try:
            carts_data = json.loads(self.request.body)
            # The client might send order_id in query, but save_order uses user_id + pass_key
            # autosave_str = self.get_query_argument("autosave", default="") # Not used by food.save_order

            logger.info(
                f"Menu POST: Attempting to save order for user {user_id_from_init_data}. Origin msg: {original_message_id}, chat: {chat_id}"
            )

            # food.save_order handles ownership via user_id and editability of paid/proof_submitted orders.
            await self.app.food.save_order(
                user_id=user_id_from_init_data,
                pass_key=PASS_KEY,  # Imported from .plugins.passes
                order_data=carts_data,
                original_message_id=original_message_id,
                chat_id=chat_id,
            )

            self.set_status(200)
            self.write({"message": "Order saved successfully"})

        except (
            ValueError
        ) as e:  # Raised by food.save_order for trying to edit a locked order
            self.set_status(403)  # Forbidden
            logger.warn(
                f"Menu POST: Forbidden to save order for user {user_id_from_init_data}. Reason: {e}"
            )
            self.write(
                {"error": str(e)}
            )  # Pass the specific error message from save_order
        except json.JSONDecodeError:
            self.set_status(400)
            self.write({"error": "Invalid JSON in request body"})
            logger.warn(
                "Menu POST: Failed to decode JSON from request body", exc_info=True
            )
        except Exception as e:
            self.set_status(500)
            self.write({"error": "Internal server error while processing menu order"})
            logger.error("Menu POST: Error processing menu order: %s", e, exc_info=True)


class FoodGetOrders(RequestHandlerWithApp):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", self.request.headers["Origin"])
        self.set_header("Access-Control-Allow-Credentials", "true")

    async def get(self):
        try:
            user_id = self.get_user_id()
            if user_id is None or user_id not in self.config.food.admins:
                self.set_status(401)
                self.write({"result": "unauthorized"})
                return
            orders = await self.app.food.get_all_orders()
            self.set_status(200)
            self.write({"orders": orders, "menu": self.app.food.menu})

        except Exception as e:
            self.set_status(500)
            self.write({"error": "internal error"})
            logger.error("error getting orders: %s", e, exc_info=1)

    async def post(self):
        try:
            user_id = self.get_user_id()
            if user_id is None or user_id not in self.config.food.admins:
                self.set_status(401)
                self.write({"result": "unauthorized"})
                return
            out_of_stock = json.loads(self.request.body)
            await self.app.food.out_of_stock(out_of_stock)
            self.set_status(200)

        except Exception as e:
            self.set_status(500)
            self.write({"error": "internal error"})
            logger.error("error getting orders: %s", e, exc_info=1)


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
        initData = self.get_argument("initData", default=None, strip=False)

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
            user = json.loads(initData["user"])
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
            self.write({"error": "internal error"})
            logger.error("error saving menu: %s", e, exc_info=1)


def domain_from_uri(origin):
    parsed_origin = urlparse(origin)
    domain: str = parsed_origin.netloc
    if ":" in domain:
        domain = domain.split(":", 2)[0]
    if domain.startswith("."):
        domain = domain[1:]
    return domain


class AuthHandler(RequestHandlerWithApp):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", self.request.headers["Origin"])
        self.set_header("Access-Control-Allow-Credentials", "true")

    async def get(self):
        try:
            check = self.get_argument("check", default=None, strip=True)
            if check is not None:
                user_id = self.get_user_id(self.config.server.auth_timeout * 0.7)
                if user_id is not None:
                    self.set_status(200)
                    self.write({"result": "authorized"})
                    return
                else:
                    self.set_status(401)
                    self.write({"result": "unauthorized"})
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
            username = self.get_argument("username", default="", strip=True)
            if username == "":
                self.set_status(400)
                logger.warn("auth without username")
                return
            user = await self.app.users_collection.find_one(
                {
                    "username": username,
                    "bot_id": self.bot.id,
                }
            )
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
                logger.warn(
                    f"auth request for user {user['user_id']} with {username} cancelled"
                )
                self.set_status(401)
                self.write({"result": "cancelled"})
                return
            if req.is_authorized():
                self.set_signed_cookie(
                    self.user_cookie,
                    str(user["user_id"]),
                    expires_days=self.config.server.auth_timeout,
                )
                logger.info(
                    f"auth request for user {user['user_id']} with {username} authorized"
                )
                self.set_status(200)
                self.write({"result": "authorized"})
                return
            else:
                logger.info(
                    f"auth request for user {user['user_id']} with {username} declined"
                )
                self.set_status(401)
                self.write({"result": "unauthorized"})
                return

        except Exception as e:
            self.set_status(500)
            self.write({"error": "internal error"})
            logger.error("error getting orders: %s", e, exc_info=1)


class BotNameHandler(RequestHandlerWithApp):
    def set_default_headers(self):
        super().set_default_headers()
        self.set_header("Access-Control-Allow-Origin", "*")

    async def get(self):
        self.set_status(200)
        self.write({"name": self.bot.name[1:]})


class ErrorHandler(RequestHandlerWithApp):
    async def post(self):
        try:
            initData = self.get_argument("initData", default=None, strip=False)
            if initData:
                initData = validate(
                    initData, self.config.telegram.token.get_secret_value()
                )
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
        def __init__(
            self,
            application: tornado.web.Application,
            request: HTTPServerRequest,
            **kwargs: Any,
        ) -> None:
            super().__init__(application, request, path="", **kwargs)

        def get_absolute_path(self, root: str, path: str) -> str:
            return self.abs_path

        def validate_absolute_path(
            self, root: str, absolute_path: str
        ) -> Optional[str]:
            logger.debug(f"abs_path {absolute_path}")
            if absolute_path == "" or not os.path.isfile(absolute_path):
                raise tornado.web.HTTPError(404)
            return absolute_path

        async def get(self, path: str, include_body: bool = True) -> None:
            path_part = self.parse_url_path(path)
            if path_part == "frame":
                self.abs_path = os.path.abspath(config.photo.frame_file)
            # elif path_part == "flare":
            #     self.abs_path = os.path.abspath(config.photo.flare_file)
            else:
                self.abs_path = await base_app.avatar.get_file(path_part)
            return await super().get(path, include_body)

    secret = hashlib.sha256(
        f"secret {config.telegram.token.get_secret_value()} {config.mongo_db.address}".encode()
    ).hexdigest()

    app = tornado.web.Application(
        [
            (r"/fit_frame", FitFrameHandler, {"app": base_app}),
            (r"/get_compensations", GetCompensationsHandler, {"app": base_app}),
            (r"/massage_timetable", MassageTimetablePageHandler, {"app": base_app}),
            (r"/massage_timetable_data", MassageTimetableHandler, {"app": base_app}),
            (r"/bot_name", BotNameHandler, {"app": base_app}),
            (r"/auth", AuthHandler, {"app": base_app}),
            (r"/orders", OrdersHandler, {"app": base_app}),
            # (r"/food_get_orders", FoodGetOrders, {"app": base_app}),
            (r"/menu", MenuHandler, {"app": base_app}),
            (r"/error", ErrorHandler, {"app": base_app}),
            (r"/photos/(.*)", PhotoHandler),
            (r"/static/x/(.*)", CustomStaticFileHandler, {"path": "static/x/"}),
            (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
        ],
        template_path="templates/",
        cookie_secret=secret,
    )
    app.listen(config.server.port)
    base_app.server = app

    return app
