"""One raw synthetic archive upload against an existing exact admin preparation."""
import argparse
import datetime as dt
import hashlib
import json
import pathlib
import ssl
import urllib.request
import uuid
import operator_cli as qa
import scenario_db as scenario


def raw_arguments(arguments, digest):
    qa.need(isinstance(arguments, dict) and set(arguments) <= {"sha256", "source_file_id"} and arguments.get("sha256") == digest, "Original raw archive SHA/arguments changed")
    # Go uuid.UUID is a fixed array: omitempty may serialize its nil value.
    qa.need(arguments.get("source_file_id", str(uuid.UUID(int=0))) == str(uuid.UUID(int=0)), "Raw upload cannot use a real source file preparation")


def upload(cfg, plan, private_dir):
    qa.runtime_guard(cfg)
    qa.idle_tasks(cfg)
    scenario.fences(cfg)
    backup = cfg["backup_receipt"]
    qa.need(qa.sha(backup["path"]) == backup["sha256"], "App backup receipt changed")
    saved = qa.load(backup["path"])
    qa.need(saved["project"] == qa.PROJECT and saved["owner"] == qa.OWNER and saved["fully_decoded"] is True, "Decoded local app backup required")
    age = dt.datetime.now(dt.timezone.utc) - dt.datetime.fromisoformat(saved["created_at"])
    qa.need(dt.timedelta(0) <= age < dt.timedelta(minutes=30), "Fresh app backup required")
    operation, proposal = str(uuid.UUID(plan["operation"])), str(uuid.UUID(plan["proposal"]))
    archive = plan["archive"]
    qa.need(qa.sha(archive["path"]) == archive["sha256"] and pathlib.Path(archive["path"]).stat().st_size <= 8 << 20, "Exact bounded synthetic archive required")
    preparation = plan["preparation"]
    qa.need(qa.sha(preparation["path"]) == preparation["sha256"], "Reviewed original preparation response required")
    prepared = qa.load(preparation["path"])
    qa.need(prepared["status"] == "prepared" and prepared["operation_id"] == operation and prepared["preconditions"]["proposal_id"] == proposal, "Original actor/proposal changed")
    raw_arguments(prepared["arguments"], archive["sha256"])
    qa.need(dt.datetime.fromisoformat(prepared["expires_at"].replace("Z", "+00:00")) > dt.datetime.now(dt.timezone.utc) + dt.timedelta(seconds=20), "Preparation already expired or too close")
    directory = scenario.protected_directory(private_dir)
    qa.write_once(directory / "intent.json", {"plan": plan, "project": qa.PROJECT, "owner": qa.OWNER})
    qa.need(qa.sha(cfg["bearer_file"]) == cfg["bearer_sha256"], "Original admin credential custody changed")
    token = pathlib.Path(cfg["bearer_file"]).read_text().strip()
    qa.need(token and not any(c.isspace() for c in token), "Invalid admin bearer")
    raw = pathlib.Path(archive["path"]).read_bytes()
    qa.need(hashlib.sha256(raw).hexdigest() == archive["sha256"] and raw.startswith(b"\x1f\x8b"), "Archive changed before upload")
    context = ssl.create_default_context(cafile=cfg["ca_file"])
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), qa.NoRedirect(), urllib.request.HTTPSHandler(context=context))
    headers = {"Authorization": "Bearer " + token, "Content-Type": "application/gzip", "X-Operation-Id": operation, "Idempotency-Key": operation, "X-Proposal-Id": proposal, "X-Content-Sha256": archive["sha256"]}
    url = cfg["base_url"].rstrip("/") + "/internal/admin/legacy/store"
    qa.write_once(directory / "dispatch.json", {"operation": operation, "proposal": proposal, "archive_sha256": archive["sha256"], "automatic_retry": False})
    try:
        with opener.open(urllib.request.Request(url, raw, headers, method="POST"), timeout=40) as response:
            qa.need(response.status == 200 and response.geturl() == url, "Unknown archive upload outcome")
            body = response.read(1_048_577)
            qa.need(len(body) <= 1_048_576, "Archive response too large")
        result = json.loads(body)
        qa.write_once(directory / "response.json", result)
        return {"upload_response_saved": True, "acceptance_pass": False, "next": "Read original archive status, validate stored bytes, separately prepare/confirm history application"}
    except Exception as error:
        qa.write_once(directory / "unknown.json", {"unknown": True, "error_type": type(error).__name__, "operation": operation})
        raise


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
        print(json.dumps({"plan_only": True, "plan_sha256": qa.sha(args.plan), "config_sha256": qa.sha(args.config), "scope": "one prepared synthetic archive; no account/action/payment creation"}))
        return
    qa.need(args.private_dir and qa.sha(args.plan) == args.apply_plan_sha256 and qa.sha(args.config) == args.apply_config_sha256, "Exact reviewed plan/config/private directory required")
    print(json.dumps(upload(cfg, plan, args.private_dir)))


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        print(json.dumps({"stopped": True, "error_type": type(error).__name__, "no_automatic_retry": True}))
        raise SystemExit(1)
