import asyncio
from concurrent.futures import ThreadPoolExecutor
from functools import wraps
import mimetypes
import os
import tempfile
import PIL.Image as Image
from ..config import Config
from telegram import Message, Update, File
from telegram.ext import filters
from .base_plugin import PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.constants import ChatAction
import logging
import numpy as np
import multiprocessing

logger = logging.getLogger(__name__)
pool = ThreadPoolExecutor(max_workers=max(1,multiprocessing.cpu_count()-1))

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


class Avatar():
    base_app = None
    name = "avatar"
    config: Config

    def __init__(self, base_app):
        self.config = base_app.config
        self.base_app = base_app
        self.message_db = base_app.mongodb[self.config.mongo_db.messages_collection]
        self.user_db = base_app.users_collection
    
    def test_message(self, message: Update):
        if filters.PHOTO.check_update(message):
            return PRIORITY_BASIC, self.handle_photo
        if filters.Document.IMAGE.check_update(message):
            return PRIORITY_BASIC, self.handle_document
        return PRIORITY_NOT_ACCEPTING, None
    
    async def handle_photo(self, update):
        await update.send_chat_action(action=ChatAction.UPLOAD_PHOTO)
        document = update.update.message.photo[-1]
        file_name = f"{document.file_id}.jpg"
        photo_file = await document.get_file()
        await self.handle_stage2(update, photo_file, file_name)
    
    async def handle_document(self, update):
        await update.send_chat_action(action=ChatAction.UPLOAD_PHOTO)
        document = update.update.message.document
        photo_file = await document.get_file()
        file_ext = mimetypes.guess_extension(document.mime_type)
        file_name = f"{document.file_id}.{file_ext}"
        await self.handle_stage2(update, photo_file, file_name)

    async def handle_stage2(self, update, file, name):
        file_path = os.path.join(tempfile.gettempdir(), name)
        await self.user_db.update_one({
            "user_id": update.user,
            "bot_id": update.context.bot.id,
        }, {
            "$inc": {
                "avatars_called": 1,
            }
        })
        await file.download_to_drive(file_path)
        with Image.open(file_path) as img:
            resized_avatar = await resize_basic(img, self.config.photo.frame_size)
            with Image.open(self.config.photo.frame_file) as frame:
                resized_frame = await resize_basic(frame, self.config.photo.frame_size)

                final = await join_images(resized_avatar, resized_frame)

                final_name = f"{file_path}_framed.jpg"
                final.save(final_name, quality=self.config.photo.quality, optimize=True)
                await update.update.message.reply_document(final_name, filename="avatar.jpg")

