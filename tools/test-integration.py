"""Run PostgreSQL integration tests in bounded processes with exact coverage."""

import argparse
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[1]
PACKAGE = "github.com/complynx/zns-chatbot/integration"
BATCH_SIZE = 100
DISCOVERY_TIMEOUT = 300
BATCH_TIMEOUT = 720


def run_logged(command, stdout, stderr, timeout):
    """Bound compilation and execution together; retain partial output on timeout."""
    with stdout.open('w') as output, stderr.open('w') as errors:
        process = subprocess.Popen(command, cwd=ROOT, stdout=output, stderr=errors,
                                   start_new_session=os.name != 'nt')
        try:
            return process.wait(timeout=timeout), False
        except BaseException as error:
            # Go can leave a compiler/test child alive when only its parent is killed.
            if os.name == 'nt':
                try:
                    subprocess.run(['taskkill', '/PID', str(process.pid), '/T', '/F'],
                                   stdout=errors, stderr=errors, timeout=5, check=True)
                finally:
                    if process.poll() is None:
                        process.kill()
                    process.wait(timeout=5)
            else:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait(timeout=5)
            if not isinstance(error, subprocess.TimeoutExpired):
                raise
            errors.write(f'Wall-clock timeout after {timeout} seconds; process tree terminated.\n')
            return 124, True


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



def partition(names, index, count):
    """Spread sorted test roots across independent PostgreSQL workers."""
    if count < 1 or not 0 <= index < count:
        raise ValueError("shard index must be in [0, shard count)")
    return names[index::count]


def verify_shards(summaries, count):
    """Require all workers and exactly one successful execution of every root."""
    if count < 1 or len(summaries) != count:
        raise ValueError("missing or extra shard summaries")
    discovered = summaries[0]["discovered"]
    if (not discovered or discovered != sorted(set(discovered))
            or any(not re.fullmatch(r"Test\w+", name) for name in discovered)):
        raise ValueError("invalid test discovery")
    seen = set()
    completed = []
    for summary in summaries:
        index = summary["shard_index"]
        if summary["shard_count"] != count or index in seen:
            raise ValueError("duplicate shard or inconsistent shard count")
        selected = partition(discovered, index, count)
        if summary["discovered"] != discovered or summary["selected"] != selected:
            raise ValueError("shards disagree on discovery or partition")
        expected = [name for batch in summary["batches"] for name in batch["expected"]]
        passed = [name for batch in summary["batches"] for name in batch["passed_roots"]]
        if (summary["passed"] is not True or expected != selected
                or sorted(passed) != sorted(selected)):
            raise ValueError("incomplete shard coverage")
        for batch in summary["batches"]:
            if (batch["passed"] is not True or batch["exit_code"] != 0
                    or batch["skips"] or batch["failures"] or batch["unexpected_roots"]
                    or batch["malformed_output"] or batch["timed_out"]):
                raise ValueError("failed, skipped or incomplete batch")
        seen.add(index)
        completed.extend(passed)
    if seen != set(range(count)) or sorted(completed) != discovered:
        raise ValueError("missing, duplicate or unexpected test roots")
    return len(completed)

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="New directory for retained test evidence")
    parser.add_argument("--shard-index", type=int, default=0)
    parser.add_argument("--shard-count", type=int, default=1)
    parser.add_argument("--verify-shards", type=int, metavar="COUNT",
                        help="Verify downloaded shard evidence instead of running Go")
    args = parser.parse_args()
    if args.verify_shards is not None:
        try:
            summaries = [json.loads(path.read_text())
                         for path in sorted(args.output.glob("*/summary.json"))]
            total = verify_shards(summaries, args.verify_shards)
        except (OSError, ValueError, KeyError, TypeError) as error:
            print(f"Integration coverage rejected: {error}", file=sys.stderr)
            return 1
        print(f"Integration coverage: {total} roots passed across {args.verify_shards} shards")
        return 0
    try:
        partition([], args.shard_index, args.shard_count)
    except ValueError as error:
        parser.error(str(error))
    if not (os.environ.get("SHUTTLE_TEST_DATABASE_URL") or os.environ.get("SHUTTLE_TEST_DATABASE_DSN")):
        parser.error("SHUTTLE_TEST_DATABASE_URL is required; skips are not acceptance")
    args.output.mkdir(parents=True, exist_ok=False)
    listing_code, listing_timeout = run_logged(
        ["go", "test", "-race", "-mod=readonly", "-list", "^Test", "./integration"],
        args.output / "discovery.stdout.log", args.output / "discovery.stderr.log", DISCOVERY_TIMEOUT)
    names = sorted(line for line in (args.output / "discovery.stdout.log").read_text().splitlines()
                   if re.fullmatch(r"Test\w+", line))
    if listing_code or not names or len(names) != len(set(names)):
        (args.output / "summary.json").write_text(json.dumps(
            {"passed": False, "discovery": {"exit_code": listing_code, "timed_out": listing_timeout}}, indent=2) + "\n")
        print("Integration test discovery failed; see discovery logs", file=sys.stderr)
        return 1
    discovered = names
    names = partition(discovered, args.shard_index, args.shard_count)
    summary = {"passed": False, "discovered": discovered, "selected": names,
               "shard_index": args.shard_index, "shard_count": args.shard_count, "batches": []}
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
        exit_code, timed_out = run_logged(command, stem.with_suffix(".jsonl"),
                                         stem.with_suffix(".stderr.log"), BATCH_TIMEOUT)
        with stem.with_suffix(".jsonl").open() as log:
            for line in log:
                try:
                    events.append(json.loads(line))
                except ValueError:
                    malformed = True
        result = verify(events, selected, exit_code)
        result.update(batch=number, malformed_output=malformed, timed_out=timed_out)
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
