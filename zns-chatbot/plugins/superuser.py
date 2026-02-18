from asyncio import gather
from datetime import datetime, timedelta
from html import escape
import logging
from langchain_google_genai import ChatGoogleGenerativeAI
from langchain.schema import HumanMessage, SystemMessage
from motor.core import AgnosticCollection
from tornado.template import Template
from telegram import ReplyKeyboardMarkup, Update, ReplyKeyboardRemove
from telegram.constants import ParseMode
from telegram.ext import CommandHandler, filters
from json import loads
from typing import Literal

from ..telegram_links import client_user_link_html, client_user_link_url, client_user_name
from ..tg_state import TGState, SilentArgumentParser
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING

logger = logging.getLogger(__name__)

CANCEL_CHR = chr(0xE007F) # Tag cancel

RECIPIENT_ALIASES = {
}

class Superuser(BasePlugin):
    name = "superuser"
    admins: set[int]
    user_db: AgnosticCollection

    def __init__(self, base_app):
        super().__init__(base_app)
        self.admins = self.config.telegram.admins
        self.user_db = base_app.users_collection
        self.pass_db = base_app.passes_collection
        self.message_db = (
            base_app.mongodb[self.config.mongo_db.messages_collection]
            if base_app.mongodb is not None
            else None
        )
        self.files_db = (
            base_app.mongodb[self.config.mongo_db.files_collection]
            if base_app.mongodb is not None
            else None
        )
        self._checker = CommandHandler("user_echo", self.handle_message)
        self._checker_gf = CommandHandler("get_file", self.handle_get_file)
        self._checker_send_message_to = CommandHandler("send_message_to", self.send_message_to)
        self._checker_refresh_events = CommandHandler("refresh_events", self.refresh_events)

    def test_message(self, message: Update, state, web_app_data):
        if message.effective_user.id not in self.admins:
            return PRIORITY_NOT_ACCEPTING, None
        if self._checker.check_update(message):
            return PRIORITY_BASIC, self.handle_message
        if self._checker_gf.check_update(message):
            return PRIORITY_BASIC, self.handle_get_file
        if self._checker_send_message_to.check_update(message):
            return PRIORITY_BASIC, self.send_message_to
        if self._checker_refresh_events.check_update(message):
            return PRIORITY_BASIC, self.refresh_events
        return PRIORITY_NOT_ACCEPTING, None

    async def refresh_events(self, update: TGState):
        try:
            events = self.base_app.events
            if events is None:
                await update.reply("Events are not initialized.", parse_mode=None)
                return
            await events.refresh()
            if hasattr(self.base_app, "passes") and self.base_app.passes is not None:
                self.base_app.passes.refresh_events_cache()
            all_keys = events.all_pass_keys()
            active_keys = events.active_pass_keys()
            await update.reply(
                "Events refreshed.\n"
                + f"All pass keys: {all_keys}\n"
                + f"Active pass keys: {active_keys}",
                parse_mode=None,
            )
        except Exception as err:
            logger.error("Error in refresh_events %s", err, exc_info=1)
            await update.reply(f"Error in refresh_events {err=}", parse_mode=None)
    
    async def send_message_to(self, update: TGState):
        try:
            args_list = update.parse_cmd_arguments()
            assert args_list is not None
            parser = SilentArgumentParser()

            parser.add_argument('--template', action='store_true', help='The message is a template')

            group = parser.add_mutually_exclusive_group(required=False)
            group.add_argument('--msg', type=str, help='Text message')
            group.add_argument('--html', type=str, help='Parse text as HTML')
            group.add_argument('--md', type=str, help='Parse text as MD v1')
            group.add_argument('--forward', action='store_true', help='Forward next incoming message')

            parser.add_argument('recipients', nargs='*', help='Recipients')

            args = parser.parse_args(args_list[1:])
            logger.debug(f"send_message_to {args=}, {args_list=}")
            is_template = args.template
            if args.msg:
                return await self.send_message_to__stage2(update, args.recipients, args.msg, is_template=is_template)
            elif args.html:
                return await self.send_message_to__stage2(update, args.recipients, args.html, ParseMode.HTML, is_template=is_template)
            elif args.md:
                return await self.send_message_to__stage2(update, args.recipients, args.md, ParseMode.MARKDOWN, is_template=is_template)
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
                "is_template": is_template,
            }, "send_message_to__timeout", 600)
        except Exception as err:
            logger.error(f"Error in send_message_to {err=}", exc_info=1)
            await update.reply(f"Error {err=}", parse_mode=None)
    
    async def send_message_to__input(self, update: TGState, args):
        try:
            if filters.TEXT.check_update(update.update) and update.message.text[0] == CANCEL_CHR:
                return await update.reply("/send_message_to cancelled.",reply_markup=ReplyKeyboardRemove())
            if not args["forward"]:
                return await self.send_message_to__stage2(
                    update, args["recipients"],
                    update.message.text_html,
                    ParseMode.HTML,
                    is_template=args["is_template"]
                )
            return await self.send_message_to__stage2(
                update,
                args["recipients"],
                parse_mode="forward",
                is_template=args["is_template"],
            )
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
    
    async def user_informal_name(self, user: dict) -> str:
        if "informal_name" in user:
            return user["informal_name"]
        chat: ChatGoogleGenerativeAI = self.base_app.assistant.chat
        names_found = {}
        for key in user.keys():
            if "name" in key and key != "bot_username":
                names_found[key] = user[key]
       
        messages = [
            SystemMessage(
                content="""
Ты — помощник, который по переданным данным пользователя определяет дружелюбное неформальное обращение по имени и кратко объясняет логику.

Правила извлечения:
1) Источники по приоритету: legal_name → known_names → first_name и другие.
2) Удали эмодзи, числа и служебные символы; не используй явные псевдонимы без человеческого имени.
3) Имя — 1–2 слова максимум, без фамилии, с корректным регистром, на русском языке.
4) Если очевидна уменьшительная форма (Иван→Ваня, Александр→Саша, Екатерина→Катя и т.п.), используй её; иначе оставь базовую.
5) Если нет надёжного имени, итог: «Имя не указано».

Формат ответа (строго):
Анализ: <кратко, почему поле(я) подходят/не подходят>
<пустая строка>
<имя>   ИЛИ   Имя не указано

Примеры:
```
-> {"name":"Иван","username":"ivan30"}
<- Нашёл «Иван» в name; username содержит то же имя, потому надёжно. Уменьшительная форма «Ваня» естественна.
 
Ваня
```

```
-> {"username":"obviouslyjustanickname","first_name":"🙂"}
<- first_name — эмодзи, не имя; username — псевдоним без человеческого имени.
 
Имя не указано
```

```
-> {"display_name":"Катюша","username":"kat_89"}
<- display_name даёт естественную форму «Катюша»; это дружелюбное обращение, предпочтительное самим пользователем.
 
Катюша
```

```
-> {"name":"Александр П.","nickname":"Sasha"}
<- nickname содержит дружелюбную форму «Sasha»; name — с фамилией-инициалом, что менее уместно.

Саша
```

```
-> {"first_name":"Remy","username":"djremy_zouk","print_name":"Remy","inner_name_en":"Vitaliy Portnikov","inner_name_ru":"Виталий Портников","legal_name":"Remy"}
<- В приоритетных полях «Remy» выглядит как сценический ник; во вспомогательных полях стабильно фигурирует «Виталий Портников», что указывает на реальное имя. Беру человеческое имя.

Виталий
```

```
-> {"first_name":"LABIRINT","username":"Dance_PanTerra_Club","known_names":["Успенский Сергей Евгеньевич"],"print_name":"LABIRINT"}
<- Анализ: first_name — бренд/ник, не имя; known_names дают «Сергей». Для дружелюбного обращения естественна уменьшительная форма «Серёжа».

Серёжа
```

```
-> {"first_name":"Darya","last_name":"Volotovskaya","username":"Darya_zouk","legal_name":"Дарья"}
<- Анализ: first_name на латинице «Darya» валиден; legal_name «Дарья» указывает на исходную кириллическую форму. Для дружелюбного обращения естественна уменьшительная «Даша», поэтому перехожу на кириллицу.

Даша
```

"""
            ),
            HumanMessage(
                content=f"Данные пользователя: {names_found}. "
            )
        ]
        response = await chat.ainvoke(messages)
        logger.info("result: %s", response)
        if response.content is None or response.content.strip() == "":
            return ""
        parts = response.content.split("\n")
        if len(parts) < 2:
            return ""
        informal_name = parts[-1].strip()
        def levenstein_distance(a: str, b: str) -> int:
            """Compute the Levenshtein distance between two strings."""
            if a == b:
                return 0
            if len(a) == 0:
                return len(b)
            if len(b) == 0:
                return len(a)
            previous_row = list(range(len(b) + 1))
            for i, ca in enumerate(a, 1):
                current_row = [i]
                for j, cb in enumerate(b, 1):
                    insertions = previous_row[j] + 1
                    deletions = current_row[j - 1] + 1
                    substitutions = previous_row[j - 1] + (ca != cb)
                    current_row.append(min(insertions, deletions, substitutions))
                previous_row = current_row
            return previous_row[-1]

        if levenstein_distance(informal_name, "Имя не указано") < 3:
            return ""
        
        informal_name = informal_name.strip()
        await self.user_db.update_one(
            {"user_id": user["user_id"], "bot_id": self.bot.id},
            {"$set": {"informal_name": informal_name}}
        )
        return informal_name
        
    async def send_message_to__stage2(
            self,
            update: TGState,
            recipients: list[str],
            text: str|None = None,
            parse_mode: ParseMode|Literal["forward"]|None = None,
            is_template: bool = False
        ):
        if parse_mode != "forward" and text is None:
            await update.reply("No message to send.", parse_mode=None, reply_markup=ReplyKeyboardRemove())
            return
        if is_template:
            template = Template(text)
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
                            real_text = text
                            if is_template:
                                if message_thread_id is not None:
                                    await update.reply(
                                        "Template can not be sent in a thread.",
                                        parse_mode=ParseMode.HTML,
                                        reply_markup=ReplyKeyboardRemove(),
                                    )
                                    continue
                                user = await self.user_db.find_one({
                                    "user_id": recipient,
                                    "bot_id": self.bot.id,
                                })
                                if user is None:
                                    await update.reply(
                                        f"User {recipient} not found in db for template filling.",
                                        parse_mode=ParseMode.HTML,
                                        reply_markup=ReplyKeyboardRemove(),
                                    )
                                    continue
                                lc = user.get("language_code", None)
                                real_text = template.generate(
                                    **user,
                                    user_link=client_user_link_html(user, language_code=lc),
                                    user_name=client_user_name(user, language_code=lc),
                                    user_informal_name=await self.user_informal_name(user),
                                ).decode("utf-8")

                            await self.bot.send_message(
                                chat_id=recipient,
                                message_thread_id=message_thread_id,
                                text=real_text,
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

    @staticmethod
    def _format_datetime(raw) -> str:
        if not isinstance(raw, datetime):
            return ""
        return raw.isoformat(sep=" ", timespec="seconds")

    @staticmethod
    def _format_name_value(raw) -> str:
        if raw is None:
            return ""
        if isinstance(raw, str):
            return raw.strip()
        if isinstance(raw, list):
            parts = [Superuser._format_name_value(v) for v in raw]
            parts = [p for p in parts if p]
            return ", ".join(parts)
        return str(raw)

    @staticmethod
    def _safe_int(raw, default: int = 0) -> int:
        if raw is None:
            return default
        if isinstance(raw, (int, float)):
            return int(raw)
        if isinstance(raw, str):
            value = raw.strip()
            if value == "":
                return default
            try:
                return int(value)
            except ValueError:
                return default
        return default

    def _pass_label(self, pass_key: str, locale: str | None = None) -> str:
        events = getattr(self.base_app, "events", None)
        if events is None:
            return pass_key
        event = events.get_event(pass_key)
        if event is None:
            return pass_key
        title = event.title_short_for_locale(locale)
        emoji = event.country_emoji.strip()
        if title and title != pass_key:
            label = f"{title} [{pass_key}]"
        else:
            label = pass_key
        if emoji:
            return f"{label} {emoji}"
        return label

    async def _collect_pass_stats_lines(
        self,
        uid: int,
        bot_id: int,
        locale: str | None = None,
    ) -> list[str]:
        if self.pass_db is None:
            return ["passes: unavailable"]
        pass_docs = await self.pass_db.find(
            {
                "user_id": uid,
                "bot_id": bot_id,
            }
        ).sort([("pass_key", 1), ("date_created", 1)]).to_list(None)
        if len(pass_docs) == 0:
            return ["passes: 0"]

        state_counts: dict[str, int] = {}
        registered_where: set[str] = set()
        paid_where: set[str] = set()
        details: list[str] = []
        for pass_doc in pass_docs:
            pass_key = str(pass_doc.get("pass_key", "unknown"))
            state = str(pass_doc.get("state", "unknown"))
            state_counts[state] = state_counts.get(state, 0) + 1
            label = self._pass_label(pass_key, locale=locale)
            registered_where.add(label)
            if state in {"paid", "payed"}:
                paid_where.add(label)

            parts = [f"state={state}"]
            for field in ["role", "type", "price"]:
                if field in pass_doc and pass_doc[field] not in [None, ""]:
                    parts.append(f"{field}={pass_doc[field]}")
            for field in [
                "date_created",
                "date_assignment",
                "proof_received",
                "proof_accepted",
            ]:
                formatted_date = self._format_datetime(pass_doc.get(field))
                if formatted_date:
                    parts.append(f"{field}={formatted_date}")
            if "proof_admin" in pass_doc and pass_doc["proof_admin"] not in [None, ""]:
                parts.append(f"proof_admin={pass_doc['proof_admin']}")
            details.append(f"{label}: " + ", ".join(parts))

        state_summary = ", ".join(
            f"{state}:{count}" for state, count in sorted(state_counts.items())
        )
        registered_summary = ", ".join(sorted(registered_where)) if registered_where else "none"
        paid_summary = ", ".join(sorted(paid_where)) if paid_where else "none"
        lines = [
            f"passes: {len(pass_docs)} ({state_summary})",
            f"passes registered in: {registered_summary}",
            f"passes paid in: {paid_summary}",
        ]
        for detail in details:
            lines.append(f"  - {detail}")
        return lines

    async def _collect_message_stats_lines(self, uid: int) -> list[str]:
        if self.message_db is None:
            return ["assistant requests: unavailable"]
        one_day_ago = datetime.now() - timedelta(days=1)
        requests_total, requests_last_day, replies_total, latest_request = await gather(
            self.message_db.count_documents({"user_id": uid, "role": "user"}),
            self.message_db.count_documents(
                {
                    "user_id": uid,
                    "role": "user",
                    "date": {"$gte": one_day_ago},
                }
            ),
            self.message_db.count_documents({"user_id": uid, "role": "assistant"}),
            self.message_db.find_one(
                {"user_id": uid, "role": "user"},
                sort=[("date", -1)],
            ),
        )
        latest_request_date = self._format_datetime(
            latest_request.get("date") if latest_request is not None else None
        )
        lines = [
            f"assistant requests: {requests_total} total, {requests_last_day} in last 24h",
            f"assistant replies: {replies_total}",
        ]
        if latest_request_date:
            lines.append(f"last assistant request: {latest_request_date}")
        return lines

    async def _collect_avatar_stats_lines(self, uid: int, bot_id: int, user: dict | None) -> list[str]:
        avatars_called = self._safe_int((user or {}).get("avatars_called"))
        avatars_simple = self._safe_int((user or {}).get("avatars_simple"))
        avatars_detailed = self._safe_int((user or {}).get("avatars_detailed"))
        lines = [
            f"avatars counters: called={avatars_called}, simple={avatars_simple}, detailed={avatars_detailed}",
        ]
        if self.files_db is None:
            return lines
        uploads_total, done_simple, done_detailed = await gather(
            self.files_db.count_documents({"user_id": uid, "bot_id": bot_id}),
            self.files_db.count_documents(
                {"user_id": uid, "bot_id": bot_id, "status": "completed_simple"}
            ),
            self.files_db.count_documents(
                {"user_id": uid, "bot_id": bot_id, "status": "completed_detailed"}
            ),
        )
        lines.append(
            f"avatar files: uploaded={uploads_total}, completed_simple={done_simple}, completed_detailed={done_detailed}"
        )
        return lines

    def _collect_name_lines(self, user: dict | None) -> list[str]:
        if user is None:
            return ["known names: user not found in users collection"]
        lines: list[str] = []
        username = user.get("username")
        if isinstance(username, str) and username.strip():
            lines.append(f"username=@{username.strip()}")

        preferred_keys = [
            "legal_name",
            "informal_name",
            "known_names",
            "first_name",
            "last_name",
            "print_name",
            "inner_name_ru",
            "inner_name_en",
        ]
        seen: set[str] = set()
        for key in preferred_keys:
            value = self._format_name_value(user.get(key))
            if value:
                lines.append(f"{key}={value}")
                seen.add(key)

        extra_name_keys = sorted(
            key
            for key in user.keys()
            if "name" in key and key not in {"bot_username", "username"} and key not in seen
        )
        for key in extra_name_keys:
            value = self._format_name_value(user.get(key))
            if value:
                lines.append(f"{key}={value}")
        if len(lines) == 0:
            return ["known names: none"]
        return lines

    async def handle_message(self, update: TGState):
        parts = update.message.text.split(maxsplit=1)
        if len(parts) < 2:
            await update.reply("Usage: /user_echo <user_id>", parse_mode=None)
            return
        try:
            uid = int(parts[1].strip())
        except ValueError:
            await update.reply("Usage: /user_echo <user_id>", parse_mode=None)
            return

        user = await self.user_db.find_one({
            "user_id": uid,
            "bot_id": update.bot.id,
        })
        locale = user.get("language_code") if isinstance(user, dict) else None
        if user is not None:
            user_url = client_user_link_url(user)
            user_name = client_user_name(user, language_code=locale)
        else:
            user_url = f"tg://user?id={uid}"
            user_name = "Unknown"

        pass_lines, message_lines, avatar_lines = await gather(
            self._collect_pass_stats_lines(uid, update.bot.id, locale=locale),
            self._collect_message_stats_lines(uid),
            self._collect_avatar_stats_lines(uid, update.bot.id, user),
        )
        name_lines = self._collect_name_lines(user)

        lines = [
            f"<b>User</b>: <a href=\"{escape(user_url, quote=True)}\">{escape(str(user_name))}</a> (<code>{uid}</code>)",
            "",
            "<b>Known names</b>:",
        ]
        lines.extend(f"• {escape(line)}" for line in name_lines)
        lines.extend(["", "<b>Passes</b>:"])
        lines.extend(f"• {escape(line)}" for line in pass_lines)
        lines.extend(["", "<b>Messages</b>:"])
        lines.extend(f"• {escape(line)}" for line in message_lines)
        lines.extend(["", "<b>Avatars</b>:"])
        lines.extend(f"• {escape(line)}" for line in avatar_lines)
        await update.reply("\n".join(lines), parse_mode=ParseMode.HTML)
            
