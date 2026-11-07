"""Add/remove only zinusya.admin in the exact application API project."""
import argparse
import json
import uuid
import iam_human as iam
import operator_cli as qa
import scenario_db as scenario


def selected_grant(grant, user, project, organization):
    # Zitadel's grant GET/PUT/DELETE do not bind path userId themselves.
    qa.need(grant["id"] and grant["userId"] == user and grant["projectId"] == project and grant["details"]["resourceOwner"] == organization, "Grant owner/user/project mismatch")
    qa.need(not grant.get("projectGrantId") and grant["state"] == "USER_GRANT_STATE_ACTIVE", "Only active directly owned API-project grant")
    qa.need(isinstance(grant.get("roleKeys", []), list), "Malformed role keys")


def grant_list(found):
    qa.need(isinstance(found, dict) and "error" not in found and isinstance(found.get("details"), dict), "Successful typed grant-search details required")
    grants = found.get("result", [])
    count = found["details"].get("totalResult", 0)  # ProtoJSON uint64 zero omission.
    qa.need(type(count) is int or (isinstance(count, str) and count.isascii() and count.isdigit()), "Strict uint64 grant count required")
    count = int(count)
    qa.need(0 <= count < 1 << 64 and isinstance(grants, list) and len(grants) <= 1 and count == len(grants), "Incomplete/duplicate grant search")
    return grants


def execute(cfg, plan, private_dir):
    denied = iam.admission(cfg)
    subject, username = plan["subject"], plan["username"]
    qa.need(subject and subject not in denied and all(c.isalnum() or c in "-_" for c in subject), "Service/operator target forbidden")
    uuid.UUID(plan["operation"])
    qa.need(type(plan["desired_admin"]) is bool, "Explicit add/remove boolean required")
    app = qa.load(cfg["configuration_custody"]["path"])
    project = app["identity"]["api_audience"]
    qa.need(project and project == plan["api_project_id"], "Exact current app API project required")
    human = iam.request(cfg, "GET", "/management/v1/users/" + subject)["user"]
    iam.selected_human(human, subject, cfg["organization_id"], username, denied)
    found = iam.request(cfg, "POST", "/management/v1/users/grants/_search", {"query": {"limit": 2, "offset": 0}, "queries": [{"userIdQuery": {"userId": subject}}, {"projectIdQuery": {"projectId": project}}]})
    grants = grant_list(found)
    current = None
    if grants:
        selected_grant(grants[0], subject, project, cfg["organization_id"])
        gid = grants[0]["id"]
        qa.need(all(c.isalnum() or c in "-_" for c in gid), "Malformed grant ID")
        current = iam.request(cfg, "GET", "/management/v1/users/" + subject + "/grants/" + gid)["userGrant"]
        qa.need(current["id"] == gid, "Grant ID changed on exact read")
        selected_grant(current, subject, project, cfg["organization_id"])
    # This IAM API has no CAS. Root supplies exact observed semantic state and
    # sequence and is the exclusive IAM writer; do not claim server-atomic CAS.
    state = None if current is None else {"id": current["id"], "roleKeys": sorted(current.get("roleKeys", [])), "sequence": current["details"]["sequence"]}
    qa.need(state == plan["expected_grant"], "Selected grant state/sequence changed")
    directory = scenario.protected_directory(private_dir)
    qa.write_once(directory / "intent.json", {"plan": plan, "scope": "only zinusya.admin in directly owned API-project grant", "before": current})
    roles = set(current.get("roleKeys", [])) if current else set()
    if ("zinusya.admin" in roles) == plan["desired_admin"]:
        qa.write_once(directory / "receipt.json", {"no_change": True, "no_write_request": True})
        return {"no_change": True}
    if plan["desired_admin"]:
        roles.add("zinusya.admin")
    else:
        roles.remove("zinusya.admin")
    if current is None:
        method, path, body = "POST", "/management/v1/users/" + subject + "/grants", {"projectId": project, "roleKeys": sorted(roles)}
    else:
        path = "/management/v1/users/" + subject + "/grants/" + current["id"]
        method, body = ("PUT", {"roleKeys": sorted(roles)}) if roles else ("DELETE", None)
    qa.write_once(directory / "dispatch.json", {"method": method, "path": path, "body": body, "no_retry": True})
    result = iam.request(cfg, method, path, body)
    qa.write_once(directory / "response.json", result)
    qa.write_once(directory / "receipt.json", {"write_response_saved": True, "expected_role_keys": sorted(roles), "no_other_project_or_org_role_change_requested": True, "acceptance_pass": False, "next": "Fresh exact grant search/read then real app-admin JWT/introspection/UI; no impersonation"})
    return {"write_response_saved": True, "acceptance_pass": False}


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
        print(json.dumps({"plan_only": True, "plan_sha256": qa.sha(args.plan), "config_sha256": qa.sha(args.config), "only_role": "zinusya.admin", "no_ORG_OWNER_or_Loom_role": True}))
        return
    qa.need(args.private_dir and qa.sha(args.plan) == args.apply_plan_sha256 and qa.sha(args.config) == args.apply_config_sha256, "Exact reviewed plan/config hashes required")
    print(json.dumps(execute(cfg, plan, args.private_dir)))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"stopped": True, "error_type": type(error).__name__, "outcome_may_be_unknown": True, "no_automatic_retry": True}))
        raise SystemExit(1)
