"""Small explicitly synthetic history snapshot, never fake Telegram ingestion."""
import argparse
import hashlib
import json
import pathlib
import uuid
import operator_cli as qa
import scenario_db as scenario


def fixture(bot_id, telegram_user_id, instance):
    qa.need(type(bot_id) is int and bot_id == qa.BOT and type(telegram_user_id) is int and 0 < telegram_user_id < 1 << 52, "Exact local bot and selected numeric test owner required")
    uuid.UUID(instance)
    owner = telegram_user_id
    return {
        "users": [{"_id": "qa-user-" + instance, "bot_id": bot_id, "user_id": owner}],
        "messages": [
            {"_id": "qa-question-" + instance, "user_id": owner, "role": "user", "content": "SYNTHETIC QA history: what was the practice event?", "date": {"$date": "2025-01-01T10:00:00Z"}},
            {"_id": "qa-answer-" + instance, "user_id": owner, "role": "assistant", "content": "SYNTHETIC QA history: practice event Example, without real-world payment facts.", "date": {"$date": "2025-01-01T10:01:00Z"}},
        ],
    }


def write_fixture(directory, bot_id, owner, instance):
    data = fixture(bot_id, owner, instance)
    directory = scenario.protected_directory(directory)
    manifest = {"version": 1, "source_instance": "manual-qa-" + instance, "bot_id": bot_id, "date_policy": "preserve_source", "collections": []}
    for role, records in data.items():
        raw = b"".join(qa.canonical(record) + b"\n" for record in records)
        name = role + ".ndjson"
        with (directory / name).open("xb") as stream:
            stream.write(raw)
        manifest["collections"].append({"role": role, "name": "synthetic_" + role, "path": name, "sha256": hashlib.sha256(raw).hexdigest(), "single_bot_collection": role == "messages"})
    qa.write_once(directory / "snapshot.json", manifest)
    return {"synthetic_only": True, "users": 1, "messages": 2, "live_tasks_created": 0, "next": "maintained shuttle-import inspect/build/verify, then separately reviewed archive upload/history apply; no Telegram events fabricated"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--bot-id", type=int, required=True)
    parser.add_argument("--telegram-user-id", type=int, required=True)
    parser.add_argument("--instance", required=True, help="Explicit new synthetic fixture UUID")
    parser.add_argument("--write-private-dir")
    args = parser.parse_args()
    fixture(args.bot_id, args.telegram_user_id, args.instance)
    if args.write_private_dir:
        print(json.dumps(write_fixture(args.write_private_dir, args.bot_id, args.telegram_user_id, args.instance)))
    else:
        print(json.dumps({"plan_only": True, "synthetic_users": 1, "synthetic_messages": 2, "no_runtime_effects": True}))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"stopped": True, "error_type": type(error).__name__}))
        raise SystemExit(1)
