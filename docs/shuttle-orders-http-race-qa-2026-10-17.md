# Orders: native-first shared proposal and overlapping HTTP requests

This is a real HTTP/API acceptance probe on owned `shuttle-live-test`, schema40
and the accepted image42a605…1a40. Native login was a locally signed Telegram HMAC
fixture for the existing permitted user8968148043/bot6087685431. Bootstrap still
used real Zitadel. The agent endpoint used a real Zitadel owner token directly.
This probe did not run through Telegram Web, Loom or a model. Actual browser
acceptance remains documented separately; this fixture is not substituted for it.

## Runtime proof

The isolated synthetic native-to-agent event already had one submitted order and
an empty draft2. The native API saved two soup portions as draft3, then prepared
submission. The agent API prepared the same versions afterward. Both operations
referenced the same preparation ID; both immutable preparation payloads equalled
the native result. Thus this probe covers the reverse preparation direction
(native first) through actual APIs.

A dedicated psql transaction locked only that exact synthetic event row. It used
a unique application name, recorded PID/backend-start identity, a3s statement
timeout and10s idle-in-transaction timeout. Two worker threads then sent the
already-confirmed native and agent requests using different operation IDs.
Before either HTTP call completed, PostgreSQL showed two distinct active Lock
waiters in the blocker chain rooted at that transaction: transactionid and tuple.
The observation occurred0.266s after launch. This is observed server overlap,
not a conclusion from sleeping or merely launching two clients. Attribution to
those two requests relies on exclusive use of this disposable stand.

The blocker was released within the native endpoint's5s request budget. Both
real HTTP calls completed successfully and returned identical complete business
payloads: order `ba659c93-298e-46b4-ba2a-75dc04028971`, exact snapshot/time, and
empty draft4. Total25.68 BYN comprised soup2 x12.34 plus the1.00 service charge.
Order count increased by one from its baseline, not two. The stored draft equalled
the returned cleared selection. Two immutable submit receipts, trusted sources
`miniapp` and `agent`, referenced the same proposal/order and exact result.

No bot, poller, Loom or companion fingerprint changed. No image, configuration,
container, volume or privilege was created. The transient lock backend exited;
a separate final read confirmed no backend with that exact application name.
Only the new fixture session was revoked and retained as history. Existing
business records and earlier test scenarios were retained. No Telegram message,
production/developer resource, commit, push, PR or public issue was changed.

## QA helper review and cleanup

The helper `.cache/shuttle-work/orders_http_race_real_qa.py` is source-hash-bound
and has its own ignored checkpoint. No credentials, bootstrap nonce or signed
initData were persisted or printed; only the proof hash/session ID needed for
exact fixture cleanup were recorded. Session revocation is scoped by exact
bot/user/proof hash. Backend termination fallback matches PID, application name,
backend start and database, preventing accidental cleanup of another session.

Independent initial review found that a failure removing the blocker could skip
fixture revocation. Cleanup now attempts barrier release, logout and exact fixture
revocation independently, recording cleanup-error types and preserving the
original scenario error. A focused isolated check of that exact cleanup function
injected barrier and logout failures and proved revocation still ran and both
failure types remained. It called no stand APIs and is helper verification,
not product acceptance. Fresh complete v3 review found no actionable finding.
Syntax/preflight passed before the runtime execution. This first actual probe
passed; no HTTP request or operation was resent to obtain the result.

Evidence is `.cache/shuttle-live-test/orders-http-race-qa/checkpoint.json`:
source hash, native/agent preparations, two observed waiters with pending client
calls, identical responses, source receipts and cleanup. The isolated cleanup
proof is `.cache/shuttle-work/orders-http-race-cleanup-unit/checkpoint.json`.
No product source, migration or dependency changed for this probe.

This completes the planned native-first API and overlapping canonical-effect
acceptance alongside the previously observed real-browser paths. It does not
establish a concurrent Telegram UI/model scenario, physical failure recovery,
or every timeout/interruption permutation. Order evidence intake, capacity,
ambassador confirmation/refund notifications and fulfillment are the next
business slice; broader roadmap and production readiness remain incomplete.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
