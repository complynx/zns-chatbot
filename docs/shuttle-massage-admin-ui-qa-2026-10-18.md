# Native massage administrator form source QA

Source: schema48 / manifest `mvp-54`, uninstalled. Owned runtime: schema47 /
`mvp-51`. This accepts the administrative Mini App component, not real-service
specialist/admin workflow, unattended inference, full Python parity or production.
No backend contract, dependency or migration was added in this stage.

## Checks

- Six Linux Go 1.27.2/PostgreSQL 17 race roots pass: four binding HTTP cases cover
  native/agent canonical handoff and original status on one DB connection, exact
  confirmation/expiry/origin/idempotency rejection, current administrator checks,
  and revalidation after an event-lock wait. API checks cover explicit false/zero
  wire fields and opted-in bounded public assets including the new script.
- All 167 Mini App JS tests pass with process timezone Pacific/Honolulu; 24 cover
  this form. Happy/critical paths include exact native grant/revoke, immutable alias
  owner, removed alias/module-off/revoked target revocation, safe target lookup,
  original canonical proposal adoption, uncertain prepare/apply and reload recovery,
  same operation/body retries, expiry, historical receipt versus newer binding,
  role/session loss, storage failure, stale replies, pagination and app lifecycle.
- API lint reports zero issues. Independent correctness and skeptical quality review
  found a revoke schedule-version mismatch and hidden startup error/recovery paths.
  They were verified against the API, fixed and covered. Fresh final textual review
  reports no actionable findings.
- Frozen source and all ten main Compose fingerprints remain unchanged during QA.
  Disposable Go/PostgreSQL memory limits are 2 GiB/512 MiB with no additional swap;
  no OOM occurred. Exact containers and the private database volume were removed.

The initial 18-case UI run had three failures caused by the DOM fixture preserving
textContent across replaceChildren. The fixture was corrected to browser semantics;
product clearing was already correct. Tests expanded to 22, then 24 after review.
The final 24-case source and full 167-case gate pass. Do not rewrite those initial
failures as defects found in production or an initially green run.

## Manual mock-browser acceptance

Actual product HTML/CSS/JS ran against an agent-owned loopback mock at port18585.
The preview exposed fixture controls separately from the product form. Verified:
readable preview and zero effect before confirmation; grant then revoke of the same
locked account; Russian preview; continuation of a synthetic agent-created proposal;
a 503 after the next effect, then page reload recovering its original receipt with
the effect counter still three; visible safe startup-outage state; backend-reported
expiry disabling confirmation; and hiding/clearing data after administrator loss.
The original result is shown separately from current access. The expired local
preview could be closed after service restoration. Desktop layout was visually
inspected; no claim is made about every viewport. The temporary tab and server
were closed. Telegram and actual IAM permissions were not changed by this QA.

This proves the real UI against mocks, not a real Loom conversation or signed
Telegram embedded launch. The six Go roots separately exercise actual HTTP/Store
with real PostgreSQL and mock peers. Those scopes must not be conflated.

## Recovery and next gate

The form persists only owner/event/operation/proposal identifiers and phase; it does not
store the target selection separately, its display data or original arguments. Reload reads original
arguments with current administrator/session authorization. The server's canonical
operation may differ from the locally adopted key, which remains the retry key.
Missing status remains unknown; recovery does not send a replacement operation or
automatically apply an effect. Unknown preparation without retrievable arguments
has check/close controls rather than an invented retry body. Closing a preview does
not cancel its server proposal.

Evidence: `.cache/shuttle-work/massage-admin-ui-v1-qa/`, the ignored master
`massage-staff-work.json`, and exact code/doc diffs in the local temporary folder.
The earlier complete staff-source gate remains 48 roots/57 events; this six-root
selection is the additional focused HTTP/asset check for the new form.

Next prepare a backup and rollback plan, install only on the owned test stand, and
exercise real IAM/Loom/Telegram administrator and specialist native, agent and mixed
paths. Source components are ready for that test gate; complete feature acceptance
is still pending. Production deployment, publication, commit/push/PR remain outside
this stage's release authorization.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
