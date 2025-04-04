from langchain_google_genai import ChatGoogleGenerativeAI
from langchain.schema import (
    AIMessage,
    HumanMessage,
    SystemMessage
)
from asyncio import create_task, sleep
from langchain_core.output_parsers import StrOutputParser
import tiktoken
import logging
import datetime
from ..tg_state import TGState
from telegram.constants import ParseMode
from motor.core import AgnosticCollection
from telegram import Message, Update
from google.oauth2 import service_account
from telegram.ext import filters
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from .avatar import async_thread
from .massage import split_list, now_msk
from asyncio import Event
from json import loads
from googleapiclient.discovery import build

output_parser = StrOutputParser()

logger = logging.getLogger(__name__)

class MessageTooLong(Exception):
    pass

NUMBER_OF_CONTEXT_DOCS = 25
RAW_NUMBER_OF_CONTEXT_DOCS = 4
QUESTION_PREFIX = "Q: "
ANSWER_PREFIX = "A: "
COMMAND_PREFIX = "%"
RECALCULATOR_LOOP = 600 # every 10 minutes
EMBEDDING_MODEL = "Alibaba-NLP/gte-large-en-v1.5"
RAG_DATABASE_INDEX = "index"
RAG_DATABASE_FOLDER = "rag_database"

ABOUT_REFRESH_INTERVAL = 3600


class Assistant(BasePlugin):
    name = "assistant"
    about: str = ""

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
            temperature=self.config.language_model.temperature
        )
        self.message_db: AgnosticCollection = base_app.mongodb[self.config.mongo_db.messages_collection]
        self.user_db: AgnosticCollection = base_app.users_collection
        self.tokenizer = tiktoken.encoding_for_model(self.config.language_model.tokenizer_model)
        
        create_task(self._refresh_about())

    @async_thread
    def fetch_document(self) -> dict:
        creds = loads(self.config.google.credentials.get_secret_value())
        credentials = service_account.Credentials.from_service_account_info(
            creds, scopes= ['https://www.googleapis.com/auth/drive']
        )
        service = build('drive', 'v3', credentials=credentials)
        document = service.files().export(fileId=self.config.google.about_doc_id, mimeType="text/markdown").execute()
        return document.decode(encoding="utf-8") if isinstance(document, bytes) else document
    
    async def _refresh_about(self) -> None:
        while True:
            try:
                self.about = await self.fetch_document()
                tokens = len(self.tokenizer.encode(self.about))
                logger.info(f"refreshed about, length: {len(self.about)}, tokens: {tokens}")
                logger.debug(self.about)
            except Exception as e:
                logger.error(f"Exception in _refresh_about: {e}", exc_info=e)
            await sleep(ABOUT_REFRESH_INTERVAL)
    
    def test_message(self, message: Update, state, web_app_data):
        if (filters.TEXT & ~filters.COMMAND).check_update(message):
            return PRIORITY_BASIC, None
        return PRIORITY_NOT_ACCEPTING, None
        
    async def handle_message(self, update: TGState):
        stopper = Event()
        create_task(self.send_typing(update, stopper))
        try:
            repl = await self.get_assistant_reply(update.update.message.text_markdown_v2, update.user, update)
        finally:
            stopper.set()
        await update.reply(repl, parse_mode=None)
    
    async def userinfo(self, update: TGState) -> str:
        from ..telegram_links import client_user_name
        user = await update.get_user()
        ret = f"имя пользователя {client_user_name(user)}"
        if "username" in user and user["username"] is not None and \
            user["username"] != "":
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

    async def send_typing(self, update: TGState, stop: Event) -> None:
        from asyncio import sleep
        while not stop.is_set():
            await update.send_chat_action()
            await sleep(5) # action is sent for 5 seconds
    
    async def get_assistant_reply(self, message: str, user_id: int, update: TGState):
        date_1_day_ago = datetime.datetime.now() - datetime.timedelta(days=1)
        ctx = self.about +"\n\n## QA"+ self.qa_data
        if self.config.logging.level == "DEBUG":
            msgs = await self.message_db.find(
            {
                "date": {"$gte": date_1_day_ago},
                "role": "user",
                "user_id": user_id
            }).to_list(length=1000)
            logger.debug(f"latest messages: {msgs}")
        result = await self.message_db.aggregate([
            {
                "$match": {
                    "date": {"$gte": date_1_day_ago},
                    "role": "user",
                    "user_id": user_id
                }
            },
            {
                "$group": {
                    "_id": None,
                    "count": {"$sum": 1}
                }
            }
        ]).to_list(length=1)
        last_question = ""
        logger.debug(f"latest messages count: {result} ?> {self.config.language_model.max_messages_per_user_per_day}")
        if len(result) > 0 and result[0]["count"] > self.config.language_model.max_messages_per_user_per_day:
            return update.l("max-assistant-messages-reached")
        if len(result) > 0:
            limit = self.config.language_model.max_messages_per_user_per_day-result[0]["count"]
            last_question = f"\n this user has only {limit} questions left for today"

        length = len(self.tokenizer.encode(message))
        tokens_message = length
        if length > self.config.language_model.message_token_cap:
            raise MessageTooLong()
        logger.debug(f"message length {length}")
        messages = [HumanMessage(
            content=message,
        )]

        cursor = self.message_db.find({"user_id": {"$eq": user_id}}).sort("date", -1)
        async for prev_message in cursor:
            ml = len(self.tokenizer.encode(prev_message["content"]))
            logger.debug(f"hist message from {prev_message['role']} ```{prev_message['content']}``` len: {ml}, current total {length}")
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

        ctx_date = "\n\nnow it's " +now_msk().strftime("%A, %d %B %Y, %H:%M")+ "\n"

        messages = [
            SystemMessage(content= """
You're a helpful assistant bot, девушка по имени ЗиНуСя.
You help participants of the dance marathon with questions.
Ты усердная, но немного блондинка.
Answer questions in a friendly and warm manner.
Keep responses short and to the point.
You can use emojis in the replies to make them more friendly.
You have to answer in the same language as the users message.
Answer the questions with the help of the following context:
```
""" + ctx + ctx_date + """
```
Avoid long and formal answers. If some details are not known, for instance sex of the participant, their region or anything,
please ask a clarification question.
If a @-mention or /-command, is relevant to a question, it is really helpful to include them.
Ответ должен быть на языке вопроса участника. Перефразируй ответы в стиле девушки-помощника.
You must answer in the same language as the users messages.
""" + last_question
        )] + messages

        await self.message_db.insert_one({
            "content": message,
            "tokens_message": tokens_message,
            "tokens_context": len(self.tokenizer.encode(ctx)),
            "tokens_history": tokens_history,
            "role": "user",
            "user_id": user_id,
            "date": datetime.datetime.now()
        })
        
        result = await self.chat.ainvoke(messages)

        logger.info("result: %s", result)
        await self.message_db.insert_one({
            "content": result.content,
            "response_metadata": result.response_metadata,
            "usage_metadata": result.usage_metadata,
            "role": "assistant",
            "user_id": user_id,
            "date": datetime.datetime.now()
        })
        return result.content

