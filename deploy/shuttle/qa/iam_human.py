"""Selected local IAM human lifecycle; app database and service principals untouched."""
import argparse
import datetime as dt
import json
import pathlib
import ssl
import urllib.parse
import urllib.request
import urllib.error
import uuid
import operator_cli as qa
import scenario_db as scenario


def selected_human(user, subject, organization, username, forbidden):
    qa.need(isinstance(subject, str) and subject and subject not in forbidden, "Service/operator subject forbidden")
    qa.need(user["id"] == subject and user["details"]["resourceOwner"] == organization, "Exact selected organization/subject required")
    qa.need(isinstance(user.get("human"), dict) and "machine" not in user, "Selected existing HUMAN only")
    qa.need(user["userName"] == username, "Selected username changed")


def create_body(value, username):
    qa.need(isinstance(value, dict) and set(value) <= {"userName", "profile", "email", "initialPassword"}, "Only selected human fields; no ID, service roles or provider-link fabrication")
    qa.need(value["userName"] == username, "Recreation must match selected username")
    qa.need(value["profile"].get("firstName") and value["profile"].get("lastName"), "Explicit test-human name required")
    qa.need(value["email"].get("email") and type(value["email"].get("isEmailVerified")) is bool, "Explicit test-human email/verification required")
    return value


def request(cfg, method, path, body=None):
    qa.need(path.startswith("/management/v1/users/") and "?" not in path and "#" not in path, "Only selected IAM user endpoints")
    qa.need(qa.sha(cfg["iam_bearer"]["path"]) == cfg["iam_bearer"]["sha256"], "IAM manager credential custody changed")
    token = pathlib.Path(cfg["iam_bearer"]["path"]).read_text().strip()
    qa.need(token and not any(c.isspace() for c in token), "Invalid IAM manager bearer")
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), qa.NoRedirect(), urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=cfg["ca_file"])))
    url = cfg["issuer"] + path
    req = urllib.request.Request(url, None if body is None else qa.canonical(body), {"Authorization": "Bearer " + token, "Content-Type": "application/json", "x-zitadel-orgid": cfg["organization_id"]}, method=method)
    with opener.open(req, timeout=20) as response:
        qa.need(response.status == 200 and response.geturl() == url, "Unknown IAM outcome; never retry automatically")
        raw = response.read(1_048_577)
        qa.need(len(raw) <= 1_048_576, "IAM response too large")
    return json.loads(raw)


def admission(cfg):
    qa.runtime_guard(cfg)
    scenario.fences(cfg)
    iam = scenario.inspect(cfg["iam_id"])
    qa.need(iam["Image"] == cfg["iam_image"] and iam["Config"]["Labels"].get("com.docker.compose.service") == "zitadel" and iam["State"]["Running"], "Local IAM changed")
    app = qa.load(cfg["configuration_custody"]["path"])
    qa.need(app["identity"]["organization_id"] == cfg["organization_id"] and app["identity"]["issuer"] == cfg["issuer"], "Wrong deployment configuration")
    denied = set(cfg["forbidden_subjects"])
    qa.need(app["identity"]["actor_id"] in denied and cfg["iam_operator_subject"] in denied, "Explicit service actor/operator exclusions required")
    backup = cfg["iam_backup_receipt"]
    qa.need(qa.sha(backup["path"]) == backup["sha256"], "IAM backup receipt changed")
    receipt = qa.load(backup["path"])
    qa.need(receipt["project"] == qa.PROJECT and receipt["owner"] == qa.OWNER and receipt["scope"] == "local_iam" and receipt["fully_decoded"] is True, "Fully decoded local IAM backup required")
    age = dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(receipt["created_at"])
    qa.need(dt.timedelta(0) <= age < dt.timedelta(minutes=30), "Fresh IAM backup required")
    # Bootstrap/removal of the only app-admin cannot require an already working
    # app-admin token. Root's owned local PG read is a guard, never a business write.
    pg = scenario.inspect(cfg["postgres_id"])
    qa.need(pg["Image"] == cfg["postgres_image"] and pg["Config"]["Labels"].get("com.docker.compose.service") == "postgres", "Local PG custody changed")
    database = urllib.parse.urlsplit(app["database_url"]).path[1:]
    qa.need(database == cfg["app_database"], "Wrong application scenario database")
    binding = json.loads(scenario.sql(cfg, "SELECT json_build_object('bot_id',bot_id,'organization_id',organization_id,'issuer',issuer) FROM shuttle_deployment;", database))
    qa.need(binding == {"bot_id": qa.BOT, "organization_id": cfg["organization_id"], "issuer": cfg["issuer"]}, "IAM operation app-deployment guard mismatch")
    scenario.idle(cfg, database)
    return denied


def execute(cfg, plan, directory):
    denied = admission(cfg)
    subject, username = plan["subject"], plan["username"]
    qa.need(subject not in denied and subject and username and all(c.isalnum() or c in "-_" for c in subject), "Exact safe selected human subject required")
    uuid.UUID(plan["operation"])
    directory = scenario.protected_directory(directory)
    qa.write_once(directory / "intent.json", {"scope": "selected local IAM human only; app DB retained", "plan": plan, "config": cfg})
    if plan["action"] == "delete":
        before = request(cfg, "GET", "/management/v1/users/" + subject)["user"]
        selected_human(before, subject, cfg["organization_id"], username, denied)
        qa.write_once(directory / "before.json", before)
        qa.write_once(directory / "dispatch.json", {"method": "DELETE", "subject": subject, "automatic_retry": False})
        result = request(cfg, "DELETE", "/management/v1/users/" + subject)
        qa.write_once(directory / "response.json", result)
        qa.need(result.get("details", {}).get("resourceOwner") == cfg["organization_id"], "Delete result organization not proven; unknown")
        receipt = {"phase": "selected_human_removed", "subject": subject, "username": username, "organization_id": cfg["organization_id"], "project": qa.PROJECT, "owner": qa.OWNER, "logical_deleted_state_not_physical_erasure": True, "app_database_untouched_by_helper": True, "normal_API_old_subject_restore_supported": False}
    elif plan["action"] == "recreate":
        previous = plan["delete_receipt"]
        qa.need(qa.sha(previous["path"]) == previous["sha256"], "Exact delete receipt hash required")
        deleted = qa.load(previous["path"])
        qa.need(deleted["phase"] == "selected_human_removed" and (deleted["subject"], deleted["username"], deleted["organization_id"], deleted["project"], deleted["owner"]) == (subject, username, cfg["organization_id"], qa.PROJECT, qa.OWNER), "Wrong original selected-human receipt")
        body = create_body(plan["body"], username)
        try:
            request(cfg, "GET", "/management/v1/users/" + subject)
        except urllib.error.HTTPError as error:
            qa.need(error.code == 404, "Old subject deletion not established")
        else:
            raise ValueError("Old subject still exists; do not recreate")
        qa.write_once(directory / "dispatch.json", {"method": "POST", "body_sha256": __import__("hashlib").sha256(qa.canonical(body)).hexdigest(), "automatic_retry": False})
        result = request(cfg, "POST", "/management/v1/users/human", body)
        qa.write_once(directory / "response.json", result)
        qa.need(result["userId"] != subject and result["details"]["resourceOwner"] == cfg["organization_id"], "New selected human subject not proven; unknown")
        receipt = {"phase": "selected_human_recreated_NEW_subject", "old_subject": subject, "new_subject": result["userId"], "organization_id": cfg["organization_id"], "app_database_untouched_by_helper": True, "links_or_grants_restored": False, "next": "Exercise actual prelink/OIDC/bot flow; never patch old subject into app rows"}
    else:
        raise ValueError("Only separate selected-human delete or recreate is supported")
    qa.write_once(directory / "receipt.json", receipt)
    return {"one_selected_IAM_action_complete": True, "app_database_untouched": True, "acceptance_pass": False}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True)
    parser.add_argument("--plan", required=True)
    parser.add_argument("--apply-plan-sha256")
    parser.add_argument("--apply-config-sha256")
    parser.add_argument("--private-dir")
    args = parser.parse_args()
    cfg, plan = qa.validate_config(qa.load(args.config)), qa.load(args.plan)
    qa.need(plan["action"] in ("delete", "recreate"), "Unsupported IAM operation")
    if not args.apply_plan_sha256:
        print(json.dumps({"plan_only": True, "action": plan["action"], "plan_sha256": qa.sha(args.plan), "config_sha256": qa.sha(args.config), "scope": "selected local IAM human; app retained", "old_subject_restore_supported": False}))
        return
    qa.need(args.private_dir and qa.sha(args.plan) == args.apply_plan_sha256 and qa.sha(args.config) == args.apply_config_sha256, "Explicit reviewed plan/config hashes required")
    print(json.dumps(execute(cfg, plan, args.private_dir)))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"stopped": True, "error_type": type(error).__name__, "outcome_may_be_unknown": True, "no_automatic_retry": True, "next": "Preserve private intent/dispatch and inspect canonical selected user before any new action"}))
        raise SystemExit(1)
