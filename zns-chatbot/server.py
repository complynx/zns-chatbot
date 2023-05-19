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

def check_hmac(data, secret_key):
    import hashlib,hmac

    # Extract the hash
    tg_hash = data.get('hash')
    # Make a copy of the data and remove the hash
    data_without_hash = dict(data)
    data_without_hash.pop('hash')
    
    # Sort the data by keys and serialize it to a check_string
    check_string = "\n".join(f"{k}={data_without_hash[k]}" for k in sorted(data_without_hash.keys()))
    
    # Calculate a new hash using the secret_key and check_string
    secret_key = hashlib.sha256(secret_key.encode()).digest()
    new_hash = hmac.new(secret_key, check_string.encode(), digestmod=hashlib.sha256).hexdigest()
    
    # Compare the new hash with the received tg_hash
    return new_hash == tg_hash

def parse_meal_data(meal_dict):
    days = ['friday', 'saturday', 'sunday']
    meals = ['lunch', 'dinner']
    restaurants_real = ['B№1','ХиВ','ГBS']
    
    ret = dict()
    for day in days:
        ret[day] = dict()
        for meal in meals:
            toggler_key = f"{day}_{meal}_restaurant_toggler"
            if toggler_key in meal_dict:
                restaurant_num = meal_dict[toggler_key].replace(f"{day}_{meal}_restaurant", "").replace("_toggler", "")
                try:
                    restaurant_num = int(restaurant_num)
                except ValueError:
                    pass

                if restaurant_num == "_none":
                    ret[day][meal] = "нет"
                elif 1<= restaurant_num <=3:
                    main_key = f"{day}_{meal}_main_r{restaurant_num}"
                    salad_key = f"{day}_{meal}_salad_r{restaurant_num}"
                    soup_key = f"{day}_{meal}_soup_r{restaurant_num}"
                    drink_key = f"{day}_{meal}_drink_r{restaurant_num}"
                    main = meal_dict.get(main_key, '')
                    salad = meal_dict.get(salad_key, '')
                    soup = meal_dict.get(soup_key, '')
                    drink = meal_dict.get(drink_key, '')
                    items = [main, soup, salad, drink]
                    filtered_items = list(filter(lambda x: x != '', items))
                    result = "\n".join(filtered_items)

                    ret[day][meal] = f"{restaurants_real[restaurant_num-1]}:\n{result}"
    return ret

class MenuHandler(tornado.web.RequestHandler):
    def initialize(self, token):
        self.token = token
    
    async def post(self):
        import json
        # Get all the post form data
        data = self.request.arguments
        
        # Convert bytes to str if needed (Tornado stores POST data as bytes)
        for key in data:
            data[key] = [x.decode('utf-8') for x in data[key]]
            if len(data[key]) == 1:
                data[key] = data[key][0]

        tg_user = json.loads(data["tg_user"])
        if not check_hmac(tg_user, self.token):
            return self.write_error(401)
        
        save = parse_meal_data(data)
        save["user"] = tg_user

        # Open the file in append mode ('a')
        with open("/menu/menu.data", 'a') as f:
            # Dump the dictionary as a JSON string into the file
            json.dump(save, f, ensure_ascii=False)
            # Write a newline character to separate each JSON object in the file
            f.write('\n')

        # If you want to send the data back as a response in pretty format
        self.write("Ваш выбор был успешно сохранён!")



async def create_server(config: Config):
    tornado.platform.asyncio.AsyncIOMainLoop().install()
    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler),
        (r"/menu", MenuHandler, {"token": config.telegram_token}),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server_port)

    return app
