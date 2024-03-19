import asyncio
from concurrent.futures import ThreadPoolExecutor
from functools import wraps
import mimetypes
import os
import tempfile
import threading
import time
import cv2
import PIL.Image as Image
from ..config import Config, full_link
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
    joiner = Image.new('RGBA', a.size)
    joiner.alpha_composite(a)
    joiner.alpha_composite(b)

    return joiner.convert('RGB')

face_cascade = cv2.CascadeClassifier(cv2.data.haarcascades + 'haarcascade_frontalface_default.xml')

def rect_suface(rect):
    return rect[2]*rect[3]

def rect_to_bounds(rect):
    return (rect[0], rect[1], rect[0]+rect[2], rect[1]+rect[3])

def extend_to_square(rect, factor, boundary_rect):
    # Extract coordinates for the rectangle and the boundary
    left, top, right, bottom = rect
    boundary_left, boundary_top, boundary_right, boundary_bottom = boundary_rect
    
    # Calculate the dimensions of the original rectangle
    width = right - left
    height = bottom - top
    
    # Calculate the maximum dimension of the original rectangle
    max_dimension = max(width, height)
    
    # Calculate the size of the square after expansion
    expanded_size = max_dimension * factor
    
    # Determine the size of the square considering the boundary
    square_size = min(expanded_size, boundary_right - boundary_left, boundary_bottom - boundary_top)
    
    # Calculate the offsets to move the original rectangle to the center of the square
    horizontal_offset = (square_size - width) / 2
    vertical_offset = (square_size - height) / 2
    
    # Adjust the rectangle coordinates
    new_left = max(left - horizontal_offset, boundary_left)
    new_top = max(top - vertical_offset, boundary_top)
    new_right = min(right + horizontal_offset, boundary_right)
    new_bottom = min(bottom + vertical_offset, boundary_bottom)
    
    return (new_left, new_top, new_right, new_bottom)

@async_thread
def resize_faces(img: Image.Image, config: Config) -> Image.Image:
    import numpy as np
    numpy_image = np.array(img.convert("RGB"))
    faces = face_cascade.detectMultiScale(
        numpy_image,
        scaleFactor=1.1,
        minNeighbors=5,
        minSize=(config.photo.face_size_min, config.photo.face_size_min)
    )
    
    if len(faces)>0:
        faces_sorted = sorted(faces, key=rect_suface)
        logger.debug("faces: %s %s", faces, faces_sorted)
        faces_chosen = [faces_sorted[0]]
        min_size = rect_suface(faces_sorted[0])*config.photo.face_size_cut
        for face in faces_sorted[1:]:
            if min_size > rect_suface(face):
                break
            faces_chosen.append(face)
        l,t,r,b = rect_to_bounds(faces_chosen[0])
        for face in faces_chosen:
            fbounds = rect_to_bounds(face)
            if l > fbounds[0]:
                l = fbounds[0]
            if t > fbounds[1]:
                t = fbounds[1]
            if r < fbounds[2]:
                r = fbounds[2]
            if b < fbounds[3]:
                b = fbounds[3]
        size = config.photo.frame_size
        pw, ph = img.size
        sq = extend_to_square((l,t,r,b), config.photo.frame_expand, (0,0,pw,ph))
        
        cropped_img = img.crop(sq)
        return cropped_img.resize((size, size), resample=Image.LANCZOS)
    else:
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
    config: Config

    def __init__(self, base_app):
        super().__init__(base_app)
        self.config = base_app.config
        self.message_db = base_app.mongodb[self.config.mongo_db.messages_collection]
        self.user_db = base_app.users_collection
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
    
    async def handle_command(self, update):
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
        file = await self.base_app.bot.bot.get_file(file_id)
        await file.download_to_drive(file_path)
        return file_path
    
    async def handle_photo(self, update):
        await update.send_chat_action(action=ChatAction.UPLOAD_PHOTO)
        document = update.update.message.photo[-1]
        file_name = f"{document.file_id}.jpg"
        await self.handle_image_stage2(update, file_name)
    
    async def handle_document(self, update):
        await update.send_chat_action(action=ChatAction.UPLOAD_PHOTO)
        document = update.update.message.document
        file_ext = mimetypes.guess_extension(document.mime_type)
        file_name = f"{document.file_id}{file_ext}"
        await self.handle_image_stage2(update, file_name)

    async def handle_image_stage2(self, update, name):
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
                resized_frame = await resize_basic(frame, self.config.photo.frame_size)

                final = await join_images(resized_avatar, resized_frame)

                final_name = f"{file_path}_framed.jpg"
                final.save(final_name, quality=self.config.photo.quality, optimize=True)
                locale = update.update.effective_user.language_code
                await update.update.message.reply_document(
                    final_name,
                    filename="avatar.jpg",
                    reply_markup=InlineKeyboardMarkup([[
                        InlineKeyboardButton(
                            update.l("avatar-custom-crop"),
                            web_app=WebAppInfo(full_link(self.base_app, f"/fit_frame?file={name}&locale={locale}"))
                        )
                    ]]),
                )

