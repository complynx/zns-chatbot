from motor.core import AgnosticCollection
from ..tg_state import TGState, SilentArgumentParser
from telegram import ReplyKeyboardMarkup, Update, ReplyKeyboardRemove
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.ext import CommandHandler, filters
from telegram.constants import ParseMode
from ..telegram_links import client_user_link_html
import logging
from typing import Literal
from .passes import PASS_KEY
from json import loads

logger = logging.getLogger(__name__)

CANCEL_CHR = chr(0xE007F) # Tag cancel

RECIPIENT_ALIASES = {
    "registered": {
        PASS_KEY: {"$exists": True}
    },
    "paid": {
        PASS_KEY+".state": "payed"
    },
}

class Superuser(BasePlugin):
    name = "superuser"
    admins: set[int]
    user_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.admins = self.config.telegram.admins
        self.user_db = base_app.users_collection
        self._checker = CommandHandler("user_echo", self.handle_message)
        self._checker_gf = CommandHandler("get_file", self.handle_get_file)
        self._checker_send_message_to = CommandHandler("send_message_to", self.send_message_to)

    def test_message(self, message: Update, state, web_app_data):
        if message.effective_user.id not in self.admins:
            return PRIORITY_NOT_ACCEPTING, None
        if self._checker.check_update(message):
            return PRIORITY_BASIC, self.handle_message
        if self._checker_send_message_to.check_update(message):
            return PRIORITY_BASIC, self.send_message_to
        return PRIORITY_NOT_ACCEPTING, None
    
    async def send_message_to(self, update: TGState):
        try:
            args_list = update.parse_cmd_arguments()
            assert args_list is not None
            parser = SilentArgumentParser()

            group = parser.add_mutually_exclusive_group(required=False)
            group.add_argument('--msg', type=str, help='Text message')
            group.add_argument('--html', type=str, help='Parse text as HTML')
            group.add_argument('--md', type=str, help='Parse text as MD v1')
            group.add_argument('--forward', action='store_true', help='Forward next incoming message')

            parser.add_argument('recipients', nargs='*', help='Recipients')

            args = parser.parse_args(args_list[1:])
            logger.debug(f"send_message_to {args=}, {args_list=}")
            if args.msg:
                return await self.send_message_to__stage2(update, args.recipients, args.msg)
            elif args.html:
                return await self.send_message_to__stage2(update, args.recipients, args.html, ParseMode.HTML)
            elif args.md:
                return await self.send_message_to__stage2(update, args.recipients, args.md, ParseMode.MARKDOWN)
            btns = []
            btns.append([CANCEL_CHR+"Cancel"])
            markup = ReplyKeyboardMarkup(
                btns,
                resize_keyboard=True
            )
            await update.reply("Enter a message you want to send:", reply_markup=markup)
            await update.require_anything(self.name, "send_message_to__input", {
                "recipients": args.recipients,
                "forward": args.forward,
            }, "send_message_to__timeout", 600)
        except Exception as err:
            logger.error(f"Error in send_message_to {err=}", exc_info=1)
            await update.reply(f"Error {err=}", parse_mode=None)
    
    async def send_message_to__input(self, update: TGState, args):
        try:
            if filters.TEXT.check_update(update.update) and update.message.text[0] == CANCEL_CHR:
                return await update.reply("/send_message_to cancelled.",reply_markup=ReplyKeyboardRemove())
            if not args["forward"]:
                return await self.send_message_to__stage2(update, args["recipients"], update.message.text_html, ParseMode.HTML)
            return await self.send_message_to__stage2(update, args["recipients"], "", "forward")
        except Exception as err:
            logger.error(f"Error in send_message_to {err=}", exc_info=1)
            await update.reply(f"Error {err=}", parse_mode=None, reply_markup=ReplyKeyboardRemove())

    async def send_message_to__timeout(self, update: TGState, data):
        await update.reply("/send_message_to timeout.", reply_markup=ReplyKeyboardRemove())
    
    async def get_recipients(self, recipient: str) -> list[int|str|tuple[int|str,int]]:
        try:
            return [int(recipient)]
        except ValueError:
            pass
        if recipient.startswith("@"):
            recipient_parts = recipient.split(":", 2)
            if len(recipient_parts) == 1:
                return recipient_parts
            return [(recipient_parts[0], int(recipient_parts[1]))]
        try:
            recipient_parts = recipient.split(":", 2)
            if len(recipient_parts) == 2:
                return [(int(recipient_parts[0]), int(recipient_parts[1]))]
        except ValueError:
            pass
        if recipient in RECIPIENT_ALIASES:
            recipient = RECIPIENT_ALIASES[recipient]
        else:
            recipient = loads(recipient)
        if isinstance(recipient, list):
            return recipient
        recipient["bot_id"] = self.bot.id
        ret = []
        async for user in self.user_db.find(recipient):
            ret.append(user["user_id"])
        return ret
    
    async def send_message_to__stage2(self, update: TGState, recipients: list[str], text: str, parse_mode: ParseMode|Literal["forward"]|None = None):
        sent_to = []
        for recipient_str in recipients:
            try:
                recipient_list = await self.get_recipients(recipient_str)
                for recipient in recipient_list:
                    try:
                        message_thread_id = None
                        if isinstance(recipient, tuple):
                            message_thread_id = recipient[1]
                            recipient = recipient[0]
                        if parse_mode == "forward":
                            logger.debug(f"forwarding message {update.message=} to {recipient=}, {message_thread_id=}")
                            await self.bot.forward_message(
                                chat_id=recipient,
                                message_thread_id=message_thread_id,
                                message_id=update.message.message_id,
                                from_chat_id=update.update.effective_chat.id,
                            )
                        else:
                            logger.debug(f"sending message to {recipient=}, {message_thread_id=}")
                            await self.bot.send_message(
                                chat_id=recipient,
                                message_thread_id=message_thread_id,
                                text=text,
                                parse_mode=parse_mode,
                            )
                    except Exception as err:
                        logger.error(f"Error in send_message_to__stage2 {err=}, {recipient=}, {message_thread_id=}", exc_info=1)
                        await update.reply(f"Error send_message_to__stage2 {err=}, {recipient=}, {message_thread_id=}", parse_mode=None, reply_markup=ReplyKeyboardRemove())
                    if message_thread_id is not None:
                        sent_to.append(f"{recipient}:{message_thread_id}")
                    else:
                        sent_to.append(recipient)
            except Exception as err:
                logger.error(f"Error in send_message_to__stage2 {err=}, {recipient_str=}", exc_info=1)
                await update.reply(f"Error send_message_to__stage2 {err=}, {recipient_str=}", parse_mode=None, reply_markup=ReplyKeyboardRemove())
        logger.info(f"Message sent to {sent_to=}")
        await update.reply(f"Message sent to {sent_to=}", parse_mode=None, reply_markup=ReplyKeyboardRemove())
    
    async def handle_get_file(self, update: TGState):
        data = update.message.text.split(" ", maxsplit=1)[1]
        abs_path = await self.base_app.avatar.get_file(data)
        await update.message.reply_document(
            abs_path,
            filename=data,
        )

    async def handle_message(self, update: TGState):
        data = update.message.text.split(" ", maxsplit=1)[1]
        uid = int(data)
        user = await self.user_db.find_one({
            "user_id": uid,
            "bot_id": update.bot.id,
        })
        if user is not None:
            await update.reply(client_user_link_html(user), parse_mode=ParseMode.HTML)
        else:
            await update.reply(f"<a href=\"tg://user?id={uid}\">Unknown</a>", parse_mode=ParseMode.HTML)
            