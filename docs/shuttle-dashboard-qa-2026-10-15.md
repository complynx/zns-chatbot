# Operations dashboard acceptance — 2026-10-15

Scope: the importable thirteen-panel [dashboard](shuttle-dashboard.md), using
existing private metrics. No Go code, metric, DB schema or production telemetry
configuration changed. Architect and senior Go consultation preceded the work.

Twelve behavioral cases run actual expressions extracted from the saved JSON
through pinned Prometheus 3.15.0 promtool. They passed for healthy empty/nonzero
snapshots, retained values after a failed collector, never-successful collection,
missing metadata, frozen timestamps, stale age, down/absent scrape, another
deployment, cache ratio and histogram observations. Undefined idle ratio/p95
remain undefined rather than becoming zero. CI now contains this query check;
the local workflow changes have not been pushed or run remotely.

Initial source correctness and quality reviews found no actionable issues.
Browser acceptance later exposed an interpolation bug missed by those reviews:
`:regex` supplied an escape that was invalid inside the PromQL string. The QA
helper had manually added another escape, masking the defect in backend tests.
The dashboard now uses exact instance comparison with `:doublequote`; both test
paths use that same literal value. Previous API-only evidence was retained and
invalidated. Promtool, every native panel query and actual browser rendering then
passed with the new artifact. A fresh source review found no actionable issues.

## Native query and visual acceptance

Separate, bounded Grafana OSS 13.2.1, Prometheus and synthetic exporter containers
ran on their own labelled QA network. Host ports bound only to loopback. Grafana
had a disposable admin credential and local anonymous Viewer access. No alerts,
notification routes or Cloud resources were installed. The existing bot and Loom
were not restarted or reconfigured.

The dashboard's exact panel/query definitions were imported. Only the QA copy's
variable configuration selected the local datasource and added synthetic/missing
instance choices. All thirteen panels executed through Grafana with their saved
instant/range modes against both real owned-stand metrics and synthetic nonzero
metrics. Synthetic values were checked, including task count 7, outbox count 2,
due count 1, pool connections 2/3/5/8, callback p95 1.75 seconds, positive-cache
hit ratio 0.75 and positive fallback/worker rates.

Manual CUA browser acceptance confirmed the real target's fresh statuses and all
synthetic chart groups. Controlled exporter phases proved:

- Task collector failure, frozen timestamp and never-successful collection:
  tasks Unavailable, while outbox and queue remained Fresh. Current task reads
  were absent; retained historical charts were not rewritten as zero backlog.
- Scrape failure: scrape and all snapshot families Unavailable.
- Missing deployment: Unknown, with no values inherited from the previous choice.
- Datasource connection refusal: explicit panel errors and Unknown, never Fresh.
- Recovery of the same datasource/exporter: Fresh statuses and original values.

Eleven screenshots and per-query/phase JSON results are retained in the ignored
local `dashboard-qa` directory. No Telegram message, Loom inference or business
write was needed. Counts and row hashes of every `shuttle_*` table matched the
pre-QA baseline. This does not establish application health, a business-latency
SLO, production dashboard activation, or notification delivery.

## Retained QA failures

Grafana initially stopped its bundled Prometheus plugin while trying to update
it, then failed the update against the read-only filesystem. Queries returned
404 `plugin.notRegistered`. Logs and stopped state were preserved. Only the
disposable Grafana was recreated with plugin preinstallation and auto-update
disabled, then the same dashboard/datasource was restored. This was a QA service
configuration failure, separate from the interpolation defect above.

The initial fingerprint comparison also compared unordered SQL result lists.
Per-table counts/hashes were identical; sorting those rows corrected the verifier.
Earlier promtool attempts retained failures for NaN input/comparison and a missing
temporary writable directory. None was relabelled as a successful attempt.

All disposable dashboard containers and their QA network were removed after
evidence capture. Grafana, Prometheus and the final promtool runner exited 0
without OOM. The synthetic Python exporter required the stop timeout's kill and
exited 137 without OOM; this is not graceful exporter-shutdown acceptance. Bot
and Loom stayed running, and the user-owned Telegram tab was left open.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
