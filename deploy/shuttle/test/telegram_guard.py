"""Owned-stand Telegram egress: only the configured account can receive messages."""
import json
import os
import re
import ssl
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
    def __init__(self, address, token, recipient, upstream="https://api.telegram.org", launch_url=""):
        if not re.fullmatch(r"[0-9]+:[A-Za-z0-9_-]+", token) or not 0 < recipient < 1 << 52:
            raise ValueError("Invalid test bot or recipient configuration")
        self.bot_prefix = "/bot" + token + "/"
        self.file_prefix = "/file/bot" + token + "/"
        self.recipient = recipient
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
        if method not in ("sendMessage", "sendDocument", "sendChatAction", "getMe", "getFile", "answerCallbackQuery", "setMyCommands", "setChatMenuButton"):
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
        if method == "sendChatAction" and not (
                set(data) == {"chat_id", "action"} and type(data["chat_id"]) is int
                and data["chat_id"] == self.server.recipient and data["action"] == "typing"):
            self.reject(403)
            return
        if method == "sendMessage":
            recipient = data.get("chat_id")
            reply = data.get("reply_parameters", {})
            if type(recipient) is not int or recipient != self.server.recipient or not isinstance(reply, dict) \
                    or reply.get("chat_id", recipient) != recipient or "business_connection_id" in data:
                self.reject(403)
                return
        self.forward(raw, MAX_REQUEST)

    def allowed_menu(self, method, data):
        if method == "setChatMenuButton":
            return (set(data) == {"chat_id", "menu_button"}
                and type(data["chat_id"]) is int and data["chat_id"] == self.server.recipient
                and data["menu_button"] == {"type": "commands"})
        if set(data) - {"commands", "scope", "language_code"} or data.get("language_code", "") not in ("", "ru"):
            return False
        scope = data.get("scope")
        if not isinstance(scope, dict) or set(scope) != {"type", "chat_id"} or scope["type"] != "chat" \
                or type(scope["chat_id"]) is not int or scope["chat_id"] != self.server.recipient:
            return False
        commands = data.get("commands")
        if not isinstance(commands, list) or not 0 < len(commands) <= 100:
            return False
        supported = {"start", "menu", "profile", "name", "registrations", "register", "orders", "massage", "avatar", "pairs", "payments", "knowledge"}
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
            and recipient.get_payload(decode=True) == str(self.server.recipient).encode("ascii")
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
        request = urllib.request.Request(self.server.upstream + self.path, data=body,
            headers={"Content-Type": content_type}, method=self.command)
        try:
            response = self.server.opener.open(request, timeout=20)
        except urllib.error.HTTPError as error:
            response = error
        except OSError:
            self.reject(502)
            return
        with response:
            length = response.headers.get("Content-Length")
            if length is not None and (not length.isdecimal() or int(length) > limit):
                self.reject(502)
                return
            self.send_response(response.code)
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
                    # No terminal chunk: clients must reject the incomplete body.
                    break
                if length is None:
                    self.wfile.write(f"{len(chunk):x}\r\n".encode())
                self.wfile.write(chunk)
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
            launch_url=os.environ.get("TEST_MINI_APP_LAUNCH_URL", ""))
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(os.environ["TEST_TLS_CERT"], os.environ["TEST_TLS_KEY"])
        server.socket = context.wrap_socket(server.socket, server_side=True)
        server.serve_forever()
    except Exception:
        raise SystemExit("Telegram guard stopped; verify test bot and account configuration") from None

# Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
# on behalf of Daniel Drizhuk
