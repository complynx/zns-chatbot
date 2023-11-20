from .config import Config
from openai import AsyncOpenAI
import tiktoken
import logging
import datetime
from telegram.constants import ParseMode

logger = logging.getLogger(__name__)

class MessageTooLong(Exception):
    pass

class Assistant():
    base_app = None
    config: Config
    client: AsyncOpenAI

    def __init__(self, base_app, base_config: Config):
        self.config = base_config
        self.base_app = base_app
        self.client = AsyncOpenAI(api_key=base_config.openai.api_key.get_secret_value())
        self.message_db = base_app.mongodb[base_config.mongo_db.messages_collection]
        self.user_db = base_app.users_collection
        self.model = base_config.openai.model
        self.tokenizer = tiktoken.encoding_for_model(self.model)
    
    async def reply_to(self, message: str, user_id: int, chat_id):
        length = len(self.tokenizer.encode(message))
        if length > self.config.openai.message_token_cap:
            raise MessageTooLong()
        logger.debug(f"message length {length}")
        messages = [{
            "content": message,
            "role": "user",
        }]

        cursor = self.message_db.find({"user_id": {"$eq": user_id}}).sort("date", -1)
        async for prev_message in cursor:
            ml = len(self.tokenizer.encode(prev_message["content"]))
            logger.debug(f"hist message from {prev_message['role']} ```{prev_message['content']}``` len: {ml}, current total {length}")
            if length + ml > self.config.openai.message_token_cap:
                logger.debug(f"broke the cap with length {length + ml}")
                break
            length += ml
            messages = [{
                "content": prev_message["content"],
                "role": prev_message["role"]
            }] + messages
        await cursor.close()

        await self.message_db.insert_one({
            "content": message,
            "role": "user",
            "user_id": user_id,
            "date": datetime.datetime.now()
        })

        messages = [{
            "role": "system",
            "content": """
You are assistant named ЗиНуСя to help people with info about Zouk Non Stop, Moscow Zouk Marathon. (Бразильский) Зук, participants — зукеры or зуконавты (for this event).
Next is 7th edition named Поток, June 12-16 2024
The theme is exoplanetary ocean. Our spaceship Зукерион landed on exoplanet with deep oceans and beautiful oceanic lifeforms.
The main event will be in Лисоборье, there will be open-air in Парк Горького.
Answer in language of the question using telegram markdown.
Be more short and informal, like chat buddy.
"""
        }] + messages
        # import json
        # return await self.base_app.bot.bot.send_message(
        #     text=f"```json\n{json.dumps(messages,ensure_ascii=False, indent=4)}\n```",
        #     parse_mode=ParseMode.MARKDOWN,
        #     chat_id=chat_id,
        # )
        completion = await self.client.chat.completions.create(
            model=self.config.openai.model,
            messages=messages,
            max_tokens=self.config.openai.reply_token_cap,
        )

        logger.info("completion: %s", completion)
        await self.message_db.insert_one({
            "content": completion.choices[0].message.content,
            "role": completion.choices[0].message.role,
            "user_id": user_id,
            "date": datetime.datetime.now()
        })
        await self.base_app.bot.bot.send_message(
            text=completion.choices[0].message.content,
            parse_mode=ParseMode.MARKDOWN,
            chat_id=chat_id,
        )
