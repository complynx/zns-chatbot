from typing import Any, Optional
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
from .photo_task import get_by_uuid, real_frame_size
from .food import FoodStorage
import logging
from .tg_constants import (
    IC_FOOD_PAYMENT_PAYED,
    IC_FOOD_PAYMENT_CANCEL,
)

logger = logging.getLogger(__name__)

class FitFrameHandler(tornado.web.RequestHandler):
    async def get(self):
        id_str = self.get_query_argument("id", default="")

        try:
            task = get_by_uuid(id_str)
            self.render(
                "fit_frame.html",
                task=task,
                id=id_str,
                real_frame_size=real_frame_size,
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)


class PhotoHandler(tornado.web.StaticFileHandler):
    def __init__(self, application: tornado.web.Application, request: HTTPServerRequest, **kwargs: Any) -> None:
        super().__init__(application, request, path="", **kwargs)
    
    @classmethod
    def get_absolute_path(cls, root: str, path: str) -> str:
        try:
            task = get_by_uuid(path)
            if task.file is None:
                return ""
            return task.file
        except (KeyError, ValueError):
            return ""
    
    def validate_absolute_path(self, root: str, absolute_path: str) -> Optional[str]:
        if absolute_path == "" or not os.path.isfile(absolute_path):
            raise tornado.web.HTTPError(404)
        return absolute_path

def parse_meal_data(meal_dict):
    days = ['friday', 'saturday', 'sunday']
    meals = ['lunch', 'dinner']
    restaurants_real = ['Bареничная №1','Хачапури и Вино','Гастробар Beer Side']
    restaurants_cost2 = [380, 440, 500]
    restaurants_cost3 = [430, 440, 600]
    
    ret_objs = []
    costs = []
    for day in days:
        for meal in meals:
            toggler_key = f"{day}_{meal}_restaurant_toggler"
            if toggler_key in meal_dict:
                restaurant_num = meal_dict[toggler_key].replace(f"{day}_{meal}_restaurant", "").replace("_toggler", "")
                try:
                    restaurant_num = int(restaurant_num)
                except ValueError:
                    pass

                ret_obj = {
                    "day": day,
                    "meal": meal,
                }
                if restaurant_num == "_none":
                    ret_obj["choice"] = "Не буду есть."
                    ret_obj["cost"] = 0
                    costs.append(0)
                elif 1<= restaurant_num <=3:
                    main_key = f"{day}_{meal}_main_r{restaurant_num}"
                    salad_key = f"{day}_{meal}_salad_r{restaurant_num}"
                    soup_key = f"{day}_{meal}_soup_r{restaurant_num}"
                    drink_key = f"{day}_{meal}_drink_r{restaurant_num}"
                    main = meal_dict.get(main_key, '')
                    salad = meal_dict.get(salad_key, '')
                    soup = meal_dict.get(soup_key, '')
                    cost = 0
                    if main != '' and salad != '' and soup != '':
                        cost = restaurants_cost3[restaurant_num-1]
                    else:
                        cost = restaurants_cost2[restaurant_num-1]
                    drink = meal_dict.get(drink_key, '')
                    items = [main, soup, salad, drink]
                    filtered_items = list(filter(lambda x: x != '', items))
                    result = "\n".join(filtered_items)
                    
                    ret_obj["choice"] = result
                    ret_obj["restaurant"] = restaurants_real[restaurant_num-1]
                    ret_obj["cost"] = cost

                    costs.append(cost)
                ret_objs.append(ret_obj)
    return costs, ret_objs

class MenuHandler(tornado.web.RequestHandler):
    def initialize(self, app):
        self.app = app

    async def get(self):
        meal_id=self.get_query_argument("id", "")
        food_storage: FoodStorage = self.app.food_storage
        try:
            async with food_storage.from_id(meal_id) as meal_context:
                if meal_context.choice is not None:
                    raise tornado.web.HTTPError(404)
                self.render(
                    "menu.html",
                    meal_context=meal_context.id
                )
        except FileNotFoundError:
            raise tornado.web.HTTPError(404)
    
    async def post(self):
        from datetime import datetime
        from telegram.constants import ParseMode
        from telegram import InlineKeyboardButton, InlineKeyboardMarkup
        # Get all the post form data
        data = self.request.arguments
        food_storage: FoodStorage = self.app.food_storage
        
        # Convert bytes to str if needed (Tornado stores POST data as bytes)
        for key in data:
            data[key] = [x.decode('utf-8') for x in data[key]]
            if len(data[key]) == 1:
                data[key] = data[key][0]

        meal = None
        try:
            async with food_storage.from_id(data["meal_context"]) as meal:
                cancelled = (data.get("cancelled", '') != '')
                sums, objs = parse_meal_data(data)
                total = sum(sums)
                meal.choice = objs
                meal.total = total
                meal.choice_date = datetime.now()

                if total > 0 and not cancelled:
                    formatted_choice = meal.format_choice()
                    
                    keyboard = [
                        [
                            InlineKeyboardButton("💸 Оплачено", callback_data=f"{IC_FOOD_PAYMENT_PAYED}|{meal.id}"),
                            InlineKeyboardButton("❌ Отменить", callback_data=f"{IC_FOOD_PAYMENT_CANCEL}|{meal.id}"),
                        ]
                    ]
                    await self.app.bot.bot.send_message(
                        chat_id=meal.tg_user_id,
                        text=
                        f"Я получила твой заказ для зуконавта по имени <i>{meal.for_who}</i>.\n"+
                        f"Вот содержание заказа:\n{formatted_choice}\n\n"
                        "<i>Следующий шаг</i> — оплата. Для оплаты, нужно сделать перевод"+
                        " на Сбер по номеру\n<b>+79175295923</b>\n"+
                        "Получатель: <i>Ушакова Дарья Евгеньевна</i>.\n"+
                        "⚠ Когда переведёшь, понадобится подтверждение перевода — именно "+
                        "<u><b>квитанцию (чек)</b></u> об оплате, а не просто скрин.\n"+
                        "Когда будешь готов(а) прислать его, <u><b>обязательно</b></u> нажми на кнопку:",
                        parse_mode=ParseMode.HTML,
                        reply_markup=InlineKeyboardMarkup(keyboard),
                    )
                    # If you want to send the data back as a response in pretty format
                else:
                    await self.app.bot.bot.send_message(
                        chat_id=meal.tg_user_id,
                        text=
                        f"Твой заказ для зуконавта по имени <i>{meal.for_who}</i> оказался пуст или отменён.\n"+
                        f"Если нужно сделать новый заказ, воспользуйся снова командой /food",
                        parse_mode=ParseMode.HTML,
                    )
                    meal.cancel()
                self.write(f"Ваш выбор был успешно сохранён!<br>Вкладку или окно можно закрыть.")
        except FileNotFoundError:
            return self.write_error(401)


async def create_server(config: Config, base_app):
    tornado.platform.asyncio.AsyncIOMainLoop().install()
    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler),
        (r"/menu", MenuHandler, {"app": base_app}),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server.port)
    base_app.server = app

    return app
