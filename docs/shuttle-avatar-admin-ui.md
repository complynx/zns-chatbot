# Avatar delivery operator form

The Mini App operator section uses the existing schema59 / manifest mvp68 native
administrator API. It adds no migration, image processor, dependency or automatic
agent task. Server authorization remains authoritative; opening the page or
knowing a delivery ID never grants administrator access.

## Review and confirmation

Read a delivery UUID to inspect its current state, version and captured Telegram
recipient. Only delivery_unknown, delivery_failed and delivery_blocked states
can be prepared. Select
retry or dismiss, enter a reason, then review the server's normalized original
arguments and delivery snapshot. Confirmation is a separate explicit click.
Retry can send another copy: inspect the recipient chat first. Dismiss closes
recovery and does not assert that the original message was delivered.

The browser keeps the current delivery separate from the immutable earlier result.
A result of pending means a retry was scheduled, not that Telegram accepted it.
Read current state again to observe later delivery progress. Eventless proposal
deadlines are shown in UTC with an explicit Z. Interface labels support en/ru;
operator-supplied reasons remain verbatim text and cannot inject HTML.

## Recovery and handoff

Before either POST, sessionStorage saves only owner, operation ID, optional
proposal ID and phase. Reasons, receipts and credentials are not stored there.
Unavailable or malformed recovery storage blocks mutations. Reload reads the
same operation through the authorized API. It does not create a replacement
proposal, renew the original five-minute deadline or send another document.

If a confirmation request may not have arrived, the operator can explicitly
repeat that exact confirmation. It retains the operation, proposal and arguments;
the server replays an immutable result or performs the single original change.
There is no automatic application retry. Closing is unavailable while such a
confirmation remains unresolved. Once completed or expired it clears only this
browser's preview. Closing a preparation never dismisses a delivery or removes
the server proposal.

Actor-owned unfinished proposals are discoverable through bounded, cursor-based
pages even after browser storage loss. Selecting one reads its original canonical
operation; no prepare POST is needed. This permits an agent-origin proposal to
be reviewed and confirmed in the form. The reverse direction uses the existing
reactive native-action locators and agent adoption contract. This UI does not
substitute a click for broker approval in an agent-mediated confirmation.

Session or role loss clears privileged DOM, retaining owner-bound recovery IDs.
After reauthorization, the same owner can read the earlier result. Account changes
fence late successes and failures, including late401 responses. Language changes,
list pagination and status reads never issue business mutations.

## Acceptance boundary

The UI gate uses behavioral JavaScript tests, Linux embedded-asset/API tests and
owned headless Edge with the shipped operator HTML, module and CSS against a mock
HTTP backend. It is not acceptance of an installed candidate, actual Loom handoff,
real Zitadel or Telegram WebView. Subsequent
[owned runtime acceptance](shuttle-avatar-admin-runtime-qa-2026-10-19.md) passed
both real-IAM/Loom handoffs on the mvp68 candidate, including a normal restart.
That gate still uses simulated Telegram and Codex-fed model responses. The
installed primary remains schema49/mvp56; the mvp68 image is retained separately.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
