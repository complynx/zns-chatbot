# Meal original-operation handoff: isolated acceptance

## Exact candidate and scope

Source `ff8d4234337891298befd3136cedeb17602026a9`, local Linux image
`sha256:87103b826849908d4ea7dded4d08bc6a9a3371a8981d6cf8d835b311c11dcf1c`,
passes the bounded behavior below. The standard Dockerfile used a tracked-only
Git archive; no private build inputs were included. All six jobs in
[CI39881779939](https://github.com/complynx/zns-chatbot/actions/runs/39881779939)
passed at that source. This report and its links are documentation changes only.

The fixture used actual IAM4.20, actual Loom revision
`ba1f4a11237a07edb242735b09edb210e2a7209e` (API/sandbox/proxy digest `cbba225f`,
file worker `856d5968`), and actual Edge `155.0.4283.62`. Telegram and model
responses were controlled fixtures. This is not autonomous model, real Telegram
WebView, server-pilot or production acceptance. The local image is unpublished.

## Observed transition

A fresh synthetic participant had meals enabled and service orders disabled.
Supported native APIs saved a meal draft and prepared backend operation N with
proposal P. An actual Loom `module_call` adopted N into distinct Loom operation L.
Original arguments, description, proposal, identity binding and expiry were
retained. Inspection created no order. Mock Telegram received the complete meal
composition, prices, total, event-local deadlines, reply keyboard and inline
`#meals` link carrying N.

The authenticated shipped HTML/app.js/meals.js were loaded in actual Edge.
Unmodified served asset bytes matched the exact Git archive. Before an explicit
click, the form displayed the frozen composition, line amounts, total7.15 BYN,
service times, cutoff and confirmation expiry in Europe/Minsk. One browser
business POST submitted N/P with the frozen versions and created one meal order.
It cleared the consumed draft once, without evidence, allocation or confirmed
payment. Browser session bootstrap and knowledge reads are counted separately
from this business POST.

While the original question was still live, an explicit ordinary-text followup
entered the same Loom run. A controlled script read the original submit receipt;
`interaction_resolve` used that same input/approval/argument hash. L succeeded
with N's original result. Complete order/receipt/draft/evidence/allocation rows
remained unchanged through reconciliation. Idempotent backend RPCs are allowed;
this is not a claim of zero HTTP POSTs throughout the system.

The same run completed. Reopening the original link in a fresh Edge context
showed that original order, disabled confirmation and made zero business POSTs.
Both browser contexts closed without page errors or denied requests. Root
visually inspected both screenshots. Final state had one order at version1,
one submit receipt for N, original prepare receipt, draft version advanced once,
zero evidence/allocations and no undelivered outbox. Exactly two completed tasks
represented the initial request and routed text input; both targeted the one
completed original Loom run.

## Retained harness failures and final observation

The first fixture stopped before browser navigation/authentication/business
requests: its verifier compared image LF bytes with Windows working-tree CRLF.
Read-only comparison proved all three assets equivalent to the tracked archive.
Its failed checkpoint and decoded database archives remain preserved. That
controller was not replayed. A fresh separately reviewed fixture used strict
archive-byte pins; remote bytes were never normalized.

The second fixture completed the observed transition, but its final harness
assertion incorrectly required one task row. A text followup legitimately creates
a separate routed input task. The original run remains recorded as failed at
that assertion; it was not replayed or relabeled successful. A separately
reviewed read-only observer checked the existing original run, both task target
links, browser receipts, N/P/result, domain rows, outbox, ownership/runtime pins
and paid-ledger hashes. It passed and wrote a separate final observation without
business mutation. Acceptance rests on that actual behavior plus the independent
final read, not a claim that either original harness completed successfully.

Each fixture's three databases were archived and fully decoded before removing
only its owned services, volumes and network. Original checkpoints remained
unchanged; zero owned resources remained. No paid generation was made. The
existing campaign stayed HELD21/21 with unchanged control/ledger bytes.

Local evidence (ignored, synthetic/private details are not published):

- `meal-reverse-build/acceptance.json`: exact archive/image/build pins.
- `meal-reverse-runtime/private/checkpoint.json` and `cleanup.json`: first failure.
- `meal-reverse-runtime-r2/private/checkpoint.json`: original final-assertion failure.
- `meal-reverse-runtime-r2/private/final-observation.json`: independent final read.
- `meal-reverse-runtime-r2/private/cleanup.json`: decoded archives and owned cleanup.
- `meal-reverse-runtime-r2/acceptance.json`: aggregate verdict and evidence hashes.

Paths are relative to `.cache/shuttle-work/parallel-acceptance-20261022/`.
Do not replay the one-shot run, observer or cleanup scripts.

## Reused evidence and remaining boundary

The earlier dd06/a2fa native preparation to actual Loom consent/submission gate
remains separately accepted in the
[original-operation contract](shuttle-order-operation-adoption.md#meal-original-operation-acceptance).
The ff8 UI change passed98 focused Node cases, two affected PostgreSQL roots,
scoped lint and two independent source reviews. Existing service-order and
IAM4.20 evidence is reused; no full compatibility matrix was repeated locally.

This bounded happy transition does not prove every contention/fault window,
real Telegram interaction, target-model voice, publication or production setup.
Those remaining gates and authorizations retain their existing scope.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
