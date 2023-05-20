from typing import Any, Optional
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
from .photo_task import get_by_uuid, real_frame_size
import logging

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
    restaurants_real = ['B№1','ХиВ','ГBS']
    restaurants_cost2 = [380, 440, 500]
    restaurants_cost3 = [430, 440, 600]
    
    ret = []
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

                if restaurant_num == "_none":
                    ret.append("нет")
                    ret.append("нет")
                    ret.append(0)
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

                    ret.append(restaurants_real[restaurant_num-1])
                    ret.append(result)
                    ret.append(cost)
                    costs.append(cost)
    return ret, costs

class MenuHandler(tornado.web.RequestHandler):
    def initialize(self, token, app):
        self.token = token
        self.app = app

    async def get(self):
        self.render(
            "menu.html",
            meal_context=self.get_query_argument("id", "")
        )
    
    async def post(self):
        import csv
        from datetime import datetime
        # Get all the post form data
        data = self.request.arguments
        
        # Convert bytes to str if needed (Tornado stores POST data as bytes)
        for key in data:
            data[key] = [x.decode('utf-8') for x in data[key]]
            if len(data[key]) == 1:
                data[key] = data[key][0]

        meal = None
        try:
            meal = self.app.get_meal_session(data["meal_context"])
        except KeyError:
            return self.write_error(401)
        
        meals, sums = parse_meal_data(data)
        total = sum(sums)

        save = [
            datetime.now().strftime("%m/%d/%Y %H:%M:%S"),
            meal.user.id,
            meal.user.first_name,
            meal.user.last_name,
            meal.user.username,
            meal.for_who
        ]
        save.extend(meals)
        save.append(total)

        with open("/menu/menu.data", 'a') as f:
            writer = csv.writer(f)
            writer.writerow(save)

        bot = self.app.bot
        await bot.send_message(chat_id=379278985, text=f"пользователь {meal.user} выбрал {meals} для {meal.for_who}")
        # If you want to send the data back as a response in pretty format
        self.write(f"Ваш выбор был успешно сохранён!<br>Можете уже перечислить {total} рублей и прислать подтверждение.")



async def create_server(config: Config, base_app):
    tornado.platform.asyncio.AsyncIOMainLoop().install()
    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler),
        (r"/menu", MenuHandler, {"token": config.telegram_token, "app": base_app}),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server_port)
    base_app.server = app

    return app
