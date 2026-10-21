# Final-image meal browser acceptance, 2026-10-21

Status: the focused stale-draft recovery and explicit checkout pass in actual
headless Edge. The disposable nine-service stand is archived and removed.
At this browser checkpoint the primary remained schema68/mvp-86. Its subsequent
[installation acceptance](shuttle-meal-primary-qa-2026-10-21.md) is separate evidence.

## Build and scope

Source commit: f78f70960e9afd97593f60b58029f831fb55bc0b.
Bot image: sha256:85acc5a4c5e488947ce1e633eb6c301c88545be26a019efdf6ef036ffc608d69.
Owned project: zinusya-mock-meal-final-browser-r1, local ports22630–22635.

The browser uses the real HTTPS proxy and native API, actual PostgreSQL and
Zitadel. The Telegram WebApp SDK is replaced by a minimal stub providing signed
synthetic initData and a no-op ready(). The proof is signed with this stand's
synthetic bot key and verified by the application. Loom is intentionally stopped.
These checks do not establish actual Telegram
WebView behavior, autonomous model quality or exact prepared-operation adoption.

## Accepted behavior

1. The native form saves a complete draft1 against event1/catalog1, quoting
   four selected lunch components for665 minor units, or6.65BYN.
2. An administrator changes event metadata to version2. The browser reads
   current event2 beside retained draft1/event1.
3. Review preparation returns409. The actual UI shows the rejection, enables
   reload, disables preparation and clears the pending recovery record. No
   order or preparation is created by the rejected request.
4. A separate browser continuation reloads and explicitly saves draft2/event2.
   It prepares that revision, displays all four components, the total, event
   timezone, service/sales/proof deadline and consent expiry before execution.
5. One explicit confirmation submits one order. The consumed preparation is
   retained; the saved draft advances to cleared version3. Browser recovery
   records are empty after the receipt.

Independent database readback confirms one submitted meal order sourced from
draft2, one consumed preparation for665minor, zero payment proofs, zero
allocations, zero tasks and zero Loom runs. Submission does not confirm payment.

## Retained diagnostic failures

Two original browser harnesses stalled and were terminated after identifying
their own Node/Edge processes. A later bounded diagnostic receives409 but times
out reading response.json in Playwright. A separate native HTTP probe reads the
finite JSON in2.078seconds. A subsequent browser diagnostic proves the UI itself
handles409 and recovers its controls even while the harness body reader times
out. The precise browser-tool mechanism is not established; no product fix is
claimed for this diagnostic limitation.

Launch/readiness timeouts from intermediate continuations remain retained.
Fresh signed launch data and awaited event readiness permit the final flow to
complete. Its success does not rewrite the earlier attempts as passes.

## Review and resource retirement

One independent textual review of the browser harnesses and ownership-scoped
retirement reports no actionable findings. Execution evidence is separate.
Both databases are exported and fully decoded before removal; all service logs
are retained privately. Project and owner labels are checked for containers,
networks and volumes. All nine services and the project's networks/volumes are
removed. The original primary database, domain and eleven runtime identities
remain preserved at schema68/mvp-86.

Private evidence: browser-ui-diagnostic.json, browser-final-accepted.json,
db-after-browser.json, retirement-review.json, retirement.json and acceptance.json
under .cache/shuttle-work/meal-financial-flow-v1/final-browser-v1/.
Consent and final receipt screenshots are retained there too. Secret launch,
token, dump and log files remain ignored.

The subsequent CI and owned-primary installation are recorded separately above.
No production readiness or cutover is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
