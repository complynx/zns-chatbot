"""Run PostgreSQL integration tests in bounded processes with exact coverage."""

import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = "github.com/complynx/zns-chatbot/integration"
BATCH_SIZE = 100


def verify(events, expected, exit_code):
    roots = [event for event in events if event.get("Test") in expected
             and event.get("Action") in ("pass", "fail", "skip")]
    passed = [event["Test"] for event in roots if event["Action"] == "pass"]
    skips = [event.get("Test") for event in events if event.get("Action") == "skip"]
    failures = [event.get("Test", event.get("Package")) for event in events
                if event.get("Action") == "fail"]
    unexpected = sorted({event["Test"] for event in events
                         if event.get("Test") and "/" not in event["Test"]
                         and event["Test"] not in expected})
    package_pass = any(event.get("Package") == PACKAGE and not event.get("Test")
                       and event.get("Action") == "pass" for event in events)
    return {"passed": exit_code == 0 and package_pass and not skips and not failures
            and not unexpected and len(passed) == len(expected) and set(passed) == set(expected),
            "exit_code": exit_code, "expected": expected, "passed_roots": passed,
            "skips": skips, "failures": failures, "unexpected_roots": unexpected}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="New directory for retained test evidence")
    args = parser.parse_args()
    if not (os.environ.get("SHUTTLE_TEST_DATABASE_URL") or os.environ.get("SHUTTLE_TEST_DATABASE_DSN")):
        parser.error("SHUTTLE_TEST_DATABASE_URL is required; skips are not acceptance")
    args.output.mkdir(parents=True, exist_ok=False)
    listing = subprocess.run(["go", "test", "-race", "-mod=readonly", "-list", "^Test", "./integration"],
                             cwd=ROOT, capture_output=True, text=True)
    (args.output / "discovery.stdout.log").write_text(listing.stdout)
    (args.output / "discovery.stderr.log").write_text(listing.stderr)
    names = sorted(line for line in listing.stdout.splitlines() if re.fullmatch(r"Test\w+", line))
    if listing.returncode or not names or len(names) != len(set(names)):
        print("Integration test discovery failed; see discovery logs", file=sys.stderr)
        return 1
    summary = {"passed": False, "selected": names, "batches": []}
    (args.output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    for number, offset in enumerate(range(0, len(names), BATCH_SIZE), 1):
        selected = names[offset:offset + BATCH_SIZE]
        pattern = "^(" + "|".join(re.escape(name) for name in selected) + ")$"
        command = ["go", "test", "-race", "-timeout", "10m", "-mod=readonly", "-count=1",
                   "-parallel", "2", "-json", "-run", pattern, "./integration"]
        events = []
        malformed = False
        stem = args.output / f"batch-{number:02d}"
        print(f"Integration batch {number}: {len(selected)} roots", flush=True)
        with stem.with_suffix(".jsonl").open("w") as log, stem.with_suffix(".stderr.log").open("w") as errors:
            with subprocess.Popen(command, cwd=ROOT, stdout=subprocess.PIPE, stderr=errors,
                                  text=True) as process:
                for line in process.stdout:
                    log.write(line)
                    try:
                        events.append(json.loads(line))
                    except ValueError:
                        malformed = True
                exit_code = process.wait()
        result = verify(events, selected, exit_code)
        result.update(batch=number, malformed_output=malformed)
        result["passed"] = result["passed"] and not malformed
        summary["batches"].append(result)
        (args.output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
        if not result["passed"]:
            print("".join(event.get("Output", "") for event in events), flush=True)
            print(stem.with_suffix(".stderr.log").read_text(), file=sys.stderr, flush=True)
        print(f"Batch {number}: {'PASS' if result['passed'] else 'FAIL'}; "
              f"{len(result['passed_roots'])}/{len(selected)} roots passed", flush=True)
    summary["passed"] = all(batch["passed"] for batch in summary["batches"])
    (args.output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
