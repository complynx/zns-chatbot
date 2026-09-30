# Frozen public Functional QA access

Freeze epoch: `ce427ca56ff9f127eba55321047c64a5e9204683`.
Frozen on 2026-10-01 after the engineer released preparation ownership.
This is an access and scope record, not a Functional QA verdict.

| Owner | Stand | Telegram-like UI | Mini App proxy | Controls |
| --- | --- | --- | --- | --- |
| Reviewer A | synthetic-qa-zns-fqa-flows | http://127.0.0.1:58403/ | http://127.0.0.1:58403/miniapp/ | http://127.0.0.1:58404/ |
| Reviewer B | synthetic-qa-zns-fqa-recovery | http://127.0.0.1:58413/ | http://127.0.0.1:58413/miniapp/ | http://127.0.0.1:58414/ |

Image binding: app/fake
`synthetic-qa-zns-fqa-app@sha256:42e43c955dbaaeb2ec6640147302af6365de824652a4b14465c66b9c75506ef3`.
Per-stand mutable state and six managed components are isolated. Direct app/PG
host access is unavailable; never use root PostgreSQL 55432. Import has no live
handoff and remains unprepared.

## Current acceptance scope

Run independent black-box checks of supported manual UI behavior in EN/RU,
visible conversation/messages, inline/reply buttons and callback acknowledgement,
prior-message edits, stale interfaces, synthetic user isolation, uploads/downloads
and Mini App boundaries where applicable. Public controls may supplement actual
UI evidence. Preserve every original planned scenario and mark unsupported
parts unavailable/blocked, never passed. This batch does not include successful
deterministic-model orchestration, lifecycle replacement, database faults or
importer acceptance. Natural reasoning and real integrations require later gates.

Initial synthetic state in both stands: Alice (user 101, owner alice) has a
massage draft at version 1 and a `readiness.txt` upload/media-intent card. UI also
offers Boris (payment administrator), Visitor (read-only), and synthetic
channel/forum choices. These are test identities. Use public UI to observe current
roles/state rather than inferring permissions from a label.

## Public controls and ownership

Lab routes require `X-Sandbox: 1`: `GET /lab/state?user=101`,
`POST /lab/input`, `/lab/blocked`, `/lab/fault`, media routes,
`POST /lab/model/fixtures`, and
`GET /lab/model/state?owner=alice&update_id=<id>`. Do not assume undocumented
request shapes. Inspect the UI or request a public contract before using them.
Fixture model turns are synthetic proposals. Successful model scenarios are
outside this current handoff scope.

Public model-fixture setup contract (optional capability check, not an accepted
scenario): use normal UI `/language` to choose the desired application locale.
For an existing user, Telegram `language_code` does not replace that chosen
locale. Fixture `expect.language` must explicitly match the currently selected
application locale. `POST /lab/model/fixtures` accepts
`{input:{user:101,language_code:'en',text:'Show my profile.'},
steps:[{expect:{language:'en',text:'Show my profile.'},
plan:{view:'profile',text:''}}]}` with `X-Sandbox:1` and en selected in UI.
Installation automatically enqueues that input and returns `update_id`; do not
send the same input again manually. Inspect documented model state and actual
visible UI before assessing the outcome. This is deterministic proposal routing,
not natural language understanding. A reviewer may check this capability within
their existing scenario ownership and report whether it supports planned cases.

Delay control `GET /control/state`, `POST /control/arm`, `POST /control/release`
requires the per-stand synthetic key supplied privately by the lead. Arming
selects exact positive chat/message IDs and SHA256 of replacement UTF-8 text,
mode `before_apply` or `after_apply_loss`. The case is one-shot. Observe a held
request before release. New case/provider restart requires an exclusive lead
window. Control acknowledgement is not business success.

Reviewer is the sole scenario writer on their own stand. No reseed, rebuild,
config/image replacement or restart during frozen manual QA. Request missing
capabilities, release the affected stand, and wait for a new handoff before
engineering mutation. The other reviewer's stand is outside your ownership.
Report exact case coverage and limitations independently; no implementation
source, diffs, engineering findings or other reviewer evidence.

## Isolated browser recipe

Existing Node executable:
`C:/Users/ddriz/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node.exe`.
Existing Playwright package:
`C:/Users/ddriz/Projects/zns-chatbot/platform/node_modules/playwright`.
Use `chromium.launch({headless:true, executablePath:
'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'})`, then
`newContext()` and a fresh page at your assigned URL. Use DOM locators and actual
UI clicks, capture screenshots/visible state, and close browser in `finally`.
Do not use personal profiles/tabs. No browser downloads are required. Windows
browser launch may require sandbox escalation. Save evidence only in your
assigned reviewer paths. Do not inspect application implementation or another
reviewer's test code. HTTP-only checks cannot substitute for the browser UI.
