import asyncio
import base64
from concurrent.futures import ThreadPoolExecutor
from functools import wraps
import hashlib
import io
import mimetypes
import os
import tempfile
import threading
import time
import PIL.Image as Image
from ..config import Config, full_link
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Message, Update, File, WebAppInfo
from telegram.ext import filters, CommandHandler
from .base_plugin import BasePlugin, PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING
from telegram.constants import ChatAction
import logging
import numpy as np
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
        await update.l("avatar-without-command")
    
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
            resized_avatar = await resize_basic(img, self.config.photo.frame_size)
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

