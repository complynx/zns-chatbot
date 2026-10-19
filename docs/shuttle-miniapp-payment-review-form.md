# Payment reviewer Mini App form

Current acceptance: [four real Telegram happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
pass with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

The ru/en reviewer form, payer projection, direct event-context read and
[native entry/reactive context](shuttle-payment-review-entry.md) pass component
checks and are now installed on the owned schema39 test image. The image-only
rollout preserved data, configuration, keys and companion services; real-IAM reads
passed. Full real-IAM/Loom/Telegram payment-review workflow acceptance remains open.

## Workflow

After the existing Telegram session starts, the page lists events for which the
current user can review payments. Choose an event and a pending proof. The detail
shows its payer display name, reported amount, remaining balance, balance version,
proof timestamp and deadline. Related times use the event's IANA zone. Names and
reasons are rendered as text. The proof loader makes an authenticated content
request; a separate blob download link allows inspection. No IAM token enters the
browser or download URL. Changing detail, reloading or leaving the account revokes
the blob URL. There is no inline PDF renderer.

Enter a credited amount in the frozen currency's decimal units, or choose rejection
and enter a reason. A rejection submits zero. Decimal parsing uses integer arithmetic,
accepts comma or point and rejects excess fractional digits or unsafe minor units.
The server retains all financial and deadline validation, including reasons for
late exceptions. Preparation saves an exact decision and credits nothing.

The preview displays the saved decision, credited amount, reason and expiry.
A separate explicit button confirms that exact immutable workspace revision and
canonical proposal. Editing inputs disables confirmation until the edit is prepared
or current state reloaded. Cancellation cancels the saved intention and credits
nothing. An agent-prepared workspace is displayed and can be edited, cancelled or
confirmed through the same controls. Completed evidence shows the recorded decision;
its confirm controls are disabled. No tool or agent is invoked proactively.

## Request recovery and lifecycle

Before any prepare/cancel/confirm POST, the form saves one bounded pending record
in tab-scoped sessionStorage under the verified owner's UUID. It contains operation
UUID, kind, event/evidence IDs and the exact body, including any reason. It contains
no IAM or Mini App bearer token. The reason is retained while the request outcome
is unknown; this is deliberate support for exact replay. Unsent inputs remain in
memory. Logout clears rendered data and blob URLs, while a pending record remains
available only to that same owner's subsequent authorized session in the tab.

Reload first checks operation-only status. A404 means unknown and keeps the record;
it does not mean that payment failed. The user may check again or retry the exact
body with its original UUID. No new edit is dispatched while that record is pending.
A successful receipt clears it. A400/409 rejection clears the rejected request and
requires loading current state before another edit. Other failures retain recovery.
Storage failure prevents a new write; a malformed record blocks new writes on the
page instead of silently replacing an uncertain financial request.

Recovery also restores the event and its proof list. The directly authorized
`GET /app/payment-reviews/events/{event}` supplies the current name/timezone when
the event is absent from the first list page. It checks reviewer authority and
session in one transaction and exposes only ContextEvent fields. A completed proof
outside the pending list remains selected as history, while other pending proofs
remain reachable. This ninth payment-review route is included in the current owned candidate.

## Verification and retained boundaries

All52 dependency-free Node behavior tests pass, including nine reviewer tests.
They exercise explicit confirmation, chosen amounts, agent-prepared intent,
edit/cancel, lost successful replies, unknown404 with exact retry, storage failure,
owner isolation, expiry, blob disposal, actual main-page session/proof/logout wiring,
and navigation to another proof after reload recovery. CI now runs all three Mini
App browser suites, including the previously omitted orders suite.

The final Linux race gate passed eight Go root tests /13 pass events with real
PostgreSQL and mock peers: seven payment HTTP tests and the public-assets test.
It froze844 inputs including browser suites and CI. After that gate, only the two
localized completed-state labels and their Node assertion changed. A recorded
hash delta proves every Go input stayed unchanged; all52 Node tests pass on the
final844-input snapshot. Strict Go lint reported zero issues before that JS-only
copy change. Fresh source, quality and post-fix reviews found no remaining findings.
The earlier clean gate is separate from the final gate and is not counted twice.

Review found that recovery loaded a detail without restoring its proof selector;
this was fixed with a behavioral navigation case before the final Go gate. Manual
QA found imperative action labels displayed as completed statuses; separate ru/en
state labels now show Confirmed/Rejected or Подтверждено/Отклонено. Initial compiler,
lint and fixture/assertion failures remain in local evidence, distinct from passes.

Manual browser QA used a separate non-authorizing localhost fixture with exact
application JS/CSS. Only its HTML Telegram SDK URL was replaced with a synthetic
stub. This was not real IAM, a Telegram WebView or a real financial operation.
It verified Russian layout, payer text, event-local times, protected-file loading,
prepare without credit, a deliberately dropped successful confirmation response,
receipt recovery after real reload, another-proof navigation, zero-amount rejection,
cancellation, final localized status and logout. The fixture counted exactly two
recorded decisions and3087 credited minor units: one positive confirmation and one
rejection, without another confirmation POST during recovery. Its browser tab and
server were closed after QA. One final JS-only label change occurred during this
fixture session; the initial and final asset hashes are retained separately.

Local evidence lives in `.cache/shuttle-work/payment-review-ui-qa`,
`payment-review-ui-final-qa` and `payment-review-ui-browser-qa`. Both Go/PG pairs,
their volumes and the browser fixture are removed; the ten main stand services
were not changed. No commit, push, production change or publication occurred.

## Next acceptance

The complete candidate is installed. Native-only with Loom unavailable, agent-only
and both pending handoffs now pass in real Telegram Web with real IAM/Loom and a
manual provider. Embedded critical paths, cross-person review, UI follow-ups and
unattended model acceptance remain open. Production readiness is not established.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
