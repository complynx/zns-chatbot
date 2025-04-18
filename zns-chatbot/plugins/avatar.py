from ._fix_torch import functional_tensor as _  # noqa: F401
from concurrent.futures import ThreadPoolExecutor
from functools import wraps
import mimetypes
import os
import tempfile
import threading
import time
import insightface
from insightface.app.common import Face
from gfpgan import GFPGANer
from motor.core import AgnosticCollection
from huggingface_hub import hf_hub_download
from PIL import Image, ImageChops
from asyncio import Event, Lock, create_task, get_event_loop
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
}


file_cache_timeout = 2*60*60 # 2 hours

def async_thread(func):
    @wraps(func)
    def wrapper(*args, **kwargs):
        return get_event_loop().run_in_executor(pool, lambda: func(*args, **kwargs))
    return wrapper

@async_thread
def join_images(a: Image.Image, b: Image.Image, c: Image.Image) -> Image.Image:
    a = a.convert('RGBA')
    b = b.convert('RGBA')
    c = c.convert('RGBA')
    joiner = Image.new('RGBA', a.size)
    joiner.alpha_composite(a)
    joiner.alpha_composite(b)
    joiner = ImageChops.screen(joiner, c)

    return joiner.convert('RGB')

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

@async_thread
def resize_basic(img: Image.Image, size: int) -> Image.Image:
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

    detector_model = insightface.app.FaceAnalysis(name=detector_model_name, root=insightface_cache)
    detector_model.prepare(ctx_id=0, det_size=(640, 640))
    fs = hf_hub_download(face_swap_repo, face_swap_filename, cache_dir=hf_cache)
    swapper_model = insightface.model_zoo.get_model(fs, download=False)
    gan = hf_hub_download(restoration_gan_repo, restoration_gan_filename, cache_dir=hf_cache)
    refiner_model = GFPGANer(
        model_path=gan,
        upscale=4,
        arch='clean',
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
        self.cache_dir = tempfile.gettempdir()
        sweep_folder(self.cache_dir)
        self.base_app.avatar = self
        self._command_test = CommandHandler("avatar", self.handle_command)
        create_task(_prepare_models())
    
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
        try:
            await update.send_chat_action(action=ChatAction.TYPING)
        except Exception as e:
            logger.error(f"send chat action exception {e}", exc_info=e)
        document = update.update.message.photo[-1]
        file_name = f"{document.file_id}.jpg"
        await self.handle_image_stage2(update, file_name)
    
    async def handle_document(self, update: TGState):
        try:
            await update.send_chat_action(action=ChatAction.TYPING)
        except Exception as e:
            logger.error(f"send chat action exception {e}", exc_info=e)
        document = update.update.message.document
        file_ext = mimetypes.guess_extension(document.mime_type)
        file_name = f"{document.file_id}{file_ext}"
        await self.handle_image_stage2(update, file_name)
    
    @async_thread
    def detect_face(self, img: Image.Image) -> Face:
        import numpy as np
        faces: list[Face] = detector_model.get(np.array(img.convert("RGB")))
        return max(faces, key=lambda face: (face.bbox[2] - face.bbox[0]) * (face.bbox[3] - face.bbox[1]))
    
    @async_thread
    def swap_faces(self, dst_img: Image.Image, src_face: Face, dst_face: Face) -> Image.Image:
        import numpy as np
        swapped = swapper_model.get(np.array(dst_img.convert("RGB")), dst_face, src_face, paste_back=True)
        _, _, restored = refiner_model.enhance(swapped, has_aligned=False, only_center_face=False, paste_back=True)
        return Image.fromarray(restored)

    async def handle_image_stage2(self, update: TGState, name):
        tgUpdate = update.update  # type: Update
        # return await update.reply(update.l("avatar-processing"), parse_mode=ParseMode.HTML)
        await self.user_db.update_one({
            "user_id": update.user,
            "bot_id": update.context.bot.id,
        }, {
            "$inc": {
                "avatars_called": 1,
            }
        })
        user = await update.get_user()
        user_role = ""
        from .passes import PASS_RU
        if PASS_RU in user and "role" in user[PASS_RU]:
            user_role = user[PASS_RU]["role"]
        if user_role == "":
            return await update.reply(update.l("avatar-no-role"), parse_mode=ParseMode.HTML)
        files = FILES.get(user_role, None)
        file_path = await self.get_file(name)
        try:
            await update.reply(update.l("avatar-processing"), parse_mode=ParseMode.HTML)
        except Exception as e:
            logger.error(f"send processing notification exception {e}", exc_info=e)
        if not models_ready.is_set():
            logger.info("models aren't ready yet...")
            await models_ready.wait()
        with Image.open(file_path) as img:
            try:
                src_face = await self.detect_face(img)
            except (IndexError, ValueError):
                return await update.reply(update.l("avatar-no-face"), parse_mode=ParseMode.HTML)

            with Image.open(files["face_file"]) as face:
                swap_start = time.time()
                
                face_resized = await resize_basic(face, self.config.photo.frame_size)
                dst_face = await self.detect_face(face_resized)

                swapped = await self.swap_faces(face_resized, src_face, dst_face)
                swapped = await resize_basic(swapped, self.config.photo.frame_size)
                logger.info(f"face swap for {update.user} took: {time.time() - swap_start} seconds")
                
                with Image.open(files["frame_file"]) as frame:
                    with Image.open(files["mask_file"]) as mask:
                        resized_frame = await resize_basic(frame, self.config.photo.frame_size)
                        resized_mask = await resize_basic(mask, self.config.photo.frame_size)
                        final = await join_images(swapped, resized_mask, resized_frame)

                        final_name = f"{file_path}_framed.jpg"
                        final.save(final_name, quality=self.config.photo.quality, optimize=True)
                        # locale = tgUpdate.effective_user.language_code
                        await tgUpdate.message.reply_document(
                            final_name,
                            filename="avatar.jpg",
                            # reply_markup=InlineKeyboardMarkup([[
                            #     InlineKeyboardButton(
                            #         update.l("avatar-custom-crop"),
                            #         web_app=WebAppInfo(full_link(self.base_app, f"/fit_frame?file={name}&locale={locale}"))
                            #     )
                            # ]]),
                        )
                        if os.path.isfile(self.config.photo.cover_file):
                            await tgUpdate.message.reply_document(
                                self.config.photo.cover_file,
                                filename="cover.jpg",
                                caption=update.l("cover-caption-message")
                            )


# if __name__ == "__main__":
    # from asyncio import run
    # run(_prepare_models())
