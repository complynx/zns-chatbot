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

        tg_user = json.loads(data["tg_user"][0])
        if not check_hmac(tg_user, self.token):
            return self.write_error(401)
        

        # If you want to send the data back as a response in pretty format
        self.write(json.dumps(data, indent=4, ensure_ascii=False) + " " + json.dumps(tg_user, indent=4, ensure_ascii=False))



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
