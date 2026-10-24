"""Local test ingress: acknowledge Telegram only after Shuttle persists an update."""

import json
import os
import signal
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path


def post(url, body, headers=None, timeout=40):
    request = urllib.request.Request(
        url, data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", **(headers or {})},
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        return json.load(response)


def private_update(update, allowed_user=None):
    callback = update.get("callback_query")
    message = callback.get("message", {}) if callback else update.get("message", {})
    user = callback.get("from", {}) if callback else message.get("from", {})
    chat = message.get("chat", {})
    uid = user.get("id", 0)
    return type(uid) is int and 0 < uid < 1 << 52 and (allowed_user is None or uid == allowed_user) and not user.get("is_bot", False) and chat.get("type") == "private" and chat.get("id") == uid

def check_bot(base=None):
    base = base or "https://api.telegram.org/bot" + os.environ["SHUTTLE_TELEGRAM_TOKEN"].strip() + "/"
    me = post(base + "getMe", {})["result"]
    if me['id'] != int(os.environ['TEST_TELEGRAM_BOT_ID']):
        raise RuntimeError('Unexpected bot identity')
    webhook = post(base + "getWebhookInfo", {})
    if webhook["result"]["url"]:
        raise RuntimeError("Existing webhook: stop its owner before enabling test polling")
    return me

def main(base=None, cursor=None):
    def stop(*_):
        # Interrupt HTTP waits. An uncommitted cursor replays safely on restart.
        raise SystemExit(0)

    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    allowed_user = int(os.environ["TEST_TELEGRAM_USER_ID"])
    if not 0 < allowed_user < 1 << 52:
        raise ValueError("Invalid test account")
    token = os.environ["SHUTTLE_TELEGRAM_TOKEN"].strip()
    base = base or "https://api.telegram.org/bot" + token + "/"
    me = check_bot(base)
    print("Polling test bot", me["id"], me.get("username", ""), flush=True)
    cursor = cursor or Path("/state/offset")
    offset = int(cursor.read_text()) if cursor.exists() else 0
    startup_reported = False

    while True:
        try:
            updates = post(base + "getUpdates", {
                "offset": offset, "timeout": 25,
                "allowed_updates": ["message", "callback_query"],
            })["result"]
            for update in updates:
                if private_update(update, allowed_user):
                    post(os.environ["TEST_WEBHOOK_URL"], update, {
                        "X-Telegram-Bot-Api-Secret-Token": os.environ["TEST_WEBHOOK_SECRET"],
                    }, timeout=5)
                else:
                    print("Ignored update outside the test account", update["update_id"], flush=True)
                offset = update["update_id"] + 1
                temporary = cursor.with_suffix(".tmp")
                temporary.write_text(str(offset))
                temporary.replace(cursor)
            if not startup_reported:
                print("Test ingress polling cycle completed", flush=True)
                startup_reported = True
        except urllib.error.HTTPError as error:
            # Exception text includes the bot credential URL; report only status.
            error.close()
            print("Ingress retry; HTTP status", error.code, flush=True)
            if error.code in (401, 409):
                raise RuntimeError("Invalid token or concurrent Telegram consumer") from None
            time.sleep(1)
        except (OSError, ValueError, KeyError):
            print("Ingress retry; transport or response failure", flush=True)
            time.sleep(1)


if __name__ == "__main__":
    try:
        if '--check' in sys.argv:
            me = check_bot()
            print('Verified expected test bot', me['id'], me.get('username', ''))
        else:
            main()
    except Exception:
        raise SystemExit("Test ingress stopped; check credentials, webhook and consumer ownership") from None
