from typing import Any, Optional
import tornado.web
from tornado.httputil import HTTPServerRequest
import tornado.platform.asyncio
import os
from .config import Config
from .photo_task import get_by_uuid
import logging
import json
import re
import io
import PIL.Image as Image
from PIL import ImageOps
import math

logger = logging.getLogger(__name__)

real_frame_size = 2000

color_pattern = re.compile(r"^#(?:[0-9a-fA-F]{3}){1,2}$|^rgb\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*\)$|^rgba\(\s*\d+\s*,\s*\d+\s*,\s*\d+\s*,\s*[01](?:\.\d+)?\s*\)$", re.IGNORECASE)

def parse_theme_params(s):
    obj = json.loads(s)
    if not all(color_pattern.match(value) for value in obj.values() if isinstance(value, str)):
        logger.error("some colors are invalid")
        return {
            'bg_color': '#17212b',
            'button_color': '#5288c1',
            'button_text_color': '#ffffff',
            'hint_color': '#708499',
            'link_color': '#6ab3f3',
            'secondary_bg_color': '#232e3c',
            'text_color': '#f5f5f5'
        }
    return obj


class FitFrameHandler(tornado.web.RequestHandler):
    async def get(self):
        id_str = self.get_query_argument("id")
        tgWebApp = {
            "version": self.get_query_argument("tgWebAppVersion"),
            "platform": self.get_query_argument("tgWebAppPlatform"),
            "theme_params": parse_theme_params(self.get_query_argument("tgWebAppThemeParams", "{}"))
        }

        try:
            task = get_by_uuid(id_str)
            self.render(
                "fit_frame.html",
                task=task,
                id=id_str,
                tgWebApp=tgWebApp,
                real_frame_size=real_frame_size,
            )
        except (KeyError, ValueError):
            raise tornado.web.HTTPError(404)


def centered_crop_with_padding(img, x, y, size):
    # Calculate the center of the original image
    original_center_x = img.width / 2
    original_center_y = img.height / 2

    # Calculate the new center of the cropped image
    new_center_x = original_center_x + x
    new_center_y = original_center_y + y

    # Calculate the crop box (left, upper, right, lower)
    left = new_center_x - size / 2
    upper = new_center_y - size / 2
    right = left + size
    lower = upper + size

    # Pad the original image if necessary
    padding_left = max(-left, 0)
    padding_upper = max(-upper, 0)
    padding_right = max(right - img.width, 0)
    padding_lower = max(lower - img.height, 0)
    padding = (int(padding_left), int(padding_upper), int(padding_right), int(padding_lower))
    
    if any(side > 0 for side in padding):
        img = ImageOps.expand(img, padding, fill=(0,0,0,0)) # You can change the fill color if needed

    # Update crop box if padding was applied
    left += padding_left
    upper += padding_upper
    right += padding_left
    lower += padding_upper

    # Crop the image
    cropped_img = img.crop((int(left), int(upper), int(right), int(lower)))

    return cropped_img

def resize(img, scale_x, scale_y):
    return img.resize((int(scale_x * img.size[0]), int(scale_y * img.size[1])), resample=Image.LANCZOS)

def img_transform(img, a, b, c, d, e, f):
    scaling_x = math.sqrt(a * a + c * c)
    scaling_y = math.sqrt(b * b + d * d)

    rotation = math.atan2(b, a)

    img = resize(img, scaling_x, scaling_y)
    img = img.rotate(-rotation * 180 / math.pi, expand=True, fillcolor=(0,0,0,0))

    return centered_crop_with_padding(img, -e, -f, real_frame_size)

class CropFrameHandler(tornado.web.RequestHandler):
    async def get(self):
        id_str = self.get_query_argument('id')
        a = float(self.get_query_argument('a'))
        b = float(self.get_query_argument('b'))
        c = float(self.get_query_argument('c'))
        d = float(self.get_query_argument('d'))
        e = float(self.get_query_argument('e'))
        f = float(self.get_query_argument('f'))
        logger.info(f"params {a},{b},{c},{d},{e},{f}")

        # try:
        #     task = get_by_uuid(id_str)
        # except (KeyError, ValueError):
        #     raise tornado.web.HTTPError(404)

        # image_path = task.file
        image_path = f"./photos/{id_str}.jpg"

        # Open the image using PIL
        with Image.open(image_path) as img:
            if img.mode != "RGBA":
                img = img.convert("RGBA")
            cropped_img = img_transform(img, a,b,c,d,e,f)

            # Save the cropped image to a byte stream
            img_io = io.BytesIO()
            cropped_img.save(img_io, 'PNG')
            img_io.seek(0)

            img_contents = img_io.getvalue()

            # Send the cropped image in the response
            self.set_header('Content-Type', 'image/png')
            self.set_header('Content-Length', str(len(img_contents)))
            self.write(img_contents)
            self.finish()



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



async def create_server(config: Config):
    tornado.platform.asyncio.AsyncIOMainLoop().install()
    app = tornado.web.Application([
        (r"/fit_frame", FitFrameHandler),
        (r"/crop_frame", CropFrameHandler),
        (r"/photos/(.*)", PhotoHandler),
        (r"/static/(.*)", tornado.web.StaticFileHandler, {"path": "static/"}),
    ], template_path="templates/")
    app.listen(config.server_port)

    return app
