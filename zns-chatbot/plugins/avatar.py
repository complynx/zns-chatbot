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
from asyncio import Event, Lock, create_task, get_event_loop
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
from functools import wraps

import insightface
from bson.objectid import ObjectId
from huggingface_hub import hf_hub_download
from insightface.app.common import Face
from motor.core import AgnosticCollection
from PIL import Image, ImageChops
from telegram import InlineKeyboardButton, InlineKeyboardMarkup, Update, WebAppInfo
from telegram.constants import ChatAction, ParseMode
from telegram.ext import CallbackQueryHandler, CommandHandler, filters

from gfpgan import GFPGANer

from ..config import full_link
from ..tg_state import TGState
from .base_plugin import PRIORITY_BASIC, PRIORITY_NOT_ACCEPTING, BasePlugin

logger = logging.getLogger(__name__)
pool = ThreadPoolExecutor(max_workers=max(1, multiprocessing.cpu_count() - 1))
detector_model_name = "buffalo_l"
face_swap_repo = "ezioruan/inswapper_128.onnx"
face_swap_filename = "inswapper_128.onnx"
restoration_gan_repo = "gmk123/GFPGAN"
restoration_gan_filename = "GFPGANv1.4.pth"
models_cache_folder = "/cache"

FILES = {
    "leader": {
        "face_file": "frame/zns_2025_2_face.jpg",
        "frame_file": "frame/zns_2025_2_frame.jpg",
        "mask_file": "frame/zns_2025_2_mask.png",
    },
    "follower": {
        "face_file": "frame/zns_2025_2_face_f.jpg",
        "frame_file": "frame/zns_2025_2_frame_f.jpg",
        "mask_file": "frame/zns_2025_2_mask_f.png",
    },
    "simple": "frame/zns_2025_simple.png",
}


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


def expand_to_square(rect):
    x, y, w, h = rect
    max_side = max(w, h)
    expanded_x = x - (max_side - w) / 2.0
    expanded_y = y - (max_side - h) / 2.0
    return (expanded_x, expanded_y, max_side, max_side)


def expand(rect, scale):
    expanded_w = rect[2] * scale
    expanded_h = rect[3] * scale
    expanded_x = rect[0] - (expanded_w - rect[2]) / 2.0
    expanded_y = rect[1] - (expanded_h - rect[3]) / 2.0
    return (expanded_x, expanded_y, expanded_w, expanded_h)


def shift_square(square, shift):
    x, y, w, h = square
    shift_x, shift_y = shift
    shifted_x = x + w * shift_x
    shifted_y = y + h * shift_y
    return (shifted_x, shifted_y, w, h)


def shrink_to_boundaries(
    square,
    boundaries,
):
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
    logger.debug(
        "rect: %s | %s %s, bounds: %s, scale %f, shift %s",
        rect,
        r - l,
        b - t,
        boundaries,
        scale,
        shift,
    )
    square = expand_to_square((l, t, r - l, b - t))
    logger.debug("square: %s", square)
    expanded_square = expand(square, scale)
    logger.debug("expanded_square: %s", expanded_square)
    shifted_square = shift_square(expanded_square, shift)
    logger.debug("shifted_square: %s", shifted_square)
    shrunk_square = shrink_to_boundaries(shifted_square, boundaries)
    logger.debug("shrunk_square: %s", shrunk_square)
    return shrunk_square


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


models_preparing = Lock()
detector_model = None
swapper_model = None
refiner_model = None
models_ready = Event()


@async_thread
def _prepare_models_sync():
    global detector_model, swapper_model, refiner_model

    from os import makedirs, path

    insightface_cache = path.join(models_cache_folder, "insightface")
    hf_cache = path.join(models_cache_folder, "hf")
    makedirs(insightface_cache, exist_ok=True)
    makedirs(hf_cache, exist_ok=True)

    detector_model = insightface.app.FaceAnalysis(
        name=detector_model_name, root=insightface_cache
    )
    detector_model.prepare(ctx_id=0, det_size=(640, 640))
    fs = hf_hub_download(face_swap_repo, face_swap_filename, cache_dir=hf_cache)
    swapper_model = insightface.model_zoo.get_model(fs, download=False)
    gan = hf_hub_download(
        restoration_gan_repo, restoration_gan_filename, cache_dir=hf_cache
    )
    refiner_model = GFPGANer(
        model_path=gan,
        upscale=4,
        arch="clean",
        channel_multiplier=2,
        bg_upsampler=None,
    )


async def _prepare_models():
    if models_ready.is_set():
        return
    async with models_preparing:
        if models_ready.is_set():
            return
        try:
            start = time.time()
            logger.info("preparing models...")
            await _prepare_models_sync()
            models_ready.set()
            logger.info(f"models prepared, took: {time.time() - start} seconds")
        except Exception as e:
            logger.error("Failed to prepare models: %s", e, exc_info=1)


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
            pattern=f"^{self.name}\\\\|avatar_choice\\\\|.*",
        )
        create_task(_prepare_models())

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
        file_record = {
            "tg_file_id": document.file_id,
            "tg_file_unique_id": document.file_unique_id,
            "original_file_name": original_file_name,
            "mime_type": mime_type,
            "user_id": update.user,
            "bot_id": update.bot.id,
            "date_received": datetime.now(timezone.utc),
            "status": "received",
        }
        if hasattr(document, "file_size") and document.file_size:
            file_record["file_size"] = document.file_size

        result = await self.files_db.insert_one(file_record)
        return str(result.inserted_id)

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
        await self.prompt_avatar_method(update, db_id_str)

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
        await self.prompt_avatar_method(update, db_id_str)

    async def prompt_avatar_method(self, update: TGState, db_id_str: str):
        buttons = [
            [
                InlineKeyboardButton(
                    update.l("avatar-method-simple"),
                    callback_data=f"{self.name}|avatar_choice|{db_id_str}|simple",
                ),
                InlineKeyboardButton(
                    update.l("avatar-method-detailed"),
                    callback_data=f"{self.name}|avatar_choice|{db_id_str}|detailed",
                ),
            ],
            [
                InlineKeyboardButton(
                    update.l("avatar-cancel-button"),
                    callback_data=f"{self.name}|avatar_choice|{db_id_str}|cancel",
                )
            ],
        ]
        reply_markup = InlineKeyboardMarkup(buttons)
        await update.reply(
            update.l("avatar-choose-method"),
            reply_markup=reply_markup,
            parse_mode=ParseMode.HTML,
        )

    async def handle_avatar_callback_query(self, update: TGState):
        query = update.update.callback_query
        await query.answer()
        try:
            _, _, db_id_str, method = query.data.split("|", 3)
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

        try:
            object_id = ObjectId(db_id_str)
        except Exception as e:
            logger.error(
                f"Invalid ObjectId string in callback data: {db_id_str} - {e}",
                exc_info=True,
            )
            await query.edit_message_text(
                update.l("avatar-error-generic"), parse_mode=ParseMode.HTML
            )
            return

        file_doc = await self.files_db.find_one({"_id": object_id})

        if not file_doc:
            logger.error(f"File record not found in DB for _id: {db_id_str}")
            await query.edit_message_text(
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
            {"_id": object_id}, {"$set": {"status": f"processing_{method}"}}
        )

        if method == "simple":
            await self.handle_simple_avatar_flow(update, file_doc)
        elif method == "detailed":
            await self.handle_detailed_avatar_flow(update, file_doc)
        else:
            logger.error(f"Unknown avatar method: {method} from data: {query.data}")
            await query.edit_message_text(
                update.l("avatar-error-generic"), parse_mode=ParseMode.HTML
            )
            await self.files_db.update_one(
                {"_id": object_id}, {"$set": {"status": "error_unknown_method"}}
            )

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
                with Image.open(FILES["simple"]) as frame_img:
                    resized_user_img = await resize_basic(
                        user_img, self.config.photo.frame_size
                    )
                    resized_frame_img = await resize_basic(
                        frame_img, self.config.photo.frame_size
                    )
                    resized_frame_img = resized_frame_img.convert("RGBA")
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
        except FileNotFoundError as e:
            if str(e) == FILES["simple"]:
                logger.error(
                    f"Simple frame file not found: {FILES['simple']}", exc_info=True
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

    @async_thread
    def detect_face(self, img: Image.Image) -> Face:
        import numpy as np

        faces: list[Face] = detector_model.get(np.array(img.convert("RGB")))
        return max(
            faces,
            key=lambda face: (face.bbox[2] - face.bbox[0])
            * (face.bbox[3] - face.bbox[1]),
        )

    @async_thread
    def swap_faces(
        self, dst_img: Image.Image, src_face: Face, dst_face: Face
    ) -> Image.Image:
        import numpy as np

        swapped = swapper_model.get(
            np.array(dst_img.convert("RGB")), dst_face, src_face, paste_back=True
        )
        enhancement_result = refiner_model.enhance(
            swapped, has_aligned=False, only_center_face=False, paste_back=True
        )
        if isinstance(enhancement_result, tuple) and len(enhancement_result) == 3:
            restored = enhancement_result[2]
        else:
            restored = enhancement_result

        return Image.fromarray(restored)

    async def handle_detailed_avatar_flow(self, update: TGState, file_doc: dict):
        db_id = file_doc["_id"]

        original_name = file_doc.get("original_file_name", "unknown.dat")
        mime_type = file_doc.get("mime_type", "application/octet-stream")
        _, ext = os.path.splitext(original_name)
        if not ext:
            ext = mimetypes.guess_extension(mime_type) or ".dat"
        file_key_for_cache = f"{file_doc['tg_file_id']}{ext}"

        await self.user_db.update_one(
            {
                "user_id": update.user,
                "bot_id": update.context.bot.id,
            },
            {
                "$inc": {
                    "avatars_called": 1,
                    "avatars_detailed": 1,
                }
            },
        )
        user = await update.get_user()
        user_role = ""
        try:
            from .passes import PASS_RU

            if PASS_RU in user and "role" in user[PASS_RU]:
                user_role = user[PASS_RU]["role"]
        except ImportError:
            logger.warning(
                "Passes plugin not available for role determination in detailed avatar."
            )
        except KeyError:
            logger.warning(
                f"User object for {update.user} does not contain PASS_RU or role information."
            )

        if not user_role:
            await update.reply(
                text=update.l("avatar-no-role"),
                parse_mode=ParseMode.HTML,
                reply_to_message_id=update.message_id,
            )
            await self.files_db.update_one(
                {"_id": db_id}, {"$set": {"status": "error_no_role"}}
            )
            return

        files_config = FILES.get(user_role, None)
        if not files_config or not all(
            k in files_config for k in ["face_file", "frame_file", "mask_file"]
        ):
            logger.error(
                f"Configuration for role '{user_role}' for detailed avatar is missing or incomplete."
            )
            await update.reply(
                text=update.l("avatar-error-config-role"),
                parse_mode=ParseMode.HTML,
                reply_to_message_id=update.message_id,
            )
            await self.files_db.update_one(
                {"_id": db_id}, {"$set": {"status": "error_role_config_missing"}}
            )
            return

        try:
            downloaded_file_path = await self.get_file(file_key_for_cache)
            await self.files_db.update_one(
                {"_id": db_id},
                {
                    "$set": {
                        "status": "downloaded_detailed",
                        "cached_file_key": file_key_for_cache,
                    }
                },
            )

            if not models_ready.is_set():
                logger.info("Models aren't ready yet for detailed avatar...")
                await models_ready.wait()

            with Image.open(downloaded_file_path) as img:
                try:
                    src_face = await self.detect_face(img)
                except (IndexError, ValueError):
                    await update.reply(
                        text=update.l("avatar-no-face"),
                        parse_mode=ParseMode.HTML,
                        reply_to_message_id=update.message_id,
                    )
                    await self.files_db.update_one(
                        {"_id": db_id}, {"$set": {"status": "error_no_face_detected"}}
                    )
                    return

                with Image.open(files_config["face_file"]) as face:
                    swap_start = time.time()
                    face_resized = await resize_basic(
                        face, self.config.photo.frame_size
                    )
                    dst_face = await self.detect_face(face_resized)
                    swapped = await self.swap_faces(face_resized, src_face, dst_face)
                    swapped = await resize_basic(swapped, self.config.photo.frame_size)
                    logger.info(
                        f"Face swap for {update.user} (db_id: {db_id}) took: {time.time() - swap_start} seconds"
                    )
                    with Image.open(files_config["frame_file"]) as frame:
                        with Image.open(files_config["mask_file"]) as mask:
                            resized_frame = await resize_basic(
                                frame, self.config.photo.frame_size
                            )
                            resized_mask = await resize_basic(
                                mask, self.config.photo.frame_size
                            )
                            final = await join_images(
                                swapped, resized_mask, resized_frame
                            )
                            base_name_of_download, _ = os.path.splitext(
                                file_key_for_cache
                            )
                            final_local_path = os.path.join(
                                self.cache_dir,
                                f"{base_name_of_download}_detailed_framed.jpg",
                            )
                            final.save(
                                final_local_path,
                                quality=self.config.photo.quality,
                                optimize=True,
                            )
                            await update.bot.send_document(
                                chat_id=update.chat_id,
                                document=final_local_path,
                                filename="avatar_detailed.jpg",
                                reply_to_message_id=update.message_id,
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
                            await self.files_db.update_one(
                                {"_id": db_id},
                                {"$set": {"status": "completed_detailed"}},
                            )
        except FileNotFoundError as e:
            if (
                str(e) in files_config.values()
                or str(e) == FILES.get(user_role, {}).get("face_file")
                or str(e) == FILES.get(user_role, {}).get("frame_file")
                or str(e) == FILES.get(user_role, {}).get("mask_file")
            ):
                logger.error(
                    f"Frame/mask/face file not found for role {user_role}: {e}",
                    exc_info=True,
                )
                await update.reply(
                    text=update.l("avatar-error-config-role"),
                    parse_mode=ParseMode.HTML,
                    reply_to_message_id=update.message_id,
                )
                await self.files_db.update_one(
                    {"_id": db_id},
                    {"$set": {"status": "error_detailed_frame_files_missing"}},
                )
            else:
                logger.error(
                    f"User image file expected at {downloaded_file_path} for db_id {db_id} not found: {e}",
                    exc_info=True,
                )
                await update.reply(
                    text=update.l("avatar-error-generic"),
                    parse_mode=ParseMode.HTML,
                    reply_to_message_id=update.message_id,
                )
                await self.files_db.update_one(
                    {"_id": db_id},
                    {"$set": {"status": "error_user_file_missing_post_dl_detailed"}},
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
                {"_id": db_id},
                {"$set": {"status": "error_get_file_value_error_detailed"}},
            )
        except Exception as e:
            logger.error(
                f"Error in detailed avatar flow for {update.user} (db_id: {db_id}): {e}",
                exc_info=True,
            )
            await update.reply(
                text=update.l("avatar-error-generic"),
                parse_mode=ParseMode.HTML,
                reply_to_message_id=update.message_id,
            )
            await self.files_db.update_one(
                {"_id": db_id}, {"$set": {"status": "error_detailed_processing"}}
            )


# if __name__ == "__main__":
# from asyncio import run
# run(_prepare_models())
