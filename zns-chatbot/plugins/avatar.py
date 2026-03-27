# fmt: off
from ._fix_torch import functional_tensor as _  # noqa: F401 # isort: skip
# fmt: on

import logging
import mimetypes
import multiprocessing
import os
import tempfile
import threading
import time
from asyncio import get_event_loop
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from functools import wraps

from bson.objectid import ObjectId
from motor.core import AgnosticCollection
from PIL import Image, ImageChops
from pymongo import ReturnDocument
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo
from telegram.constants import ChatAction, ParseMode
from telegram.ext import CallbackQueryHandler, CommandHandler, filters

from ..config import full_link
from ..tg_state import TGState
from .base_plugin import PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING, BasePlugin

logger = logging.getLogger(__name__)
pool = ThreadPoolExecutor(max_workers=max(1, multiprocessing.cpu_count() - 1))
SIMPLE_FRAME_FILE = "frame/zns_frame.png"


file_cache_timeout = 2 * 60 * 60  # 2 hours


def async_thread(func):
    @wraps(func)
    def wrapper(*args, **kwargs):
        return get_event_loop().run_in_executor(pool, lambda: func(*args, **kwargs))

    return wrapper


@async_thread
def join_images(
    a: Image.Image, b: Image.Image, c: Image.Image | None = None
) -> Image.Image:
    a = a.convert("RGBA")
    b = b.convert("RGBA")
    if c is not None:
        c = c.convert("RGBA")
    joiner = Image.new("RGBA", a.size)
    joiner.alpha_composite(a)
    joiner.alpha_composite(b)
    if c is not None:
        joiner = ImageChops.screen(joiner, c)

    return joiner.convert("RGB")

@async_thread
def resize_basic(img: Image.Image, size: int) -> Image.Image:
    pw, ph = img.size
    left = right = top = bottom = 0
    if pw < ph:
        top = (ph - pw) // 2
        bottom = top + pw
        right = pw
    else:
        left = (pw - ph) // 2
        right = left + ph
        bottom = ph
    cropped_img = img.crop((left, top, right, bottom))
    return cropped_img.resize((size, size), resample=Image.LANCZOS)


def sweep_folder(folder_path):
    def sweep():
        while True:
            try:
                current_time = time.time()

                for filename in os.listdir(folder_path):
                    file_path = os.path.join(folder_path, filename)

                    if (
                        os.path.isfile(file_path)
                        and current_time - os.path.getmtime(file_path)
                        > file_cache_timeout
                    ):
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
        self.files_db: AgnosticCollection = base_app.mongodb[
            self.config.mongo_db.files_collection
        ]
        self.cache_dir = tempfile.gettempdir()
        sweep_folder(self.cache_dir)
        self.base_app.avatar = self
        self._command_test = CommandHandler("avatar", self.handle_command)
        self._cq_handler = CallbackQueryHandler(
            self.handle_avatar_callback_query,
            pattern=f"^{self.name}\\|.*",
        )

    def test_message(self, message: Update, state, web_app_data):
        if filters.PHOTO.check_update(message):
            return PRIORITY_BASIC, self.handle_photo
        if filters.Document.IMAGE.check_update(message):
            return PRIORITY_BASIC, self.handle_document
        if self._command_test.check_update(message):
            return PRIORITY_BASIC, self.handle_command
        return PRIORITY_NOT_ACCEPTING, None

    def test_callback_query(self, update_obj: Update, state):
        if hasattr(super(), "test_callback_query"):
            prio, cb = super().test_callback_query(update_obj, state)
            if cb:
                return prio, cb
        if self._cq_handler.check_update(update_obj):
            return PRIORITY_BASIC, self.handle_avatar_callback_query
        return PRIORITY_NOT_ACCEPTING, None

    async def handle_command(self, update: TGState):
        await update.reply(
            update.l("avatar-without-command"), parse_mode=ParseMode.MARKDOWN
        )

    async def get_file(self, file_key_with_tg_id: str):
        """Downloads a file from Telegram given a key of format 'tg_file_id.ext' and caches it.
        The file is saved in self.cache_dir using file_key_with_tg_id as its name.
        """
        cache_file_path = os.path.join(self.cache_dir, file_key_with_tg_id)
        try:
            if os.path.isfile(cache_file_path):
                touch_file(cache_file_path)
                logger.debug(
                    f"File {file_key_with_tg_id} found in cache and touched: {cache_file_path}"
                )
                return cache_file_path
        except Exception as e:
            logger.warn(f"Failed to touch file {cache_file_path}: {e}", exc_info=True)

        actual_tg_file_id, _ = os.path.splitext(file_key_with_tg_id)
        if not actual_tg_file_id:
            logger.error(
                f"Could not extract actual_tg_file_id from file_key_with_tg_id: {file_key_with_tg_id}"
            )
            raise ValueError(
                f"Invalid file_key_with_tg_id format: {file_key_with_tg_id}. Expected 'tg_file_id.ext'."
            )

        logger.debug(
            f"Downloading file with actual_tg_file_id: {actual_tg_file_id} to cache: {cache_file_path}"
        )
        try:
            file_to_download = await self.bot.get_file(actual_tg_file_id)
            await file_to_download.download_to_drive(cache_file_path)
            logger.debug(
                f"Successfully downloaded {actual_tg_file_id} to {cache_file_path}"
            )
            return cache_file_path
        except Exception as e:
            logger.error(
                f"Failed to download file {actual_tg_file_id} to {cache_file_path}: {e}",
                exc_info=True,
            )
            raise  # Re-raise after logging

    async def _store_file_metadata(
        self, update: TGState, document, original_file_name: str, mime_type: str
    ) -> str:
        query = {
            "tg_file_unique_id": document.file_unique_id,
            "user_id": update.user,
            "bot_id": update.bot.id,
        }

        set_on_insert_fields = {
            "tg_file_id": document.file_id,
            "original_file_name": original_file_name,
            "mime_type": mime_type,
            "user_id": update.user,
            "bot_id": update.bot.id,
            "date_received": datetime.now(timezone.utc),
            "status": "received",
        }
        if hasattr(document, "file_size") and document.file_size:
            set_on_insert_fields["file_size"] = document.file_size

        update_payload = {"$setOnInsert": set_on_insert_fields}

        stored_document = await self.files_db.find_one_and_update(
            query, update_payload, upsert=True, return_document=ReturnDocument.AFTER
        )

        if stored_document:
            logger.info(
                f"Ensured file record (ID: {stored_document['_id']}) for "
                f"tg_file_unique_id: {document.file_unique_id}, user: {update.user}, bot: {update.bot.id}"
            )
            return str(stored_document["_id"])
        else:
            log_message = (
                f"CRITICAL: find_one_and_update with upsert=True and ReturnDocument.AFTER returned None. "
                f"Query: {query}"
            )
            logger.critical(log_message)
            raise RuntimeError(
                f"Upsert operation failed to return a document for tg_file_unique_id: {document.file_unique_id}"
            )

    async def handle_photo(self, update: TGState):
        try:
            await update.send_chat_action(action=ChatAction.TYPING)
        except Exception as e:
            logger.error(f"send chat action exception {e}", exc_info=e)

        tg_message = update.update.message
        document = tg_message.photo[-1]
        original_file_name = f"{document.file_unique_id}.jpg"
        mime_type = "image/jpeg"

        db_id_str = await self._store_file_metadata(
            update, document, original_file_name, mime_type
        )
        await self.process_avatar(update, db_id_str)

    async def handle_document(self, update: TGState):
        try:
            await update.send_chat_action(action=ChatAction.TYPING)
        except Exception as e:
            logger.error(f"send chat action exception {e}", exc_info=e)

        tg_message = update.update.message
        document = tg_message.document
        mime_type = document.mime_type or "application/octet-stream"
        original_file_name = (
            document.file_name
            or f"{document.file_unique_id}{mimetypes.guess_extension(mime_type) or '.dat'}"
        )

        db_id_str = await self._store_file_metadata(
            update, document, original_file_name, mime_type
        )
        await self.process_avatar(update, db_id_str)

    async def handle_avatar_callback_query(self, update: TGState):
        query = update.update.callback_query
        await query.answer()
        try:
            _, db_id_str, method = query.data.split("|", 2)
        except ValueError:
            logger.error(f"Invalid callback data structure: {query.data}")
            await query.edit_message_text(
                update.l("avatar-error-generic"), parse_mode=ParseMode.HTML
            )
            return

        if method == "cancel":
            await query.edit_message_text(
                update.l("avatar-cancelled-message"),
                reply_markup=None,
                parse_mode=ParseMode.HTML,
            )
            if ObjectId.is_valid(db_id_str):
                await self.files_db.update_one(
                    {"_id": ObjectId(db_id_str)},
                    {"$set": {"status": "cancelled_by_user"}},
            )
            return

        await query.edit_message_text(
            text=update.l("avatar-processing-initial"),
            reply_markup=None,
            parse_mode=ParseMode.HTML,
        )
        await self.process_avatar(update, db_id_str)

    async def process_avatar(self, update: TGState, db_id_str: str):
        try:
            object_id = ObjectId(db_id_str)
        except Exception as e:
            logger.error(
                f"Invalid ObjectId string in callback data: {db_id_str} - {e}",
                exc_info=True,
            )
            await update.edit_or_reply(
                update.l("avatar-error-generic"), parse_mode=ParseMode.HTML
            )
            return

        file_doc = await self.files_db.find_one({"_id": object_id})

        if not file_doc:
            logger.error(f"File record not found in DB for _id: {db_id_str}")
            await update.edit_or_reply(
                update.l("avatar-error-file-expired-or-missing"),
                parse_mode=ParseMode.HTML,
            )
            if ObjectId.is_valid(db_id_str):
                await self.files_db.update_one(
                    {"_id": object_id},
                    {"$set": {"status": "error_not_found_on_callback"}},
                )
            return

        await self.files_db.update_one(
            {"_id": object_id}, {"$set": {"status": "processing_simple"}}
        )
        await self.handle_simple_avatar_flow(update, file_doc)

    async def handle_simple_avatar_flow(self, update: TGState, file_doc: dict):
        db_id = file_doc["_id"]

        original_name = file_doc.get("original_file_name", "unknown.dat")
        mime_type = file_doc.get("mime_type", "application/octet-stream")
        _, ext = os.path.splitext(original_name)
        if not ext:
            ext = mimetypes.guess_extension(mime_type) or ".dat"
        file_key_for_cache_and_web_app = f"{file_doc['tg_file_id']}{ext}"

        await self.user_db.update_one(
            {"user_id": update.user, "bot_id": update.context.bot.id},
            {"$inc": {"avatars_called": 1, "avatars_simple": 1}},
        )

        try:
            downloaded_file_path = await self.get_file(file_key_for_cache_and_web_app)
            await self.files_db.update_one(
                {"_id": db_id},
                {
                    "$set": {
                        "status": "downloaded_simple",
                        "cached_file_key": file_key_for_cache_and_web_app,
                    }
                },
            )

            with Image.open(downloaded_file_path) as user_img:
                with Image.open(SIMPLE_FRAME_FILE) as frame_img:
                    resized_user_img = await resize_basic(
                        user_img, self.config.photo.frame_size
                    )
                    resized_frame_img = await resize_basic(
                        frame_img, self.config.photo.frame_size
                    )
                    final_image = await join_images(
                        resized_user_img, resized_frame_img, None
                    )

                    base_name_of_download, _ = os.path.splitext(
                        file_key_for_cache_and_web_app
                    )
                    final_local_path = os.path.join(
                        self.cache_dir, f"{base_name_of_download}_simple_framed.jpg"
                    )

                    final_image.save(
                        final_local_path,
                        quality=self.config.photo.quality,
                        optimize=True,
                        keep_rgb=True,
                    )

                    locale = (
                        update.update.effective_user.language_code
                        if update.update.effective_user
                        else "en"
                    )
                    web_app_relative_path = f"/fit_frame?file={file_key_for_cache_and_web_app}&locale={locale}"
                    full_web_app_url = full_link(self.base_app, web_app_relative_path)

                    custom_framing_button = InlineKeyboardButton(
                        update.l("avatar-custom-crop"),
                        web_app=WebAppInfo(url=full_web_app_url),
                    )
                    reply_markup = InlineKeyboardMarkup([[custom_framing_button]])

                    await update.bot.send_document(
                        chat_id=update.chat_id,
                        document=final_local_path,
                        filename="avatar_simple.jpg",
                        caption=update.l("avatar-simple-caption"),
                        reply_markup=reply_markup,
                        parse_mode=ParseMode.HTML,
                        reply_to_message_id=update.message_id,
                    )
                    await self.files_db.update_one(
                        {"_id": db_id}, {"$set": {"status": "completed_simple"}}
                    )
                    if os.path.isfile(self.config.photo.cover_file):
                        await update.bot.send_document(
                            chat_id=update.chat_id,
                            document=self.config.photo.cover_file,
                            filename="cover.jpg",
                            caption=update.l("cover-caption-message"),
                            parse_mode=ParseMode.HTML,
                            reply_to_message_id=update.message_id,
                        )
        except FileNotFoundError as e:
            if str(e) == SIMPLE_FRAME_FILE:
                logger.error(
                    f"Simple frame file not found: {SIMPLE_FRAME_FILE}", exc_info=True
                )
                await update.reply(
                    text=update.l("avatar-error-frame-missing"),
                    parse_mode=ParseMode.HTML,
                    reply_to_message_id=update.message_id,
                )
                await self.files_db.update_one(
                    {"_id": db_id}, {"$set": {"status": "error_frame_missing"}}
                )
            else:
                logger.error(
                    f"User image file expected at {downloaded_file_path} for db_id {db_id} not found after get_file call: {e}",
                    exc_info=True,
                )
                await update.reply(
                    text=update.l("avatar-error-generic"),
                    parse_mode=ParseMode.HTML,
                    reply_to_message_id=update.message_id,
                )
                await self.files_db.update_one(
                    {"_id": db_id},
                    {"$set": {"status": "error_user_file_missing_post_dl"}},
                )
        except ValueError as e:
            logger.error(
                f"ValueError during get_file for db_id {db_id}: {e}", exc_info=True
            )
            await update.reply(
                text=update.l("avatar-error-generic"),
                parse_mode=ParseMode.HTML,
                reply_to_message_id=update.message_id,
            )
            await self.files_db.update_one(
                {"_id": db_id}, {"$set": {"status": "error_get_file_value_error"}}
            )
        except Exception as e:
            logger.error(
                f"Error in simple avatar flow for db_id {db_id}: {e}", exc_info=True
            )
            await update.reply(
                text=update.l("avatar-error-generic"),
                parse_mode=ParseMode.HTML,
                reply_to_message_id=update.message_id,
            )
            await self.files_db.update_one(
                {"_id": db_id}, {"$set": {"status": "error_simple_processing"}}
            )
