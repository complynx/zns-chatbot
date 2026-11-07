"""Local human-stand operator: offline plans, explicit one-request application.

No credential acquisition, SQL mutation, automatic confirmation or retry.
"""
import argparse
import datetime as dt
import hashlib
import json
import pathlib
import ssl
import subprocess
import urllib.error
import urllib.parse
import urllib.request
import uuid

PROJECT = "zinusya-local-testbot-20261023"
OWNER = "92a098283e6c4222a85291cc70dceea5"
BOT = 6087685431
AGENT = "/internal/agent/administration/"
# name: (verb, path, mutates, required argument keys)
ACTIONS = {
    "events": ("GET", "/internal/admin/events", False, ()),
    "event": ("GET", "/internal/admin/events/{event_id}", False, ()),
    "event-set": ("PUT", "/internal/admin/events/{event_id}", True, ("expected_version", "document")),
    "registration": ("GET", "/internal/events/{event_id}/registration", False, ()),
    "registration-join": ("POST", "/internal/events/{event_id}/registration", True, ("role",)),
    "registrations": ("GET", "/internal/admin/events/{event_id}/registrations", False, ()),
    "registration-assign": ("POST", "/internal/admin/events/{event_id}/registrations/{registration_id}/assign", True, ("expected_version", "expected_event_version")),
    "authorities": ("POST", AGENT + "authorities", False, ("event_id",)),
    "authority-prepare": ("POST", AGENT + "authority/prepare", True, ("event_id", "user_id", "role", "expected_version")),
    "authority-confirm": ("POST", AGENT + "authority", True, ("event_id", "user_id", "role", "expected_version")),
    "authority-status": ("POST", AGENT + "authority/status", False, ()),
    "app-user": ("POST", AGENT + "user", False, ("id",)),
    "app-revoke-prepare": ("POST", AGENT + "revoke_user/prepare", True, ("user_id", "expected_subject", "expected_revoked")),
    "app-revoke-confirm": ("POST", AGENT + "revoke_user", True, ("user_id", "expected_subject", "expected_revoked")),
    "tasks": ("POST", AGENT + "tasks", False, ()),
    "task": ("POST", AGENT + "task", False, ("id",)),
    "orders-catalog": ("POST", AGENT + "orders_catalog", False, ("event_id",)),
    "meals-catalog": ("POST", AGENT + "meal_catalog", False, ("event_id",)),
}
for kind in ("orders", "meal"):
    for suffix, tail in (("prepare", "/prepare"), ("confirm", "")):
        ACTIONS[f"{kind}-catalog-{suffix}"] = (
            "POST", AGENT + f"set_{kind}_catalog" + tail, True,
            ("event_id", "expected_version", "expected_event_version", "document"))


def need(condition, message):
    if not condition:
        raise ValueError(message)


def sha(path):
    return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()


def load(path):
    return json.loads(pathlib.Path(path).read_text(encoding="utf-8-sig"))


def canonical(value):
    return json.dumps(value, ensure_ascii=False, sort_keys=True, separators=(",", ":")).encode()


def build(action, arguments, targets, operation, proposal=None):
    need(action in ACTIONS, "Unsupported action: no SQL reset, hard delete or fabricated approval")
    method, path, mutation, required = ACTIONS[action]
    operation = str(uuid.UUID(operation))
    for key, value in targets.items():
        need(key in ("event_id", "registration_id"), "Unexpected route target")
        path = path.replace("{" + key + "}", str(uuid.UUID(value)))
    need("{" not in path, "Explicit route target UUID required")
    need(isinstance(arguments, dict) and set(required) <= arguments.keys(), "Required arguments missing")
    for key, value in arguments.items():
        if key.startswith("expected_") and key.endswith("version"):
            need(type(value) is int and value >= 0, "Explicit nonnegative version required")
        if key.endswith("_id") or key == "id":
            uuid.UUID(value)
    if action.startswith("authority-") and mutation:
        need(arguments["role"] in ("none", "ambassador", "administrator"), "Unsupported local role")
    if action.startswith("app-revoke-"):
        need(type(arguments["expected_revoked"]) is bool and bool(arguments["expected_subject"]), "Exact current subject/revocation required")
    if method == "GET":
        need(not arguments, "GET arguments use explicit route targets only")
        body = None
    elif path.startswith("/internal/agent/"):
        body = {"protocol_version": 1, "operation_id": operation, "arguments": arguments}
    else:
        body = {"operation_id": operation, **arguments}
    if action.endswith("-confirm"):
        need(proposal is not None, "Existing human-reviewed proposal required; no automatic prepare+confirm")
        body.update(idempotency_key=operation, confirmation={"mode": "exact_parameters", "preconditions": {"proposal_id": str(uuid.UUID(proposal))}})
    return {"action": action, "method": method, "path": path, "body": body, "mutation": mutation}


def validate_config(cfg):
    need((cfg["project"], cfg["owner"], cfg["bot_id"]) == (PROJECT, OWNER, BOT), "Wrong human stand")
    endpoint = urllib.parse.urlsplit(cfg["base_url"])
    need(endpoint.scheme == "https" and endpoint.hostname in ("iam.zns.localhost", "zns.localhost", "localhost", "127.0.0.1"), "Only explicit local verified TLS")
    need(endpoint.port is not None and not endpoint.username and not endpoint.password and not endpoint.query and not endpoint.fragment and endpoint.path in ("", "/"), "Local endpoint origin required")
    need(cfg["issuer"] == "https://iam.zns.localhost:28443" and cfg["organization_id"], "Wrong deployment scope")
    return cfg


def runtime_guard(cfg):
    validate_config(cfg)
    # Only selected metadata is used; Docker environment/secret values are never printed.
    app = json.loads(subprocess.check_output(["docker", "inspect", cfg["app_id"]], text=True, timeout=20))[0]
    labels = app["Config"]["Labels"]
    need(app["Id"] == cfg["app_id"] and app["Image"] == cfg["app_image"], "Application custody changed")
    need(labels.get("com.docker.compose.project") == PROJECT and labels.get("zinusya.qa.owner") == OWNER, "Container ownership mismatch")
    need(app["State"]["Running"] and not app["State"].get("OOMKilled"), "Application unavailable")
    for item in (cfg["configuration_custody"], cfg["deployment_receipt"]):
        need(sha(item["path"]) == item["sha256"], "Custody receipt/configuration changed")
    receipt = load(cfg["deployment_receipt"]["path"])
    for key in ("project", "owner", "bot_id", "issuer", "organization_id", "app_id", "app_image"):
        need(receipt[key] == cfg[key], "Deployment receipt mismatch: " + key)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *_):
        return None


def request(cfg, req):
    context = ssl.create_default_context(cafile=cfg["ca_file"])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect(), urllib.request.HTTPSHandler(context=context))
    need(sha(cfg["bearer_file"]) == cfg["bearer_sha256"], "Selected principal custody changed")
    token = pathlib.Path(cfg["bearer_file"]).read_text().strip()
    need(token and not any(c.isspace() for c in token), "Invalid principal bearer")
    url = cfg["base_url"].rstrip("/") + req["path"]
    raw = None if req["body"] is None else canonical(req["body"])
    http = urllib.request.Request(url, raw, {"Authorization": "Bearer " + token, "Content-Type": "application/json"}, method=req["method"])
    try:
        with opener.open(http, timeout=20) as response:
            need(response.status == 200 and response.geturl() == url, "Unexpected HTTP outcome")
            data = response.read(1_048_577)
            need(len(data) <= 1_048_576, "Response too large")
    except urllib.error.HTTPError as error:
        if error.code == 404 and not req["mutation"]:
            return {"_qa_http_status": 404}
        raise
    result = json.loads(data)
    need(result.get("status") not in ("unknown", "pending", "not_found", "rejected"), "Unknown, pending, missing or rejected operation; no retry")
    return result


def idle_tasks(cfg):
    """Bounded current task/outbox read; historical receipts are never deleted."""
    after = None
    for _ in range(50):
        arguments = {"limit": 20}
        if after:
            arguments["after"] = after
        result = request(cfg, build("tasks", arguments, {}, str(uuid.uuid4())))["result"]
        for task in result.get("tasks") or []:
            need(task["state"] in ("completed", "closed", "failed", "cancelled"), "Active/unknown task: drain or reconcile, do not reset")
            diagnostic = request(cfg, build("task", {"id": task["id"]}, {}, str(uuid.uuid4())))["result"]
            need(all(row["state"] == "delivered" for row in diagnostic.get("deliveries") or []), "Pending/unknown delivery blocks setup")
        after = result.get("next")
        if not after:
            return
    raise ValueError("Task inventory exceeds bounded proof; no mutation")


def pointer(value, path):
    need(path.startswith("/") and path != "/", "Explicit JSON pointer required")
    for part in path[1:].split("/"):
        key = part.replace("~1", "/").replace("~0", "~")
        value = value[int(key)] if isinstance(value, list) else value[key]
    return value


def write_once(path, value):
    with pathlib.Path(path).open("x", encoding="utf-8") as stream:
        stream.write(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
        stream.flush()
        import os
        os.fsync(stream.fileno())


def apply_plan(cfg, plan, directory):
    runtime_guard(cfg)
    need(plan["config_sha256"] == hashlib.sha256(canonical(cfg)).hexdigest(), "Plan/config drift")
    req = build(**plan["selection"])
    need(req == plan["request"] and req["mutation"], "Invalid mutation plan")
    before = build(**plan["before"])
    need(not before["mutation"] and plan["expected_fields"], "Read-before-write with exact expected fields required")
    # Root must supply an already protected directory; refuse generic chmod on Windows.
    directory = pathlib.Path(directory)
    need(directory.is_dir() and not directory.is_symlink(), "Existing protected audit directory required")
    if __import__("os").name == "nt":
        script = "$a=Get-Acl -LiteralPath $env:SHUTTLE_QA_AUDIT;$r=@($a.Access);$s=[System.Security.Principal.WindowsIdentity]::GetCurrent().User.Value;if(-not $a.AreAccessRulesProtected -or $r.Count -ne 1 -or $r[0].IsInherited -or $r[0].IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value -ne $s -or $r[0].AccessControlType -ne 'Allow' -or $r[0].FileSystemRights -ne 'FullControl' -or $r[0].InheritanceFlags -ne ([System.Security.AccessControl.InheritanceFlags]::ObjectInherit -bor [System.Security.AccessControl.InheritanceFlags]::ContainerInherit) -or $r[0].PropagationFlags -ne [System.Security.AccessControl.PropagationFlags]::None){throw 'Private DACL required'}"
        subprocess.run(["pwsh", "-NoProfile", "-NonInteractive", "-Command", script], env={**__import__("os").environ, "SHUTTLE_QA_AUDIT": str(directory)}, check=True, capture_output=True, timeout=20)
    else:
        need(directory.stat().st_mode & 0o077 == 0, "Private mode required")
    # Prepare and confirm share the backend operation but are separate one-shot actions.
    prefix = directory / (plan["selection"]["operation"] + "-" + plan["selection"]["action"])
    intent = prefix.with_suffix(".intent.json")
    write_once(intent, {"phase": "read_before_write", "plan": plan})
    current = request(cfg, before)
    write_once(prefix.with_suffix(".before.json"), current)
    for field, expected in plan["expected_fields"].items():
        need(pointer(current, field) == expected, "Object changed: " + field)
    idle_tasks(cfg)
    backup = cfg["backup_receipt"]
    need(sha(backup["path"]) == backup["sha256"], "Backup receipt changed")
    saved = load(backup["path"])
    need(saved["project"] == PROJECT and saved["owner"] == OWNER and saved["fully_decoded"] is True, "Decoded local backup required")
    age = dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(saved["created_at"])
    need(dt.timedelta(0) <= age < dt.timedelta(minutes=30), "Fresh backup required")
    runtime_guard(cfg)
    # Exclusive dispatch marker before send. A lost response remains unknown; never resend.
    write_once(prefix.with_suffix(".dispatch.json"), {"phase": "dispatch_intent", "request_sha256": hashlib.sha256(canonical(req)).hexdigest()})
    try:
        result = request(cfg, req)
        write_once(prefix.with_suffix(".response.json"), result)
    except Exception as error:
        write_once(prefix.with_suffix(".unknown.json"), {"phase": "unknown_do_not_retry", "error_type": type(error).__name__})
        raise
    return {"sent_once": True, "response_saved": True, "acceptance_pass": False,
            "next": "Inspect canonical status/result as same actor; HTTP success alone is not business acceptance"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    sub = parser.add_subparsers(dest="command")
    sub.add_parser("inspect", help="Offline supported action map; no credentials/network")
    plan = sub.add_parser("plan", help="Validate request draft offline; no writes to stand")
    plan.add_argument("--config", required=True)
    plan.add_argument("--selection", required=True, help="JSON selection/before/expected_fields")
    observe = sub.add_parser("observe", help="Explicit read-only API request; output can contain PII")
    observe.add_argument("--config", required=True)
    observe.add_argument("--selection", required=True)
    apply = sub.add_parser("apply", help="One explicit mutation; no automatic retry/confirmation")
    apply.add_argument("--config", required=True)
    apply.add_argument("--plan", required=True)
    apply.add_argument("--sha256", required=True)
    apply.add_argument("--audit-dir", required=True)
    apply.add_argument("--ack", required=True, choices=["LOCAL_SELECTED_OPERATION_ONLY"])
    args = parser.parse_args()
    if args.command in (None, "inspect"):
        print(json.dumps({"offline": True, "project": PROJECT, "actions": ACTIONS}, indent=2))
        return
    cfg = validate_config(load(args.config))
    if args.command == "plan":
        draft = load(args.selection)
        req = build(**draft["selection"])
        need(req["mutation"], "Use observe for reads")
        before = build(**draft["before"])
        need(not before["mutation"] and draft["expected_fields"], "Exact read preconditions required")
        print(json.dumps({**draft, "request": req, "config_sha256": hashlib.sha256(canonical(cfg)).hexdigest()}, ensure_ascii=False, indent=2))
    elif args.command == "observe":
        req = build(**load(args.selection))
        need(not req["mutation"], "Observe rejects mutations")
        runtime_guard(cfg)
        print(json.dumps(request(cfg, req), ensure_ascii=False, indent=2))
    else:
        need(sha(args.plan) == args.sha256, "Explicit reviewed plan hash required")
        print(json.dumps(apply_plan(cfg, load(args.plan), args.audit_dir), indent=2))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        # Do not expose request bodies, credentials, URLs with secrets or API error detail.
        print(json.dumps({"stopped": True, "error_type": type(error).__name__, "no_automatic_retry": True}))
        raise SystemExit(1)
