import asyncio
from concurrent.futures import ThreadPoolExecutor
from functools import wraps
import mimetypes
import os
import tempfile
import threading
import time
import dlib
from motor.core import AgnosticCollection
from PIL import Image, ImageChops
from ..config import Config, full_link
from ..tg_state import TGState
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo
from telegram.ext import filters, CommandHandler
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.constants import ChatAction, ParseMode
import logging
import multiprocessing

logger = logging.getLogger(__name__)
pool = ThreadPoolExecutor(max_workers=max(1,multiprocessing.cpu_count()-1))

file_cache_timeout = 2*60*60 # 2 hours

def async_thread(func):
    @wraps(func)
    def wrapper(*args, **kwargs):
        return asyncio.get_event_loop().run_in_executor(pool, lambda: func(*args, **kwargs))
    return wrapper

@async_thread
def join_images(a, b):
    a = a.convert('RGBA')
    b = b.convert('RGBA')
    # c = c.convert('RGBA')
    joiner = Image.new('RGBA', a.size)
    joiner.alpha_composite(a)
    joiner.alpha_composite(b)
    # joiner = ImageChops.screen(joiner, c)

    return joiner.convert('RGB')

face_cascade = dlib.get_frontal_face_detector()

def expand_to_square(rect):
    x, y, w, h = rect
    max_side = max(w, h)
    expanded_x = x - (max_side - w) / 2.
    expanded_y = y - (max_side - h) / 2.
    return (expanded_x, expanded_y, max_side, max_side)

def expand(rect, scale):
    expanded_w = rect[2] * scale
    expanded_h = rect[3] * scale
    expanded_x = rect[0] - (expanded_w - rect[2]) / 2.
    expanded_y = rect[1] - (expanded_h - rect[3]) / 2.
    return (expanded_x, expanded_y, expanded_w, expanded_h)

def shift_square(square, shift):
    x, y, w, h = square
    shift_x, shift_y = shift
    shifted_x = x + w * shift_x
    shifted_y = y + h * shift_y
    return (shifted_x, shifted_y, w, h)

def shrink_to_boundaries(square, boundaries,):
    x, y, w, h = square
    boundary_x, boundary_y, boundary_w, boundary_h = boundaries
    
    # Calculate the maximum possible size that still fits within the boundaries
    max_size = min(boundary_w - max(0, x), boundary_h - max(0, y))
    
    # Adjust the width and height while maintaining the square shape
    new_size = min(w, h, max_size)
    
    # Adjust the position of the square if necessary to keep it within the boundaries
    new_x = max(min(x, boundary_x + boundary_w - new_size), boundary_x)
    new_y = max(min(y, boundary_y + boundary_h - new_size), boundary_y)
    
    return (int(new_x), int(new_y), int(new_size + new_x), int(new_size + new_y))

def create_scaled_square(rect, boundaries, scale, shift):
    l = rect.left()
    t = rect.top()
    r = rect.right()
    b = rect.bottom()
    logger.debug("rect: %s | %s %s, bounds: %s, scale %f, shift %s", rect, r-l, b-t, boundaries, scale, shift)
    square = expand_to_square((l,t,r-l,b-t))
    logger.debug("square: %s", square)
    expanded_square = expand(square, scale)
    logger.debug("expanded_square: %s", expanded_square)
    shifted_square = shift_square(expanded_square, shift)
    logger.debug("shifted_square: %s", shifted_square)
    shrunk_square = shrink_to_boundaries(shifted_square, boundaries)
    logger.debug("shrunk_square: %s", shrunk_square)
    return shrunk_square

def rectProd(rect):
    l = rect.left()
    t = rect.top()
    r = rect.right()
    b = rect.bottom()
    return (r-l)*(b-t)

@async_thread
def resize_faces(img: Image.Image, config: Config) -> Image.Image:
    import numpy as np
    numpy_image = np.array(img.convert("RGB"))
    faces = face_cascade(
        numpy_image,1
    )
    
    if len(faces)>0:
        logger.debug("faces: %s", faces)
        faces_sorted = sorted(faces, key=rectProd)
        logger.debug("sorted: %s", faces_sorted)
        size = config.photo.frame_size
        pw, ph = img.size
        sq = create_scaled_square(faces_sorted[0], (0,0,pw,ph), config.photo.face_expand,
                                  (config.photo.face_offset_x, config.photo.face_offset_y))
        
        cropped_img = img.crop(sq)
        return cropped_img.resize((size, size), resample=Image.LANCZOS)
    else:
        logger.debug("no faces")
        size = config.photo.frame_size
        pw, ph = img.size
        left = right = top = bottom = 0
        if pw < ph:
            top = (ph-pw)//2
            bottom = top+pw
            right = pw
        else:
            left = (pw-ph)//2
            right = left + ph
            bottom = ph
        cropped_img = img.crop((left,top,right,bottom))
        return cropped_img.resize((size, size), resample=Image.LANCZOS)

@async_thread
def resize_basic(img, size):
    pw, ph = img.size
    left = right = top = bottom = 0
    if pw < ph:
        top = (ph-pw)//2
        bottom = top+pw
        right = pw
    else:
        left = (pw-ph)//2
        right = left + ph
        bottom = ph
    cropped_img = img.crop((left,top,right,bottom))
    return cropped_img.resize((size, size), resample=Image.LANCZOS)


def sweep_folder(folder_path):
    def sweep():
        while True:
            try:
                current_time = time.time()

                for filename in os.listdir(folder_path):
                    file_path = os.path.join(folder_path, filename)

                    if os.path.isfile(file_path) and current_time - os.path.getmtime(file_path) > file_cache_timeout:
                        os.remove(file_path)

            except Exception as e:
                logger.error("Sweeper error: %s", e, exc_info=1)
            time.sleep(file_cache_timeout)

    # Create a daemon thread to run the sweep function
    thread = threading.Thread(target=sweep)
    thread.daemon = True
    thread.start()

def touch_file(file_path):
    current_time = time.time()
    os.utime(file_path, times=(current_time, current_time))


class Avatar(BasePlugin):
    name = "avatar"

    def __init__(self, base_app):
        super().__init__(base_app)
        self.user_db: AgnosticCollection = base_app.users_collection
        self.cache_dir = tempfile.gettempdir()
        sweep_folder(self.cache_dir)
        self.base_app.avatar = self
        self._command_test = CommandHandler("avatar", self.handle_command)
    
    def test_message(self, message: Update, state, web_app_data):
        if filters.PHOTO.check_update(message):
            return PRIORITY_BASIC, self.handle_photo
        if filters.Document.IMAGE.check_update(message):
            return PRIORITY_BASIC, self.handle_document
        if self._command_test.check_update(message):
            return PRIORITY_BASIC, self.handle_command
        return PRIORITY_NOT_ACCEPTING, None
    
    async def handle_command(self, update: TGState):
        await update.reply(update.l("avatar-without-command"), parse_mode=ParseMode.MARKDOWN)
    
    async def get_file(self, file_name):
        file_path = os.path.join(self.cache_dir, file_name)
        try:
            if os.path.isfile(file_path):
                touch_file(file_path)
                return file_path
        except Exception as e:
            logger.warn("Failed to touch file: %s", e, exc_info=1)
        file_id, _ = os.path.splitext(file_name)
        logger.debug(f"file id: {file_id}")
        file = await self.bot.get_file(file_id)
        await file.download_to_drive(file_path)
        return file_path
    
    async def handle_photo(self, update: TGState):
        await update.send_chat_action(action=ChatAction.UPLOAD_PHOTO)
        document = update.update.message.photo[-1]
        file_name = f"{document.file_id}.jpg"
        await self.handle_image_stage2(update, file_name)
    
    async def handle_document(self, update: TGState):
        await update.send_chat_action(action=ChatAction.UPLOAD_PHOTO)
        document = update.update.message.document
        file_ext = mimetypes.guess_extension(document.mime_type)
        file_name = f"{document.file_id}{file_ext}"
        await self.handle_image_stage2(update, file_name)

    async def handle_image_stage2(self, update: TGState, name):
        tgUpdate = update.update  # type: Update
        await self.user_db.update_one({
            "user_id": update.user,
            "bot_id": update.context.bot.id,
        }, {
            "$inc": {
                "avatars_called": 1,
            }
        })
        file_path = await self.get_file(name)
        with Image.open(file_path) as img:
            resized_avatar = await resize_faces(img, self.config)
            with Image.open(self.config.photo.frame_file) as frame:
                # with Image.open(self.config.photo.flare_file) as flare:
                resized_frame = await resize_basic(frame, self.config.photo.frame_size)
                # resized_flare = await resize_basic(flare, self.config.photo.frame_size)

                final = await join_images(resized_avatar, resized_frame)

                final_name = f"{file_path}_framed.jpg"
                final.save(final_name, quality=self.config.photo.quality, optimize=True)
                locale = tgUpdate.effective_user.language_code
                await tgUpdate.message.reply_document(
                    final_name,
                    filename="avatar.jpg",
                    reply_markup=InlineKeyboardMarkup([[
                        InlineKeyboardButton(
                            update.l("avatar-custom-crop"),
                            web_app=WebAppInfo(full_link(self.base_app, f"/fit_frame?file={name}&locale={locale}"))
                        )
                    ]]),
                )
                if os.path.isfile(self.config.photo.cover_file):
                    await tgUpdate.message.reply_document(
                        self.config.photo.cover_file,
                        filename="cover.jpg",
                        caption=update.l("cover-caption-message")
                    )

