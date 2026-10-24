"""Explicit existing-owner chat clear or one synthetic passive event; no run/model."""
import argparse
import datetime as dt
import json
import pathlib
import ssl
import urllib.parse
import urllib.request
import uuid
import operator_cli as qa
import scenario_db as scenario


def literal(value):
    return "'" + value.replace("'", "''") + "'"


def execute(cfg, plan, private_dir):
    qa.runtime_guard(cfg)
    scenario.fences(cfg)
    for key, image in (("postgres_id", "postgres_image"), ("loom_api_id", "loom_api_image")):
        qa.need(scenario.inspect(cfg[key])["Image"] == cfg[image], "Local runtime custody changed")
    scenario.idle(cfg, cfg["app_database"])
    chat, owner = str(uuid.UUID(plan["chat_id"])), str(uuid.UUID(plan["owner_id"]))
    operation = str(uuid.UUID(plan["operation"]))
    selected = json.loads(scenario.sql(cfg, "SELECT json_build_object('chat_id',c.id::text,'owner_id',c.owner_id::text,'org_id',c.org_id,'subject',u.subject,'issuer',u.issuer) FROM chats c JOIN users u ON u.id=c.owner_id WHERE c.id=" + literal(chat) + "::uuid AND c.deleted_at IS NULL;", cfg["loom_database"]))
    qa.need(selected == {"chat_id": chat, "owner_id": owner, "org_id": cfg["organization_id"], "subject": plan["subject"], "issuer": cfg["issuer"]}, "Exact existing selected chat/owner/subject required")
    backup = cfg["loom_backup_receipt"]
    qa.need(qa.sha(backup["path"]) == backup["sha256"], "Loom backup receipt changed")
    saved = qa.load(backup["path"])
    qa.need(saved["project"] == qa.PROJECT and saved["owner"] == qa.OWNER and saved["scope"] == "local_loom" and saved["fully_decoded"] is True, "Fully decoded local Loom backup required")
    age = dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(saved["created_at"])
    qa.need(dt.timedelta(0) <= age < dt.timedelta(minutes=30), "Fresh Loom backup required")
    endpoint = urllib.parse.urlsplit(cfg["loom_base_url"])
    qa.need(endpoint.scheme == "https" and endpoint.hostname in ("zns.localhost", "iam.zns.localhost", "localhost", "127.0.0.1") and endpoint.port and endpoint.path in ("", "/") and not endpoint.username and not endpoint.password and not endpoint.query and not endpoint.fragment, "Only verified local Loom origin")
    bearer = cfg["loom_bearer"]
    qa.need(qa.sha(bearer["path"]) == bearer["sha256"], "Actual delegated Loom credential custody changed")
    token = pathlib.Path(bearer["path"]).read_text().strip()
    qa.need(token and not any(c.isspace() for c in token), "Invalid delegated bearer")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), qa.NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=cfg["ca_file"])))
    if plan["action"] == "clear":
        method, suffix, body, expected_status = "DELETE", "", None, 204
    elif plan["action"] == "append":
        qa.need(plan["text"].startswith("SYNTHETIC QA history: ") and len(plan["text"]) <= 2000, "Explicit bounded synthetic history only")
        body = {"source_event_id": "manual-qa:" + operation, "type": "action", "action": "qa.history.seed", "text": plan["text"], "data": {"synthetic": True}, "occurred_at": plan["occurred_at"]}
        dt.datetime.fromisoformat(plan["occurred_at"].replace("Z", "+00:00"))
        method, suffix, expected_status = "POST", "/events", 201
    else:
        raise ValueError("Only explicit selected chat clear or passive append")
    directory = scenario.protected_directory(private_dir)
    qa.write_once(directory / "intent.json", {"plan": plan, "selected": selected, "no_model_or_ASR_dispatch": True})
    url = cfg["loom_base_url"].rstrip("/") + "/v1/chats/" + chat + suffix
    qa.write_once(directory / "dispatch.json", {"method": method, "chat_id": chat, "automatic_retry": False})
    with opener.open(urllib.request.Request(url, None if body is None else qa.canonical(body), {"Authorization": "Bearer " + token, "Content-Type": "application/json"}, method=method), timeout=20) as response:
        qa.need(response.status == expected_status and response.geturl() == url, "Unknown native chat outcome")
        raw = response.read(1_048_577)
        qa.need(len(raw) <= 1_048_576, "Native result too large")
    qa.write_once(directory / "response.json", {"http_status": expected_status, "body": json.loads(raw) if raw else None})
    qa.write_once(directory / "receipt.json", {"response_saved": True, "action": plan["action"], "no_run_created_by_helper": True, "acceptance_pass": False, "next": "Root checks canonical selected chat/event and unchanged run/paid ledger counts; chat clear is logical deletion, not physical erasure"})
    return {"response_saved": True, "no_run_endpoint_called": True, "acceptance_pass": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--plan", required=True)
    parser.add_argument("--apply-plan-sha256")
    parser.add_argument("--apply-config-sha256")
    parser.add_argument("--private-dir")
    args = parser.parse_args()
    cfg, plan = qa.validate_config(qa.load(args.config)), qa.load(args.plan)
    if args.apply_plan_sha256 is None:
        print(json.dumps({"plan_only": True, "plan_sha256": qa.sha(args.plan), "config_sha256": qa.sha(args.config), "no_default_chat_GET": True, "no_model_calls": True}))
        return
    qa.need(args.private_dir and qa.sha(args.plan) == args.apply_plan_sha256 and qa.sha(args.config) == args.apply_config_sha256, "Reviewed plan/config/private directory required")
    print(json.dumps(execute(cfg, plan, args.private_dir)))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"stopped": True, "error_type": type(error).__name__, "outcome_may_be_unknown": True, "no_automatic_retry": True}))
        raise SystemExit(1)
