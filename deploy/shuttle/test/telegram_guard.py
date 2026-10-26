"""Owned-stand Telegram egress: only configured accounts can receive messages."""
import http.client
import json
import os
import re
import ssl
import time
import uuid
import urllib.error
import urllib.request
from urllib.parse import urlsplit
from email import policy
from email.parser import BytesParser
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MAX_REQUEST = 1 << 20
MAX_FILE = 32 << 20
MAX_DOCUMENT_REQUEST = 5 << 20


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError("Duplicate JSON key")
        result[key] = value
    return result


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_):
        return None


class GuardServer(ThreadingHTTPServer):
    def __init__(self, address, token, recipient, upstream="https://api.telegram.org", launch_url="", additional_recipients=()):
        recipients = (recipient, *additional_recipients)
        if not re.fullmatch(r"[0-9]+:[A-Za-z0-9_-]+", token) or any(type(value) is not int or not 0 < value < 1 << 52 for value in recipients):
            raise ValueError("Invalid test bot or recipient configuration")
        self.bot_prefix = "/bot" + token + "/"
        self.file_prefix = "/file/bot" + token + "/"
        self.recipients = frozenset(recipients)
        self.recipient_bytes = frozenset(str(value).encode("ascii") for value in recipients)
        if launch_url:
            parsed = urlsplit(launch_url)
            if (parsed.scheme != "https" or not parsed.hostname or parsed.username is not None
                    or parsed.password is not None or parsed.query or parsed.fragment
                    or not parsed.path.rstrip("/").endswith("/app")
                    or parsed.path.endswith("//") or parsed.geturl() != launch_url
                    or any(char.isspace() or ord(char) < 32 for char in launch_url)):
                raise ValueError("Invalid MiniApp launch URL")
            parsed.port  # Reject malformed ports before accepting any request.
        self.launch_url = launch_url
        self.upstream = upstream.rstrip("/")
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
        super().__init__(address, GuardHandler)

    def handle_error(self, *_):
        # Exception text and request paths can contain the bot token.
        print("Telegram guard request failed", flush=True)


class GuardHandler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def setup(self):
        super().setup()
        self.connection.settimeout(20)

    def log_message(self, *_):
        pass

    def reject(self, code):
        body = json.dumps({"ok": False, "error_code": code, "description": "Test Telegram guard rejected request"}).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Connection", "close")
        self.end_headers()
        self.close_connection = True
        self.wfile.write(body)

    def do_POST(self):
        if not self.path.startswith(self.server.bot_prefix):
            self.reject(403)
            return
        method = self.path[len(self.server.bot_prefix):]
        if method not in ("sendMessage", "sendDocument", "sendChatAction", "setMessageReaction", "getMe", "getFile",
                "getStickerSet", "getCustomEmojiStickers", "answerCallbackQuery", "setMyCommands", "setChatMenuButton"):
            self.reject(403)
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            limit = MAX_DOCUMENT_REQUEST if method == "sendDocument" else MAX_REQUEST
            if self.headers.get("Transfer-Encoding") or not 0 < length <= limit:
                raise ValueError()
            raw = self.rfile.read(length)
            if len(raw) != length:
                raise ValueError()
            if method == "sendDocument":
                if not self.allowed_document(raw):
                    self.reject(403)
                    return
                self.forward(raw, MAX_REQUEST, self.headers.get("Content-Type", ""))
                return
            data = json.loads(raw, object_pairs_hook=unique_object)
            if not isinstance(data, dict):
                raise ValueError()
        except (ValueError, OSError):
            self.reject(400)
            return
        if method in ("setMyCommands", "setChatMenuButton"):
            if not self.allowed_menu(method, data):
                self.reject(403)
                return
        if method in ("getStickerSet", "getCustomEmojiStickers") and not self.allowed_symbol_read(method, data):
            self.reject(403)
            return
        if method == "sendChatAction" and not (
                set(data) == {"chat_id", "action"} and type(data["chat_id"]) is int
                and data["chat_id"] in self.server.recipients
                and data["action"] in ("typing", "choose_sticker")):
            self.reject(403)
            return
        if method == "sendMessage":
            recipient = data.get("chat_id")
            reply = data.get("reply_parameters", {})
            if type(recipient) is not int or recipient not in self.server.recipients or not isinstance(reply, dict) \
                    or reply.get("chat_id", recipient) != recipient or "business_connection_id" in data:
                self.reject(403)
                return
        if method == "setMessageReaction" and not self.allowed_reaction(data):
            self.reject(403)
            return
        self.forward(raw, MAX_REQUEST)

    def allowed_reaction(self, data):
        if set(data) != {"chat_id", "message_id", "reaction"} or type(data['chat_id']) is not int \
                or data['chat_id'] not in self.server.recipients or type(data['message_id']) is not int \
                or data['message_id'] <= 0 or not isinstance(data['reaction'], list) or len(data['reaction']) > 1:
            return False
        supported = {"❤", "👍", "🥰", "👏", "😁", "🤔", "🤯", "😱", "😢", "🎉", "🤩", "🙏", "👌",
                     "😍", "💯", "🤣", "💔", "🤨", "🙈", "😇", "🤝", "🤗", "🫡"}
        return all(isinstance(value, dict) and set(value) == {"type", "emoji"}
                   and value['type'] == 'emoji' and value['emoji'] in supported for value in data['reaction'])

    def allowed_symbol_read(self, method, data):
        if method == "getStickerSet":
            return (set(data) == {"name"} and isinstance(data["name"], str)
                and re.fullmatch(r"[A-Za-z0-9_]{1,64}", data["name"]) is not None)
        return (set(data) == {"custom_emoji_ids"} and isinstance(data["custom_emoji_ids"], list)
            and 0 < len(data["custom_emoji_ids"]) <= 200
            and all(isinstance(value, str) and re.fullmatch(r"[0-9]{1,32}", value) is not None
                for value in data["custom_emoji_ids"]))

    def allowed_menu(self, method, data):
        if method == "setChatMenuButton":
            return (set(data) == {"chat_id", "menu_button"}
                and type(data["chat_id"]) is int and data["chat_id"] in self.server.recipients
                and data["menu_button"] == {"type": "commands"})
        if set(data) - {"commands", "scope", "language_code"} or data.get("language_code", "") not in ("", "ru"):
            return False
        scope = data.get("scope")
        if scope in ({"type": "default"}, {"type": "all_private_chats"}):
            return data.get("commands") == []
        if not isinstance(scope, dict) or set(scope) != {"type", "chat_id"} or scope["type"] != "chat" \
                or type(scope["chat_id"]) is not int or scope["chat_id"] not in self.server.recipients:
            return False
        commands = data.get("commands")
        if not isinstance(commands, list) or not 0 <= len(commands) <= 100:
            return False
        supported = {"menu", "profile", "name", "registrations", "register", "orders", "massage", "avatar", "pairs", "payments"}
        return all(isinstance(command, dict) and set(command) == {"command", "description"}
            and isinstance(command.get("command"), str) and command["command"] in supported and isinstance(command.get("description"), str)
            and 0 < len(command["description"]) <= 256 for command in commands)

    def allowed_document(self, raw):
        content_type = self.headers.get("Content-Type", "")
        # Match Go's canonical writer. MIME parsers can disagree on extra parameters.
        if not re.fullmatch(r"multipart/form-data; boundary=[A-Za-z0-9_-]{1,70}", content_type):
            return False
        message = BytesParser(policy=policy.default).parsebytes(
            b"Content-Type: " + content_type.encode("ascii") + b"\r\nMIME-Version: 1.0\r\n\r\n" + raw)
        if message.get_content_type() != "multipart/form-data" or not message.is_multipart() or message.defects \
                or message.preamble not in (None, "") or message.epilogue not in (None, ""):
            return False
        parts = list(message.iter_parts())
        if len(parts) not in (2, 3) or any(part.defects or part.is_multipart() for part in parts):
            return False
        by_name = {part.get_param("name", header="content-disposition"): part for part in parts}
        if len(by_name) != len(parts) or set(by_name) not in ({"chat_id", "document"}, {"chat_id", "document", "reply_markup"}):
            return False
        if "reply_markup" in by_name:
            markup = by_name["reply_markup"]
            if markup.get_all("Content-Disposition") != ['form-data; name="reply_markup"'] \
                    or not self.allowed_crop_markup(markup.get_payload(decode=True)):
                return False
        recipient, document = by_name["chat_id"], by_name["document"]
        body = document.get_payload(decode=True)
        return (recipient.get_all("Content-Disposition") == ['form-data; name="chat_id"']
            and document.get_all("Content-Disposition") == ['form-data; name="document"; filename="zinusya-avatar.jpg"']
            and recipient.get_filename() is None
            and recipient.get_payload(decode=True) in self.server.recipient_bytes
            and document.get_content_disposition() == "form-data"
            and document.get_filename() == "zinusya-avatar.jpg"
            and body is not None and 0 < len(body) <= 4 << 20)

    def allowed_crop_markup(self, raw):
        if not self.server.launch_url:
            return False
        try:
            markup = json.loads(raw, object_pairs_hook=unique_object)
            if not isinstance(markup, dict) or set(markup) != {"inline_keyboard"}:
                return False
            rows = markup["inline_keyboard"]
            if not isinstance(rows, list) or len(rows) != 1 or not isinstance(rows[0], list) or len(rows[0]) != 1:
                return False
            button = rows[0][0]
            if not isinstance(button, dict) or set(button) != {"text", "web_app"} \
                    or not isinstance(button["text"], str) or not 0 < len(button["text"]) <= 256:
                return False
            web_app = button["web_app"]
            if not isinstance(web_app, dict) or set(web_app) != {"url"} or not isinstance(web_app["url"], str):
                return False
            prefix = self.server.launch_url + "#avatar-crop?draft="
            return web_app["url"].startswith(prefix) and re.fullmatch(
                r"[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}", web_app["url"][len(prefix):]) is not None
        except (ValueError, TypeError):
            return False

    def do_GET(self):
        if self.path == "/health":
            self.send_response(200)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if not self.path.startswith(self.server.file_prefix):
            self.reject(403)
            return
        path = self.path[len(self.server.file_prefix):]
        if not path or len(path) > 1024 or not re.fullmatch(r"[A-Za-z0-9_/.-]+", path) \
                or any(part in ("", ".", "..") for part in path.split("/")):
            self.reject(403)
            return
        self.forward(None, MAX_FILE)

    def forward(self, body, limit, content_type="application/json"):
        started = time.monotonic()
        event = {"event": "telegram_forward", "request_id": uuid.uuid4().hex,
                 "method": "file_download" if self.command == "GET" else self.path.rsplit("/", 1)[-1],
                 "status": 0, "bytes": 0}
        if event["method"] == "sendChatAction" and body is not None:
            event["action"] = json.loads(body)["action"]
        try:
            self.forward_request(body, limit, content_type, event)
        except (OSError, http.client.HTTPException) as error:
            event["error_class"] = type(error).__name__
            self.close_connection = True
            if not event["status"]:
                event["status"] = 502
                self.reject(502)
        finally:
            event["elapsed_ms"] = round((time.monotonic() - started) * 1000)
            event["at_unix_ms"] = round(time.time() * 1000)
            print(json.dumps(event), flush=True)

    def forward_request(self, body, limit, content_type, event):
        request = urllib.request.Request(self.server.upstream + self.path, data=body,
            headers={"Content-Type": content_type}, method=self.command)
        try:
            response = self.server.opener.open(request, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        except OSError as error:
            event.update(status=502, error_class=type(error).__name__)
            reason = getattr(error, "reason", None)
            if isinstance(reason, BaseException):
                event["reason_class"] = type(reason).__name__
            self.reject(502)
            return
        with response:
            event["upstream_status"] = response.code
            length = response.headers.get("Content-Length")
            if length is not None and (not length.isdecimal() or int(length) > limit):
                event.update(status=502, error_class="ResponseSizeLimit")
                self.reject(502)
                return
            self.send_response(response.code)
            event["status"] = response.code
            self.send_header("Content-Type", response.headers.get("Content-Type", "application/octet-stream"))
            if length is not None:
                self.send_header("Content-Length", length)
            else:
                self.send_header("Transfer-Encoding", "chunked")
            self.send_header("Connection", "close")
            self.end_headers()
            remaining = limit
            self.close_connection = True
            while True:
                chunk = response.read(min(65536, remaining + 1))
                if not chunk:
                    if length is None:
                        self.wfile.write(b"0\r\n\r\n")
                    break
                if len(chunk) > remaining:
                    event["error_class"] = "ResponseSizeLimit"
                    # No terminal chunk: clients must reject the incomplete body.
                    break
                if length is None:
                    self.wfile.write(f"{len(chunk):x}\r\n".encode())
                self.wfile.write(chunk)
                event["bytes"] += len(chunk)
                if length is None:
                    self.wfile.write(b"\r\n")
                remaining -= len(chunk)


if __name__ == "__main__":
    try:
        token = os.environ["SHUTTLE_TELEGRAM_TOKEN"].strip()
        if token.split(":", 1)[0] != os.environ.get("TEST_TELEGRAM_BOT_ID", "6087685431"):
            raise ValueError()
        recipient = int(os.environ["TEST_TELEGRAM_USER_ID"])
        server = GuardServer(("0.0.0.0", 8080), token, recipient,
            launch_url=os.environ.get("TEST_MINI_APP_LAUNCH_URL", ""),
            additional_recipients=tuple(int(value) for value in os.environ.get("TEST_TELEGRAM_ADDITIONAL_USER_IDS", "").split(",") if value))
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(os.environ["TEST_TLS_CERT"], os.environ["TEST_TLS_KEY"])
        server.socket = context.wrap_socket(server.socket, server_side=True)
        server.serve_forever()
    except Exception:
        raise SystemExit("Telegram guard stopped; verify test bot and account configuration") from None

# Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
# on behalf of Daniel Drizhuk
