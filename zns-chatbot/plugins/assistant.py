from dataclasses import dataclass
import datetime
import logging
from asyncio import Event, create_task, sleep
from json import loads
import csv
from functools import lru_cache

import tiktoken
from google.oauth2 import service_account
from googleapiclient.discovery import build
from langchain.schema import AIMessage, HumanMessage, SystemMessage
from langchain_core.output_parsers import StrOutputParser
from langchain_google_genai import ChatGoogleGenerativeAI
from motor.core import AgnosticCollection
from telegram import Update
from telegram.ext import filters

from ..tg_state import TGState
from .avatar import async_thread
from .base_plugin import PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING, BasePlugin
from .massage import now_msk

output_parser = StrOutputParser()

logger = logging.getLogger(__name__)


class MessageTooLong(Exception):
    pass


NUMBER_OF_CONTEXT_DOCS = 25
RAW_NUMBER_OF_CONTEXT_DOCS = 4
QUESTION_PREFIX = "Q: "
ANSWER_PREFIX = "A: "
COMMAND_PREFIX = "%"
RECALCULATOR_LOOP = 600  # every 10 minutes
EMBEDDING_MODEL = "Alibaba-NLP/gte-large-en-v1.5"
RAG_DATABASE_INDEX = "index"
RAG_DATABASE_FOLDER = "rag_database"

DJ_DAY_CUTOFF_HOUR = 7  # DJs before this hour belong to the previous day

ABOUT_REFRESH_INTERVAL = 3600


class Assistant(BasePlugin):
    name = "assistant"
    about: str = ""

    @dataclass
    class DJTime:
        start: datetime.datetime
        dj: str
        room: str

    def _parse_column_date(self, col_name:str, year:int|None=None) -> datetime.date:
        """
        Extracts the date from a column name of format 'день, DD.MM' and returns a datetime.date.
        If year is None, uses current year.
        """
        try:
            # Split on comma and take the part after, e.g. '11.06'
            date_part = col_name.split(',')[1].strip()
            day_str, month_str = date_part.split('.')
            day = int(day_str)
            month = int(month_str)
            if year is None:
                year = datetime.datetime.now().year
            return datetime.datetime(year, month, day).date()
        except Exception:
            raise ValueError(f"Unable to parse date from column name '{col_name}'")

    @lru_cache(maxsize=1)
    def load_schedule(self) -> list[DJTime]:
        """
        Loads and caches the schedule from the given CSV file.
        Returns a list of DJTime sorted by start time.
        """
        entries = []
        # Read entire CSV into memory
        with open(self.config.line_up, newline='', encoding='utf-8') as f:
            reader = csv.reader(f)
            rows = list(reader)

        if not rows:
            return []

        header = rows[0]
        rooms = rows[1]
        current_year = datetime.datetime.now().year

        # Iterate through each schedule column (skip time column at index 0)
        num_cols = len(header)
        for col_index in range(1, num_cols):
            room = rooms[col_index].strip()
            base_date = self._parse_column_date(header[col_index], year=current_year)
            for row in rows[2:]:
                # Ensure row has enough columns
                if col_index >= len(row):
                    continue
                time_str = row[0].strip() if row[0] else ''
                dj = row[col_index].strip() if row[col_index] else ''
                if not time_str or not dj:
                    continue
                try:
                    t = datetime.datetime.strptime(time_str, "%H:%M").time()
                except ValueError:
                    continue
                # Determine correct date: times before DJ_DAY_CUTOFF_HOUR belong to next calendar day
                if t.hour < DJ_DAY_CUTOFF_HOUR:
                    entry_date = base_date + datetime.timedelta(days=1)
                else:
                    entry_date = base_date
                start_dt = datetime.datetime(entry_date.year, entry_date.month, entry_date.day, t.hour, t.minute)
                entries.append(Assistant.DJTime(
                    start=start_dt,
                    dj=dj,
                    room=room
                ))
        # Sort entries by start time
        entries.sort(key=lambda x: x.start)
        return entries

    def get_daily_djs(self, current_dt: datetime.datetime) -> dict[str, list[str]]:
        """
        Given a datetime, returns a dict mapping room names to lists of DJs scheduled for that calendar day,
        including DJs whose set starts before the cutoff hour the next day (as per timetable logic).
        If the incoming datetime is before the cutoff hour on the next day, treat it as belonging to the previous day.
        """
        cutoff = datetime.time(DJ_DAY_CUTOFF_HOUR, 0)
        # If current_dt is before cutoff hour, treat as previous day
        if current_dt.time() < cutoff:
            target_date = (current_dt - datetime.timedelta(days=1)).date()
        else:
            target_date = current_dt.date()
        next_date = target_date + datetime.timedelta(days=1)
        schedule = self.load_schedule()
        result = {}
        for entry in schedule:
            entry_date = entry.start.date()
            entry_time = entry.start.time()
            # Include entries for the target date, and for the next day before cutoff hour
            if entry_date == target_date or (entry_date == next_date and entry_time < cutoff):
                room = entry.room
                result.setdefault(room, []).append(f"{entry_time:%H:%M} {entry.dj}")
        return result

    def get_current_djs(self, current_dt: datetime.datetime) -> list[dict[str, str]]:
        """
        Given a datetime, returns a list of dicts for DJs currently playing at that moment.
        Each dict has keys 'room' and 'dj'. If no DJs are playing, returns an empty list.
        """
        schedule = self.load_schedule()
        now_entries = []
        for entry in schedule:
            start = entry.start
            end = start + datetime.timedelta(hours=1)
            if start <= current_dt < end:
                now_entries.append({'room': entry.room, 'dj': entry.dj})
        return now_entries

    def __init__(self, base_app):
        super().__init__(base_app)
        with open("static/rag_data.yaml", "r", encoding="utf-8") as f:
            import yaml

            data = yaml.safe_load(f)
            text = []
            for doc in data["collection"]:
                if "question" in doc:
                    text.append(QUESTION_PREFIX + doc["question"])
                if "alt_questions" in doc:
                    text.append(" / " + " / ".join(doc["alt_questions"]))
                if "answer" in doc:
                    text.append(ANSWER_PREFIX + doc["answer"])
                text.append("")
        self.qa_data = "\n".join(text)

        self.chat = ChatGoogleGenerativeAI(
            api_key=self.config.language_model.google_api_key.get_secret_value(),
            model=self.config.language_model.model,
            temperature=self.config.language_model.temperature,
        )
        self.message_db: AgnosticCollection = base_app.mongodb[
            self.config.mongo_db.messages_collection
        ]
        self.user_db: AgnosticCollection = base_app.users_collection
        self.tokenizer = tiktoken.encoding_for_model(
            self.config.language_model.tokenizer_model
        )

        create_task(self._refresh_about())

    @async_thread
    def fetch_document(self) -> dict:
        creds = loads(self.config.google.credentials.get_secret_value())
        credentials = service_account.Credentials.from_service_account_info(
            creds, scopes=["https://www.googleapis.com/auth/drive"]
        )
        service = build("drive", "v3", credentials=credentials)
        document = (
            service.files()
            .export(fileId=self.config.google.about_doc_id, mimeType="text/markdown")
            .execute()
        )
        return (
            document.decode(encoding="utf-8")
            if isinstance(document, bytes)
            else document
        )

    async def _refresh_about(self) -> None:
        while True:
            try:
                self.about = await self.fetch_document()
                tokens = len(self.tokenizer.encode(self.about))
                logger.info(
                    f"refreshed about, length: {len(self.about)}, tokens: {tokens}"
                )
                logger.info(self.about)
            except Exception as e:
                logger.error(f"Exception in _refresh_about: {e}", exc_info=e)
            await sleep(ABOUT_REFRESH_INTERVAL)

    def test_message(self, message: Update, state, web_app_data):
        if (filters.TEXT & ~filters.COMMAND).check_update(message):
            return PRIORITY_BASIC, None
        return PRIORITY_NOT_ACCEPTING, None

    async def handle_message(self, update: TGState):
        stopper = Event()
        create_task(update.keep_sending_chat_action_until(stopper))
        try:
            repl = await self.get_assistant_reply(
                update.update.message.text_markdown_v2, update.user, update
            )
        finally:
            stopper.set()
        await update.reply(repl, parse_mode=None)

    async def userinfo(self, update: TGState) -> str:
        from ..telegram_links import client_user_name

        user = await update.get_user()
        ret = f"имя пользователя {client_user_name(user)}"
        if (
            "username" in user
            and user["username"] is not None
            and user["username"] != ""
        ):
            ret += f", ник @{user['username']}"
        return ret

    # async def context_userfood(self, update: TGState) -> str:
    #     orders = await self.base_app.food.get_user_orders_assist(update.user)
    #     if orders != "":
    #         return "\n заказы пользователя в /meal\n"+orders
    #     else:
    #         return "\nпользователь пока не заказал ничего через /meal"

    # async def context_userfood_closest(self, update: TGState) -> str:
    #     return await self.base_app.food.get_user_orders_assist_closest(update.user)

    # async def context_userfood_today(self, update: TGState) -> str:
    #     return await self.base_app.food.get_user_orders_assist_today(update.user)

    # async def context_userfood_tomorrow(self, update: TGState) -> str:
    #     return await self.base_app.food.get_user_orders_assist_tomorrow(update.user)

    async def get_assistant_reply(self, message: str, user_id: int, update: TGState):
        date_1_day_ago = datetime.datetime.now() - datetime.timedelta(days=1)
        ctx = self.about + "\n\n## QA" + self.qa_data
        if self.config.logging.level == "DEBUG":
            msgs = await self.message_db.find(
                {"date": {"$gte": date_1_day_ago}, "role": "user", "user_id": user_id}
            ).to_list(length=1000)
            logger.debug(f"latest messages: {msgs}")
        result = await self.message_db.aggregate(
            [
                {
                    "$match": {
                        "date": {"$gte": date_1_day_ago},
                        "role": "user",
                        "user_id": user_id,
                    }
                },
                {"$group": {"_id": None, "count": {"$sum": 1}}},
            ]
        ).to_list(length=1)
        last_question = ""
        logger.debug(
            f"latest messages count: {result} ?> {self.config.language_model.max_messages_per_user_per_day}"
        )
        if (
            len(result) > 0
            and result[0]["count"]
            > self.config.language_model.max_messages_per_user_per_day
        ):
            return update.l("max-assistant-messages-reached")
        if len(result) > 0:
            limit = (
                self.config.language_model.max_messages_per_user_per_day
                - result[0]["count"]
            )
            last_question = f"\n this user has only {limit} questions left for today"

        length = len(self.tokenizer.encode(message))
        tokens_message = length
        if length > self.config.language_model.message_token_cap:
            raise MessageTooLong()
        logger.debug(f"message length {length}")
        messages = [
            HumanMessage(
                content=message,
            )
        ]

        cursor = self.message_db.find({"user_id": {"$eq": user_id}}).sort("date", -1)
        async for prev_message in cursor:
            ml = len(self.tokenizer.encode(prev_message["content"]))
            logger.debug(
                f"hist message from {prev_message['role']} ```{prev_message['content']}``` len: {ml}, current total {length}"
            )
            if length + ml > self.config.language_model.message_token_cap:
                logger.debug(f"broke the cap with length {length + ml}")
                break
            length += ml
            if prev_message["role"] == "user":
                msg = HumanMessage(content=prev_message["content"])
            else:
                msg = AIMessage(content=prev_message["content"])
            messages = [msg] + messages
        await cursor.close()
        tokens_history = length - tokens_message

        ctx_now = now_msk()

        ctx_date = "\n\nnow it's " + ctx_now.strftime("%A, %d %B %Y, %H:%M") + "\n"

        lineups = ""
        current_djs_str = ""
        current_djs = self.get_current_djs(ctx_now)
        if current_djs:
            current_djs_str += "\n\ncurrent DJs:\n" + "\n".join(
                f"{entry['room']}: {entry['dj']}"
                for entry in current_djs
            )
        else:
            current_djs_str += ("\n\nno party right now, no one is playing, "+
            "don't answer somebody is playing if nobody is playing right now!"+
            "If the question is who's playing right now, just say no one is playing!")
        day_lineups = ""
        daily_djs = self.get_daily_djs(ctx_now)
        if daily_djs:
            day_lineups += "\n\n"
            if not current_djs:
                day_lineups += "no one is playing right now, but "
            day_lineups += "here are the DJs scheduled for today:\n"
            day_lineups += "\n".join(
                f"{room}:\n " + "\n ".join(djs)
                for room, djs in daily_djs.items()
            )
        else:
            day_lineups += "\n\nno party today"
        # Group entries by (date, room)
        from collections import defaultdict
        grouped = defaultdict(list)
        for entry in self.load_schedule():
            entry_date = entry.start.date()
            if entry.start.hour < DJ_DAY_CUTOFF_HOUR:
                entry_date -= datetime.timedelta(days=1)
            key = (entry_date, entry.room)
            grouped[key].append(entry)
        lineups += "\n\n"
        if not daily_djs:
            lineups += "today is not yet a marathon day, but "
        lineups += "here are the full lineups for the marathon:\n"
        for (date, room), entries in sorted(grouped.items()):
            lineups += f"\n{date:%a %d.%m}, {room}"
            for entry in entries:
                lineups += f"\n {entry.start.strftime('%H:%M')} {entry.dj}"
        lineups += day_lineups + current_djs_str
        logger.debug(f"lineups: {lineups}")
        ctx += lineups

        messages = (
            [
                SystemMessage(
                    content="""
You're a helpful assistant bot, девушка по имени ЗиНуСя.
You help participants of the dance marathon with questions.
Ты усердная, но немного блондинка.
Answer questions in a friendly and warm manner.
Keep responses short and to the point.
You can use emojis in the replies to make them more friendly.
You have to answer in the same language as the users message.
Answer the questions with the help of the following context:
```
"""
                    + ctx
                    + ctx_date
                    + """
```
Avoid long and formal answers. If some details are not known, for instance sex of the participant, their region or anything,
please ask a clarification question.
If a @-mention or /-command, is relevant to a question, it is really helpful to include them.
Ответ должен быть на языке вопроса участника. Перефразируй ответы в стиле девушки-помощника.
You must answer in the same language as the users messages.
"""
                    + last_question
                )
            ]
            + messages
            + [
                SystemMessage(
                    content="You must answer in the same language as the users question."
                )
            ]
        )

        await self.message_db.insert_one(
            {
                "content": message,
                "tokens_message": tokens_message,
                "tokens_context": len(self.tokenizer.encode(ctx)),
                "tokens_history": tokens_history,
                "role": "user",
                "user_id": user_id,
                "date": datetime.datetime.now(),
            }
        )

        result = await self.chat.ainvoke(messages)

        logger.info("result: %s", result)
        await self.message_db.insert_one(
            {
                "content": result.content,
                "response_metadata": result.response_metadata,
                "usage_metadata": result.usage_metadata,
                "role": "assistant",
                "user_id": user_id,
                "date": datetime.datetime.now(),
            }
        )
        return result.content
