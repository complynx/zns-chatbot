from ..config import Config
from openai import AsyncOpenAI
import tiktoken
import logging
import datetime
from ..tg_state import TGState
from telegram.constants import ParseMode
from motor.core import AgnosticCollection
from telegram import Message, Update
from telegram.ext import filters
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING

logger = logging.getLogger(__name__)

class MessageTooLong(Exception):
    pass

class Assistant(BasePlugin):
    name = "assistant"
    client: AsyncOpenAI

    def __init__(self, base_app):
        super().__init__(base_app)
        self.client = AsyncOpenAI(api_key=self.config.openai.api_key.get_secret_value())
        self.message_db: AgnosticCollection = base_app.mongodb[self.config.mongo_db.messages_collection]
        self.user_db: AgnosticCollection = base_app.users_collection
        self.model = self.config.openai.model
        self.tokenizer = tiktoken.encoding_for_model(self.model)
    
    def test_message(self, message: Update, state, web_app_data):
        if (filters.TEXT & ~filters.COMMAND).check_update(message):
            return PRIORITY_BASIC, None
        return PRIORITY_NOT_ACCEPTING, None
        
    async def handle_message(self, update: TGState):
        await update.send_chat_action()
        repl = await self.get_assistant_reply(update.update.message.text_markdown_v2, update.user, update)
        await update.reply(repl, parse_mode=None)

    async def user_info(self, user_id, tgUpdate: Update):
        user = await self.user_db.find_one({
            "user_id": user_id,
            "bot_id": self.bot.id,
        })
        ret = f"имя пользователя {tgUpdate.effective_user.full_name}"
        if tgUpdate.effective_user.username is not None:
            ret += f", ник @{tgUpdate.effective_user.username}"
        orders = await self.base_app.food.get_user_orders_assist(user_id)
        if orders != "":
            ret += "\n заказы пользователя в /meal\n"+orders
        else:
            ret += "\nпользователь пока не заказал ничего через /meal"
        if 'avatars_called' in user:
            ret += f"\nаватарок создано: {user['avatars_called']}"
        return ret
    
    async def get_assistant_reply(self, message: str, user_id: int, update: TGState):
        date_1_day_ago = datetime.datetime.now() - datetime.timedelta(days=1)
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
            last_question = f"\nвопросов на сегодня осталось: {limit}, следующие пользователь сможет задать завтра"

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

        user_info = await self.user_info(user_id, update.update)

        messages = [{
            "role": "system",
            "content": """
Ты — полезный бот-помощник, девушка по имени ЗиНуСя, помогающая с вопросами участникам танцевального марафона. Усердная, но немного блондинка.
Информация о марафоне
```
Zouk Non Stop (Зук Нон Стоп/ZNS/ЗНС) — танцевальный марафон по Бразильскому Зуку / Brazilian Zouk с космической тематикой. Включает в себя почти-круглосуточные танцы с перерывом на утренний сон.

В 2024 году проходит в 7 раз 12-16.06.2024. Участники продолжают космическое путешествие, космический корабль Зукерион приводнился на одну из водных экзопланет с уникальной экосистемой и мы погружаемся в “Поток” (название ZNS 7)

Особенности ZNS
концепция “все на одной площадке”: безлимитные танцы с нон-стоп музыкой, столовая, снеки, горячее питание, массажисты и кальянная комната, раздевалки, душевые, туалетные комнаты с базовыми уходовыми средствами
большое количество диджеев, фотографов, видеографов
Особое кастомное оформление и освещение площадки, фотозона, аквагрим, утренние разминки, Dark room, подарки участникам (наборы зуконавта)
Отсутствие мастер-классов
баланс партнеров и партнерш ~ 50/50 (сейчас уже более 230 чел, 51.5% / 48.5%)
60+ часов танцев, 21 диджей, 7 спутников, 4 массажиста, 4 видеографа, 3 фотографа, 3 гримера, 1 кальянщик
дружеская атмосфера  для полного расслабления и погружения в танцевальный поток

ЗНС дважды становился “событием года” по версии Russian Zouk Awards (2019 2023)
Локация Россия, Москва, Старокирочный переулок 2, школа танцев “Лисоборье”
Для входа обязателен оригинал паспорта или пропуск. Пропуск иностранцам оформляется по фото загранпаспорта не менее чем за неделю до мероприятия через телеграм @liubka_z
При выходе с территории после 1 AM вход обратно невозможен
Программа:
12.6 - 16.00-06.00 pre-party в Лисоборье (входит в пасс), без пасса 2200 с контролем баланса
13.6 - Open-air в парке Коломенское в новом шатре у дворца (280м², у входа 4 в парк), можно прийти без пасса, обязательна сменка.
основное мероприятие в Лисоборье:
14.6 - 20.00-06.00
15.6 - 13.00-06.00
16.6 - 13.00-06.00
Фулл-пасс — от 8600 Rub, текущую цену по лоту и наличие мест знает амбассадор по региону
Weekend-пасс — только основные даты, фиксированная стоимость 7500 Rub
На отдельные вечеринки пассы не предусмотрены
В задачу Амбассадоров входит сбор группы, прием/рассрочка/возврат платежей, ответы на вопросы участников
Вода, чай, кофе, снеки входят в пасс
На площадке предоставляется горячее питание за отдельную плату. Питание предоставляет компания МИЛТИ (MEALTY).
На площадке будут:
Dark room — работает на площадке ночью, в пиковые танцевальные часы как альтернатива основному танцполу. В отдельном помещении почти без света звучит спокойная, залипательная музыка под которую можно максимально расслабиться и “улететь”
Кальянная — работает на площадке ночью, в пиковые танцевальные часы, в отдельном изолированном помещении. Входит в пасс
Массажисты — На площадке ежедневно работают 3-4 массажиста к которым можно будет записаться во время ZNS через бот, услуга оплачивается отдельно
Спутники — это партнеры в задачу которых входит танцевать с любыми партнершами не более 2-3 танцев подряд в течение нескольких часов в день, таким образом не давая им скучать. В часы работы Спутника у него горит синий браслет
По оплате: Оплата 2000 rub в течение 7 дней с момента брони. В течение 60 дней, но не позднее 7 дней до начала ивента, внесение остатка. Иначе бронь слетает
Списки ожидания для партнёрш — так как партнёрш больше, они ставятся в лист ожидания. Место в списке не раскрывается, спрашивать амбассадора не стоит. Чтобы не ждать — можно найти партнёра и пойти парой. Поиск партнёра — задача партнёрши.
Проживание участники ищут сами
Амбассадоры и их телеграм:
Христина Москва @hri_stinka — только Москва, Московская Область, Нижний Новгород
Оля Тесла @o_tesl — Северо-Запад России, Калининград и иностранцы
Елена Ергина @ElenaErgina — Западная часть России, кроме регионов, Христины и Оли
Анна Рякина @annyrya — территория России, расположенная восточнее Уральских гор
Питание: Всё питание готовится день в день и привозится в вакуумной упаковке в охлаждённом виде. На площадке есть место, где разогреть. Срок хранения от 12-72 часов. Заказ нужно оформить и оплатить до 4 июня включительно.
Питание будет в дни лисоборья, обед и ужин. В пятницу из-за позднего старта только ужин. Обед примерно в 17, ужин примерно в 22.
Чтобы заказать питание, пользователю надо нажать, ввести или выбрать в меню "/meal" и после этого следовать твоим инструкциям. Там также есть информация о самих блюдах, их состав, фото. Всё автоматизировано.
```
Используя эту информацию, как можно точнее ответь на вопрос участника.
По-возможности избегай длинных и формальных ответов. Если неизвестны детали требуемые для короткого ответа, например пол участника, день брони или город проживания для определения амбассадора, задай наводящий вопрос.
Обязательно указывай @-тег, если он есть в ответе. Обязательно указывай /-команду, если она есть в ответе.
Если пользователь хочет аватарку фестиваля, попроси просто прислать фото.
Ответ должен быть на языке вопроса участника.
""" + user_info + last_question
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
            temperature=self.config.openai.temperature,
        )

        logger.info("completion: %s", completion)
        await self.message_db.insert_one({
            "content": completion.choices[0].message.content,
            "role": completion.choices[0].message.role,
            "user_id": user_id,
            "date": datetime.datetime.now()
        })
        return completion.choices[0].message.content
