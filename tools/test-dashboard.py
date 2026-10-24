"""Generate promtool behavioral cases from the shipped dashboard expressions."""

import argparse
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
JOB = "integrations/zinusya"
INSTANCE = "complynx.net"
LABELS = {"job": JOB, "instance": INSTANCE}


def series(name, value, **labels):
    pairs = {**LABELS, **labels}
    metric = name + "{" + ",".join(f"{k}={json.dumps(v)}" for k, v in sorted(pairs.items())) + "}"
    return {"series": metric, "values": str(value) + "+0x10"}


def sample(value, **labels):
    return {"labels": "{" + ",".join(f"{k}={json.dumps(v)}" for k, v in sorted(labels.items())) + "}", "value": value}


def generate(output):
    dashboard = json.loads((ROOT / "deploy/shuttle/observability/dashboard.json").read_text())
    panels = {p["title"]: p for p in dashboard["panels"]}
    assert len(panels) == len(dashboard["panels"]), "Panel titles must be distinct"
    for panel in panels.values():
        assert panel["datasource"] == {"type": "prometheus", "uid": "${datasource}"}
        if panel["type"] == "stat":
            assert all(t["instant"] and not t["range"] for t in panel["targets"])
            assert panel["options"]["reduceOptions"]["calcs"] == ["last"]
        else:
            assert panel["fieldConfig"]["defaults"]["custom"]["spanNulls"] is False

    def check(title, expected, ref="A"):
        target = next(t for t in panels[title]["targets"] if t["refId"] == ref)
        expression = target["expr"].replace("${instance:doublequote}", json.dumps(INSTANCE)).replace("$__rate_interval", "5m")
        assert "$" not in expression, "Unexpanded dashboard variable"
        if expected and all(s["value"] == "NaN" for s in expected):
            # NaN is unequal to itself; promtool's sample comparator cannot match it directly.
            expression = f"({expression}) != bool ({expression})"
            expected = [dict(s, value=1) for s in expected]
        return {"expr": expression, "eval_time": "10m", "exp_samples": expected}

    fixture = [series("up", 1)]
    for family in ("tasks", "outbox", "queue"):
        for suffix, value in (("collection_success", 1), ("last_success_timestamp_seconds", 600), ("snapshot_age_seconds", 0)):
            fixture.append(series(f"zinusya_{family}_{suffix}", value))
    values = [
        ("Unresolved tasks", "zinusya_tasks_unresolved", 7, {"state": "awaiting_user"}),
        ("Oldest task acceptance age", "zinusya_tasks_oldest_accepted_age_seconds", 30, {"state": "awaiting_user"}),
        ("Unfinished outbox chunks", "zinusya_outbox_unresolved", 2, {"state": "retryable"}),
        ("Oldest outbox creation age", "zinusya_outbox_oldest_created_age_seconds", 50, {"state": "retryable"}),
        ("Due River jobs", "zinusya_queue_due", 1, {"kind": "shuttle_task", "state": "retryable"}),
        ("Oldest scheduling lateness", "zinusya_queue_oldest_scheduled_age_seconds", 300, {"kind": "shuttle_task", "state": "retryable"}),
    ]
    fixture.extend(series(name, value, **labels) for _, name, value, labels in values)
    tests = []

    def case(name, inputs, checks):
        tests.append({"name": name, "interval": "1m", "input_series": inputs, "promql_expr_test": checks})

    case("healthy nonzero snapshots", fixture, [check(title, [sample(value, __name__=name, **LABELS, **labels)]) for title, name, value, labels in values] + [
        check("Current snapshot freshness", [sample(1, __name__="up", **LABELS)], ref) for ref in "ABC"
    ])
    for name, field, replacement in (
        ("failed collection with retained data", "collection_success", 0),
        ("never successful", "last_success_timestamp_seconds", 0),
        ("frozen timestamp with falsely fresh age", "last_success_timestamp_seconds", 100),
        ("stale snapshot age", "snapshot_age_seconds", 121),
        ("missing freshness field", "snapshot_age_seconds", None),
    ):
        changed = [s for s in fixture if not s["series"].startswith("zinusya_tasks_" + field + "{")]
        if replacement is not None:
            changed.append(series("zinusya_tasks_" + field, replacement))
        case(name, changed, [check(values[0][0], []), check(values[1][0], []),
             check("Current snapshot freshness", [sample(0, **LABELS)]),
             check(values[2][0], [sample(2, __name__=values[2][1], **LABELS, state="retryable")])])
    for name, inputs, status in (
        ("scrape down", [s for s in fixture if not s["series"].startswith("up{")] + [series("up", 0)], [sample(0, __name__="up", **LABELS)]),
        ("target absent", [s for s in fixture if not s["series"].startswith("up{")], []),
    ):
        case(name, inputs, [check(title, []) for title, _, _, _ in values] + [check("Metrics scrape", status)])
    empty = [dict(s) for s in fixture]
    for row in empty:
        if any(row["series"].startswith(metric + "{") for _, metric, _, _ in values):
            row["values"] = "0+0x10"
    case("healthy empty snapshots are actual zeros", empty, [check(title, [sample(0, __name__=name, **LABELS, **labels)]) for title, name, _, labels in values])
    foreign = [dict(s, series=s["series"].replace(INSTANCE, "unrelated.example")) for s in fixture]
    case("unrelated deployment excluded", foreign, [check(title, []) for title, _, _, _ in values] + [check("Metrics scrape", [])])

    traffic = [series("up", 1)]
    for outcome, increment in (("hit", 3), ("miss", 1)):
        traffic.append(dict(series("zinusya_identity_positive_cache_lookups_total", 0, outcome=outcome), values=f"0+{increment}x10"))
    for le, increment in (("1", 8), ("2", 10), ("+Inf", 10)):
        traffic.append(dict(series("zinusya_worker_attempt_duration_seconds_bucket", 0, kind="shuttle_task", outcome="returned", le=le), values=f"0+{increment}x10"))
    traffic.append(dict(series("zinusya_worker_attempt_duration_seconds_count", 0, kind="shuttle_task", outcome="returned"), values="0+10x10"))
    case("positive cache traffic and callback histogram", traffic, [
        check("Positive introspection cache hit ratio", [sample(.75)]),
        check("Returned callback p95", [sample(1.75, kind="shuttle_task")]),
        check("Returned worker callbacks / second", [sample(1/6, kind="shuttle_task", outcome="returned")]),
    ])
    idle = [dict(s, values="0+0x10") if not s["series"].startswith("up{") else s for s in traffic]
    case("no cache or histogram observations remain undefined", idle, [
        check("Positive introspection cache hit ratio", [sample("NaN")]),
        check("Returned callback p95", [sample("NaN", kind="shuttle_task")]),
    ])
    output.mkdir(parents=True, exist_ok=True)
    text = json.dumps({"evaluation_interval": "1m", "tests": tests}, indent=2)
    (output / "dashboard.test.yaml").write_text(text + "\n")
    print(f"Generated {len(tests)} dashboard query behavior cases from {len(panels)} shipped panels")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path, help="Disposable directory for generated promtool input")
    generate(parser.parse_args().output)
