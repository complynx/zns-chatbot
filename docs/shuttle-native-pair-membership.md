# Native current pairs and participant leave

The owned primary now runs the final current-pair/leave image. Four functional
paths pass on an isolated real-IAM/Loom stand. The actual Telegram form displays
four current pairs with the corrected checkboxes, and its new session was logged
out. That Telegram gate is read-only: it does not exercise a real Telegram pair
write. Full registration/pass parity, unattended model understanding and
production readiness remain separate requirements.

## API and business records

- `GET /app/pairs/my?event_id=&after=&limit=` lists the current owner's waitlisted
  or assigned pairs. Pagination and participant ownership match the agent's
  existing `zinusya_pairs.my` read. Historical invitation receipts are separate.
- `POST /app/pairs/dissolve` accepts `operation_id` and `arguments` containing
  `event_id`, `pair_id` and `expected_version`.
- `GET /app/pairs/operations/{operation}?kind=dissolve` recovers the original
  dissolution receipt. A receipt describes that operation, not current membership.

The write uses the existing agent domain operation and receipt namespace. It
checks the current Mini App owner/session after acquiring the event and owner
locks, before receipt replay or mutation. A session revoked while waiting for an
event lock cannot execute the write. Retry uses the original operation key and
arguments. A changed request conflicts. A different participant cannot read the
actor's receipt or act using that identity.

Existing participant policy is unchanged: either participant can end membership,
including after sales close or module disablement. Administrator rights do not
substitute for participant ownership. Dissolution removes active membership and
advances registration versions. It does not cancel passes, change price/deposit
terms, confirm payments or create a refund.

The next user-triggered agent request receives up to five own invitation and
membership operation facts. A dissolution fact contains its pair ID and original
state. Facts are marked untrusted history, not instructions or consent. No
proactive agent run is started by native entry.

## Form

The existing pair form shows current pairs separately from invitation history.
Each current row shows its event, state and event-local time. Leaving requires an
explicit checkbox and button click. The form re-reads the current pair/version
before sending; a changed pair requires renewed review. Text is rendered inertly.
Both English and Russian labels are provided.

The owner-bound recovery record contains the exact operation key, action and
arguments before dispatch. An uncertain outcome blocks replacement actions.
Reload recovers the original receipt; an uncommitted request can retry the same
body. A rejected request can be checked and closed under the existing rules.
Logout clears owner data and ignores late responses. Refresh reflects an agent's
completed dissolution without rewriting accepted invitation history.

## Component verification

Five PostgreSQL root scenarios pass, including both native-first and agent-first
receipt replay after restart, preservation of assigned passes, foreign access,
revocation during event-lock wait, HTTP receipt recovery and reactive history.
Existing dissolution cases also cover deposits and pending payment evidence.
These tests use real PostgreSQL with mock IAM/Loom/Telegram peers. No test skips
were observed. The12 UI test files pass244 behavior cases, including four new
current-pair/leave cases. The affected API, store and task packages pass. Strict
lint reports0 issues after fixing an if-chain warning and an extra blank line.

Fresh API and full-slice independent reviews have no actionable findings. A
Linux/amd64 build from564 pinned source/test files produced binary SHA256
`bf66ed167d61aba31ed3c9b7de115c6dbe6ddb37d5aa8685384d964993e89fe4`.
First functionally accepted local candidate image:
`sha256:63bf3cbe55aee5069b0da9d60673524b39f8d7933489d1e5e7923db51676aa98`.
It adds a binary layer and QA label to the accepted local base; helper, license
and CA files are inherited. This is not a fresh release-image build. The first
BuildKit attempt misinterpreted the local image ID as a registry name and failed;
its log is retained. The corrected local build passed without RUN steps.

## Owned functional runtime QA

The isolated stand used real IAM and Loom, mock Telegram and headless Edge.
Its fixtures created three synthetic identities, SQL event rules/leader waitlists
and four accepted pairs through authenticated participant APIs. Fixture creation
is not end-to-end registration acceptance. From those existing pairs, four paths
passed: native current-pair read/leave, agent read/leave, native leave followed by
an agent read of the same owner's history/current state, and agent leave followed
by that owner's native read. Agent requests used a controlled provider; token
counters are synthetic. This does not prove unattended model understanding.

The native-only case committed one server write, deliberately lost its HTTP
response and reloaded the form. The original receipt recovered without a second
write. The native-to-agent case included the original manual operation fact and
read an empty current-pair list without a write. The agent-to-native case showed
no current pair while the accepted invitation remained historical. Browser
contexts logged out and closed. All four pairs are dissolved with0 active members;
12 operation receipts comprise8 fixture receipts and4 dissolution receipts. The
four original accepted receipts retain their exact prior pair results. All3
reactive tasks/runs completed and3 replies were delivered. This fixture has0
passes; assigned-pass/deposit/evidence preservation is covered by the separate
PostgreSQL component cases, not this runtime.

Visual inspection found a full-width checkbox caused by the shared input rule.
A rule scoped to `#pairs input[type="checkbox"]` now restores natural width and
spacing. A networkless headless Edge fragment with the full original/fixed CSS
measured checkbox width512px before and13px after, with consent text next to it.
This follow-up changes CSS only. Its564-file source closure differs from the
functionally accepted candidate only at `internal/api/miniapp_ui/app.css`.
The final Linux binary SHA256 is
`f84dbc5f50940110bef619e36825092bd03c951bf619b80a5e52a9dbb2d98db8`;
new local image:
`sha256:2aa2c96ca78e9b7cd80f7e2d43b2e78c09934d956eca148665a230e6e7ac93de`.
This final image is now installed. Its actual Telegram form read/geometry/logout
acceptance is described below; no real Telegram membership write is claimed.

The component PostgreSQL dump was validated; its512MiB-limited container exited0
without OOM using a30-second database stop timeout. Its container/owned volume were
removed. The runtime stand's3 database dumps were validated,9 containers stopped
without OOM, and its containers/network/volumes were removed. Bot and PostgreSQL
exited0; PostgreSQL used30 seconds. At isolated-stand cleanup, the primary's11 service identities, data and134 Loom
runs were preserved under `installed_primary_observer_v15.py`. The later primary
installation changes the bot/poller epoch as described below.
## Primary installation and actual Telegram form

A fresh source-pinned controller installed the final image using candidate,
previous image, candidate against the existing database. No migration or database
restore was performed. The three bot stops took0.625,0.656 and0.672 seconds, all
exit0 without OOM. The exact poller was fenced during changes and resumed only
after a fresh polling cycle. All14 embedded public assets match source bytes and
retain no-store/nosniff headers. Other service identities, canonical business data,
old communications and134 Loom runs were preserved. The rollback image remains
available. An independent controller review and ingress-failure probe passed.

The first inspection used an incorrect avatar-helper path and stopped before any
runtime change; its inspection containers were removed. The corrected check used
`/usr/local/bin/zinusya-avatar`, verified exact candidate binary bytes and unchanged
helper/CA hashes, and passed. The initial failure remains in ignored evidence.

Using only the authorized current account in `@complynx_test_bot`, the existing
pair-form button was opened without sending a message. The form showed the correct
owner code and four current pairs matching scoped database rows. Assigned and
waiting states were distinct. All four consent checkboxes remained unchecked;
measured width was approximately13px, with an8px gap before text. The initial
heading landed at-0.054..26.946px within the530px viewport, within the existing
one-pixel rounding tolerance. Actual screenshot inspection confirmed the layout.
No invite, accept, revoke or leave action was submitted.

Sign out revoked exactly one new owner session; the modal closed and the authorized
chat remained selected. Readback preserves all prior sessions, business/profile/
schema records and task/inbox/outbox/conversation hashes. No new Telegram message,
agent run or River job was created. Only the new revoked session and scheduler
leader metadata differ. A focus/scroll attempt exceeded its locator deadline;
all four unchecked states were re-read before successful native Sign out/Close.
The baseline query also initially assumed an inbox `id` column; canonical row
sorting corrected it before opening the form. Both failed observations are retained.

The actual screenshot was displayed through CUA. No exported PNG is claimed;
earlier CUA filesystem export returned EPERM. This does not replace the separate
saved headless runtime screenshots. Current custody is
`installed_primary_observer_v17.py`. Ignored installation evidence is
`.cache/shuttle-work/native-pair-membership-install-v1/`; actual Telegram evidence
is `.cache/shuttle-work/native-pair-membership-real-v1-qa/`.

The Git index remains empty; no commit, push or production deployment occurred.
Ignored component evidence: `.cache/shuttle-work/native-pair-membership-v1/`.
Owned browser QA evidence: `.cache/shuttle-work/native-pair-membership-runtime-v1-qa/`.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
