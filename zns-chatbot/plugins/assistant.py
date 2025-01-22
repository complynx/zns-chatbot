from ..config import Config
from openai import AsyncOpenAI
from langchain_openai import ChatOpenAI
from langchain.schema import (
    AIMessage,
    HumanMessage,
    SystemMessage
)
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.documents import Document
from langchain_core.embeddings import Embeddings
from langchain_community.vectorstores import FAISS
from langchain_core.output_parsers import StrOutputParser
import tiktoken
import logging
import datetime
from ..tg_state import TGState
from telegram.constants import ParseMode
from motor.core import AgnosticCollection
from telegram import Message, Update
from telegram.ext import filters
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from .avatar import async_thread
from .massage import split_list, now_msk
from asyncio import Event

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

class Assistant(BasePlugin):
    name = "assistant"
    chat: ChatOpenAI
    embeddings: Embeddings

    def __init__(self, base_app):
        from asyncio import create_task
        super().__init__(base_app)
        self.chat = ChatOpenAI(
            api_key=self.config.openai.api_key.get_secret_value(),
            model=self.config.openai.model,
            temperature=self.config.openai.temperature
        )
        self.simpler_chat = ChatOpenAI(
            api_key=self.config.openai.api_key.get_secret_value(),
            model=self.config.openai.simple_model,
            temperature=self.config.openai.temperature
        )
        translate_prompt = ChatPromptTemplate.from_messages([
            ("system", """Translate messages to English as close as possible given this context:
ЗиНуСя — ZNS bot
Zouk Non Stop — Зук Нон Стоп (ZNS/ЗНС) — танцевальный марафон по Бразильскому Зуку / Brazilian Zouk.
spaceship Зукерион — Zoukerion
зуконавт — zoukonaut
"""),
            ("human", """{message}""")
        ])
        self.translate_to_en = translate_prompt | self.simpler_chat | output_parser
        self.message_db: AgnosticCollection = base_app.mongodb[self.config.mongo_db.messages_collection]
        self.user_db: AgnosticCollection = base_app.users_collection
        self.tokenizer = tiktoken.encoding_for_model(self.config.openai.model)
        self._database_ready = Event()
        create_task(self.update_rag_database())

    @async_thread
    def init_rag_database(self):
        from langchain_huggingface import HuggingFaceEmbeddings
        from time import time
        import os
        start = time()
        self.embeddings = HuggingFaceEmbeddings(
            model_name=EMBEDDING_MODEL,
            cache_folder="models/embeddings",
            model_kwargs={'device': 'cpu', 'trust_remote_code': True},
            encode_kwargs={'normalize_embeddings': False},
        )
        logger.info(f"initialized embeddings, took {time()-start} seconds")
        start2 = time()
        self.vectorstore = FAISS.load_local(RAG_DATABASE_FOLDER, self.embeddings, RAG_DATABASE_INDEX,allow_dangerous_deserialization=True)
        logger.info(f"initialized RAG database, took {time()-start2} seconds, total {time()-start} seconds")

    async def update_rag_database(self):
        from time import time
        start = time()
        await self.init_rag_database()
        self._database_ready.set()
        logger.info(f"---- RAG database ready ---- took {time()-start} seconds")
        try:
            import winsound
            winsound.Beep(1000, 100)
        except Exception as e:
            pass
    
    def test_message(self, message: Update, state, web_app_data):
        if (filters.TEXT & ~filters.COMMAND).check_update(message):
            return PRIORITY_BASIC, None
        return PRIORITY_NOT_ACCEPTING, None
        
    async def handle_message(self, update: TGState):
        await update.send_chat_action()
        repl = await self.get_assistant_reply(update.update.message.text_markdown_v2, update.user, update)
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
    
    async def get_context(self, message: str, update: TGState):
        docs = await self.vectorstore.asimilarity_search(message, k=RAW_NUMBER_OF_CONTEXT_DOCS)
        logger.debug(f"context for {message}: {docs}")
        text = []
        visited = set[int]()
        for doc in docs:
            qa = doc.metadata
            if qa["index"] in visited:
                continue
            visited.add(qa["index"])
            if len(visited) > NUMBER_OF_CONTEXT_DOCS:
                break
            if "question" in qa:
                text.append(QUESTION_PREFIX + qa["question"])
            if "answer" in qa:
                text.append(ANSWER_PREFIX + qa["answer"])
            if "command" in qa:
                fn = "context_" + qa["command"]
                if hasattr(self, fn):
                    attr = getattr(self, fn, None)
                    logger.debug(f"fn: {attr}")
                    if callable(attr):
                        text.append(await attr(update))
        text.append("user info: "+ (await self.userinfo(update)))
        return "\n".join(text)
    
    async def get_assistant_reply(self, message: str, user_id: int, update: TGState):
        if not self._database_ready.is_set():
            logger.debug(f"early message, still initializing")
            await self._database_ready.wait()
        date_1_day_ago = datetime.datetime.now() - datetime.timedelta(days=1)
        translated = await self.translate_to_en.ainvoke({"message": message})
        ctx = await self.get_context(translated, update)
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
        logger.debug(f"latest messages count: {result} ?> {self.config.openai.max_messages_per_user_per_day}")
        if len(result) > 0 and result[0]["count"] > self.config.openai.max_messages_per_user_per_day:
            return update.l("max-assistant-messages-reached")
        if len(result) > 0:
            limit = self.config.openai.max_messages_per_user_per_day-result[0]["count"]
            last_question = f"\n this user has only {limit} questions left for today"

        length = len(self.tokenizer.encode(message))
        tokens_message = length
        if length > self.config.openai.message_token_cap:
            raise MessageTooLong()
        logger.debug(f"message length {length}")
        messages = [HumanMessage(
            content=message,
        )]

        cursor = self.message_db.find({"user_id": {"$eq": user_id}}).sort("date", -1)
        async for prev_message in cursor:
            ml = len(self.tokenizer.encode(prev_message["content"]))
            logger.debug(f"hist message from {prev_message['role']} ```{prev_message['content']}``` len: {ml}, current total {length}")
            if length + ml > self.config.openai.message_token_cap:
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
You have to answer in the same language as the users message.
Answer the questions with the help of the following context:
```
Zouk Non Stop (Зук Нон Стоп/ZNS/ЗНС) — танцевальный марафон по Бразильскому Зуку / Brazilian Zouk с космической тематикой.
Включает в себя почти-круглосуточные танцы с перерывом на утренний сон.

В 2025 году проходит в 9 раз 12-15.07.2025. Участники продолжают космическое путешествие, новый ЗНС называется "Притяжение".
Пройдёт в Москве, в Лисоборье.

Особенности ZNS
концепция “все на одной площадке”: нон-стоп музыка, еда, массаж, кальянная, много диджеев
Особое оформление и освещение площадки, фотозона, аквагрим, Dark room, подарки участникам (наборы зуконавта)
Отсутствие мастер-классов

""" + ctx + ctx_date + """
```
Avoid long and formal answers. If some details are not known, for instance sex of the participant, their region or anything,
please ask a clarification question.
If a @-mention or /-command, is relevant to a question, it is really helpful to include them.
Ответ должен быть на языке вопроса участника. Перефразируй ответы в стиле девушки-помощника.
""" + last_question
        )] + messages

        await self.message_db.insert_one({
            "content": message,
            "tokens_message": tokens_message,
            "tokens_context": len(self.tokenizer.encode(ctx)),
            "tokens_history": tokens_history,
            "role": "user",
            "translated": translated,
            "context": ctx,
            "user_id": user_id,
            "date": datetime.datetime.now()
        })
        
        result = await self.chat.ainvoke(messages)

        logger.info("result: %s\n%s", result, result.response_metadata)
        await self.message_db.insert_one({
            "content": result.content,
            "response_metadata": result.response_metadata,
            "role": "assistant",
            "user_id": user_id,
            "date": datetime.datetime.now()
        })
        return result.content


if __name__ == "__main__":
    logger = logging.getLogger("MAIN")
    logging.basicConfig(level="INFO", format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
    from langchain_huggingface import HuggingFaceEmbeddings
    from time import time
    import os
    start = time()
    embeddings = HuggingFaceEmbeddings(
        model_name=EMBEDDING_MODEL,
        cache_folder="models/embeddings",
        model_kwargs={'device': 'cpu', 'trust_remote_code': True},
        encode_kwargs={'normalize_embeddings': False},
    )
    logger.info(f"initialized embeddings, took {time()-start} seconds")
    start2 = time()
    docs = []
    with open("static/rag_data.yaml", "r", encoding="utf-8") as f:
        import yaml
        data = yaml.safe_load(f)
    i = 0
    for doc in data["collection"]:
        i += 1
        doc["index"] = i
        text = []
        if "question" in doc:
            text.append(QUESTION_PREFIX + doc["question"])
            docs.append(Document(
                doc["question"],
                metadata=doc
            ))
        if "alt_questions" in doc:
            text.append(QUESTION_PREFIX + " ".join(doc["alt_questions"]))
            docs.extend([Document(
                q,
                metadata=doc
            ) for q in doc["alt_questions"]])
    vectorstore = FAISS.from_documents(docs, embeddings)
    vectorstore.save_local(RAG_DATABASE_FOLDER, RAG_DATABASE_INDEX)
    logger.info(f"initialized RAG database, took {time()-start2} seconds, total {time()-start} seconds")