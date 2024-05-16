from motor.core import AgnosticCollection
from ..tg_state import TGState
from telegram import Update, InlineKeyboardMarkup, InlineKeyboardButton, CallbackQuery, Message
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CallbackQueryHandler
from telegram.constants import ParseMode
import logging
from asyncio import Event, Lock

logger = logging.getLogger(__name__)

class Request:
    def __init__(self, msg: Message, user: int, cancelled_text: str) -> None:
        self.message = msg
        self.user = user
        self.cancelled_text = cancelled_text
        self._event = Event()
        self._lock = Lock()
        self._cancelled = False
        self._cancel_sent = False
        self._authorized = False
    def is_authorized(self):
        return self._authorized
    def is_declined(self):
        return not self._authorized
    def is_cancelled(self):
        return self._cancelled
    def is_finished(self):
        return self._event.is_set()
    async def wait(self):
        await self._event.wait()
        if self._cancelled:
            cancel_sent = False
            async with self._lock:
                if not self._cancel_sent:
                    self._cancel_sent = True
                else:
                    cancel_sent = True
            if not cancel_sent:
                await self.message.edit_text(
                    self.cancelled_text,
                    parse_mode=ParseMode.HTML,
                    reply_markup=InlineKeyboardMarkup([])
                )
    async def cancel(self):
        async with self._lock:
            if not self._event.is_set():
                self._cancelled = True
                self._event.set()
    async def authorize(self):
        async with self._lock:
            if not self._event.is_set():
                self._authorized = True
                self._event.set()
    async def decline(self):
        async with self._lock:
            if not self._event.is_set():
                self._event.set()

class Auth(BasePlugin):
    name = "auth"
    user_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        base_app.auth = self
        self.user_db = base_app.users_collection
        self._cbq_handler = CallbackQueryHandler(self.handle_callback_query, pattern=f"^{self.name}|.*")
        self.requests = dict()
        self._lock = Lock()

    def test_callback_query(self, message: Update, state):
        if self._cbq_handler.check_update(message):
            return PRIORITY_BASIC, self.handle_callback_query
        return PRIORITY_NOT_ACCEPTING, None

    async def add_request(self, msg: Message, update: TGState):
        async with self._lock:
            if update.user in self.requests:
                await self.requests[update.user].cancel()
            req = Request(msg, update.user, update.l("auth-cancelled"))
            self.requests[update.user] = req
            return req

    async def request_auth(self, update: TGState, request_info):
        reqMessage = await update.reply(
            update.l("auth-request", **request_info),
            parse_mode=ParseMode.HTML, 
            reply_markup=InlineKeyboardMarkup([[
                InlineKeyboardButton(update.l("auth-authorize"), callback_data="auth|authorize"),
                InlineKeyboardButton(update.l("auth-decline"), callback_data="auth|decline"),
            ]]),
        )
        return await self.add_request(reqMessage, update)
    
    async def handle_cq_authorize(self, update: TGState, q: CallbackQuery):
        await q.edit_message_text(update.l("auth-authorized"), reply_markup=InlineKeyboardMarkup([]))
        async with self._lock:
            if update.user in self.requests:
                await self.requests[update.user].authorize()
                del self.requests[update.user]

    async def handle_cq_decline(self, update: TGState, q: CallbackQuery):
        await q.edit_message_text(update.l("auth-declined"), reply_markup=InlineKeyboardMarkup([]))
        async with self._lock:
            if update.user in self.requests:
                await self.requests[update.user].decline()
                del self.requests[update.user]

    async def handle_callback_query(self, update: TGState):
        q = update.update.callback_query
        await q.answer()
        logger.info(f"Received callback_query from {update.user}, data: {q.data}")
        data = q.data.split("|")
        fn = "handle_cq_" + data[1]
        if hasattr(self, fn):
            attr = getattr(self, fn, None)
            if callable(attr):
                return await attr(update, q, *data[2:])
        logger.error(f"unknown callback {data[1]}: {data[2:]}")
            