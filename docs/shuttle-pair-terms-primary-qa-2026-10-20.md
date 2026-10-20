# Pair terms primary acceptance, 2026-10-20

The owned local primary is now schema64 / `zinusya/mvp-77`. Eleven services,
original business data, durable queues and 134 Loom runs are preserved.
Production and the Loom developer's checkout and stands were not changed.

Installed image:
`sha256:9cbe199071fbcdad221aa9a4c6a9cedd16158c416af0e94d86ed8b224e553614`.
Verified Linux binary SHA256:
`df2ab626c23820688a398ef37dad498471c01cc418c0f7200c02e0f4bac75878`.
Product source SHA256:
`cc71c43c9657d0323d4a21e5980e432e005f6e84affd2c400e1bd29223bc658b`.

## Installation and rollback

The reviewed controller stops the original poller, waits for quiet work, stops
the bot gracefully and saves custom archives for Shuttle, Loom and Zitadel.
All three archives are fully decoded with pg_restore. A fresh database restore
is not exercised.

Only the bot image changes. Actual mvp77/mvp74/mvp77 binaries run against the same
schema64 database. No migration, Down or database restore is performed. Each leg
checks the manifest, original data and queues, database identity, schema objects,
mounted configuration and companion container identities. The three bot stops
exit with code0 and no OOM. This gate does not measure Docker signal-to-exit time
or establish the full-process one-second shutdown target.

## Ingress observation and installed HTTP checks

The initial ingress restart fails its 30-second observation window: no current-
start startup/completed-cycle markers are observed. The controller safely stops
the original poller with exit0 and retains the failed checkpoint. Read-only
outgoing HTTPS probes succeed afterward; the exact cause of that first delayed
startup is not established.

A separately reviewed recovery confirms the original poller is terminal, records
a new start intent and starts that same container once. It observes that start
without replay, using a 130-second bound that accommodates preflight HTTP waits
and long polling. Both expected bot identity and a completed polling cycle are
observed. The final original poller is running, and preservation checks pass.
The original failure remains evidence, rather than becoming a retrospective PASS.

Installed read-back verifies all17 current Mini App assets byte for byte and
`Cache-Control: no-store`. Payment targets, pair proposals and the new agent
payment-operation read return401 without credentials when the request is valid.
The first agent probe used an invalid empty body and returned422 before auth;
its failed QA result is retained. The corrected schema-valid read passes without
any application change. No business action, Telegram message, IAM grant or new
Loom run is submitted by these read-back checks.

## Reviews and acceptance boundary

Fresh ordinary and adversarial reviews accept the installation controller and
the separate ingress recovery. The existing [source/full-Go gate](shuttle-pair-assignment-terms-2026-10-20.md)
and [actual old/new compatibility gate](shuttle-pair-terms-binary-runtime-qa-2026-10-20.md)
remain separate evidence. The source suite and whole joined workflows were not
rerun during this binary-only primary promotion.

Local ignored evidence is in `.cache/shuttle-work/pair-terms-install-v1`: journal,
three private archives, review gates, retained failures, installed epoch and HTTP
read-back. Current custody uses `installed_primary_observer_v22.py`; observer21
and earlier installed epochs remain historical. No new container remains from
this gate; the eleven primary services are running. The Git index is empty.

Next are registration/pass deadline and failure paths, then complete orders/meals
and massage journeys. Same-pair proposal history labels remain a usability
follow-up. Real Telegram WebView, unattended model comprehension, Linux/race,
archive restoration and production readiness are not accepted by this gate.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
