# Meal draft runtime acceptance — 2026-10-19

The owned `meal-runtime-r1` stand passes four meal-draft paths on the uninstalled
Linux/amd64 image `sha256:232e1d477ea5685990e1622e2ad41dac07f460e58a53c8f9a552bf99d44d4a0d`
(schema53 / `mvp-60`). This gate uses real PostgreSQL, Zitadel and Loom, the
shipped Mini App in headless Edge, mock Telegram transport and a controlled
provider. It does not prove unattended model interpretation or Telegram WebView.

## Setup and boundaries

The separate nine-service project had its own database, identities, loopback
ports21740–21745 and synthetic bot. No real Telegram message was sent. Normal
signed mock initData bootstrapped browser sessions through the actual bot and
Zitadel. Only Telegram's external SDK was replaced with a local initData fixture;
the shipped application and HTTP APIs ran unchanged. Browser traffic was limited
to the owned origin. Synthetic event/pass/catalog setup used normal authorized
APIs; this is not registration UI acceptance. Three isolated users included a
fixture administrator. Four events enabled `registrations` and `meals`, with
assigned unpaid passes, explicit service instants and Europe/Berlin timezone.
All prices and menus were synthetic; no production defaults were selected.

## Observed paths

1. Native-only: stop only this stand's Loom, send `/menu` through mock Telegram,
   open the actual shipped form, compose soup/main/side/salad and optional tea,
   save and reload. The command creates one structured task with no Loom run;
   the form creates no agent task. One original receipt remains after reload.
2. Agent-only: an explicit user message creates one actual Loom run. Reads use
   Loom's `script` tool; `module_call` performs the authorized `save_draft` write.
   The complete quote is715 minor units, with one saved revision and receipt.
3. Native-to-agent: the browser saves an incomplete combo with only main chosen.
   The next explicit agent request includes bounded untrusted native-action
   metadata. The agent reads the current draft and catalog before completing it.
   The same owner/event draft advances from version1 to2; both original receipts
   remain. A partial known amount is never treated as a payable quote.
4. Agent-to-native: Loom saves the incomplete draft, then remains at a model step.
   A normal bot SIGTERM stop takes0.640s, exits0 and is not OOM-killed. The same
   container starts; task ID, Loom run ID and draft version1 remain. The browser
   completes that draft to version2. The original Loom run then reads the exact
   browser receipt and finishes without another save. The initial explicit user
   request described this form handoff; no unsolicited new agent task is started.

Each browser path uses the real IAM-backed HTTP session, saves once and reloads
without a second POST. All four final quotes are715 minor units. Cross-owner
reads return404. Final reconciliation finds four drafts, six original operation
receipts and three native-action journal records. All four tasks and three Loom
runs complete; all outbox records are delivered. There are no submitted orders,
order payment claims or massage bookings. Saving a meal draft remains nonfinancial.

The first agent read probe incorrectly used the write-only `module_call` broker;
Loom returned `use_script_for_reads`. This QA-driver failure and its source are
retained. The same original run continued using the offered `script` contract;
no user message or write was repeated. No product change was required.

## Evidence and cleanup

Ignored `.cache/shuttle-work/meal-runtime-v1-qa/` retains the stand checkpoint,
original requests/results, browser screenshots and receipts, restart timing,
`acceptance.json`, both database dumps and service logs. `pg_restore --list`
validated both dumps before removal. Exact owner labels, container IDs and images
bounded cleanup. All nine containers, project volumes and network were removed.
No QA container was OOM-killed. The source freeze still matches the candidate;
primary runtime/data preservation checks pass. The temporary image builder was
already removed. The primary stays schema49 / `mvp-56`; the candidate is uninstalled.

Financial submission and immutable sold composition, meal capacity policy,
evidence/review integration, unattended model behavior and actual Telegram
WebView remain separate work. The jointly deferred CA/zitadeltg session is open.
D04 release hold remains in force: no commit, push, deployment or primary install.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
