# Native massage entry source checkpoint

Date: 2026-10-17. Source schema46 / manifest `mvp-47`.
The owned runtime remains schema45 / `mvp-44`; this source has not been installed.
This extends the [owner-form component](shuttle-massage-native-ui-qa-2026-10-17.md).

`/massage` uses the existing durable structured-command and outbox path. The menu
and reply keyboard include it. English/Russian replies link to the configured
Mini App launch URL with `#massage`, independently of the authentication origin.
The command creates no Loom run and does not answer or replace an active agent
question. If no form URL is configured, it reports that the form is unavailable.
The visible massage section scrolls into view for its launch anchor, including
an unreadable saved-request explanation; the anchor submits no booking.

The existing bounded structured-request projection now includes `/massage`.
The next user-triggered agent turn receives this navigation metadata under the
same user/conversation rules, including active-run steering after a bot restart.
This history is not consent, a booking or a new request. Native command navigation
alone does not trigger an agent run.

## Verification and retained failure

The first Linux/PostgreSQL race run failed two new checks: its launch assertion
still expected the old orders URL, and the real structured-context whitelist did
not include `/massage`. The failed output is retained. The assertion was corrected
to the configured launch URL plus `#massage`; the projection was fixed without
weakening the context assertion.

The final bounded Linux Go1.27.2/PostgreSQL17 race run passes all seven selected
roots: three new massage cases, three neighboring orders cases and the existing
local/public launch-origin case. There are zero failures/skips, and all 968 frozen
inputs remained unchanged. Coverage includes duplicate update/restart reuse,
no calls to unavailable Loom, en/ru launch text, current public launch URL,
unconfigured fallback, preservation of an active agent question and subsequent
steering with the navigation context. IAM/Loom/Telegram peers are mocks.

All 121 Mini App JavaScript tests pass, including 18 massage cases and the new
launch scenarios. Configured task/database/integration lint reports zero issues.
Fresh source review after the fixes found no actionable findings. Both attempts'
temporary containers and private volumes were removed; no Go runner was OOM-killed.
The ten main Compose services retained IDs, images, start times and restart counts.
Go was limited to2GiB and PostgreSQL512MiB with swap disabled. Evidence remains in
ignored `.cache/shuttle-work/massage-command-v1-qa/` and `v2-qa/` directories.

## Next actual handoff slice

Architect and senior Go consultations agree: add an explicit optional
`existing_proposal_id` to agent preparation arguments, retaining required business
arguments. Prepare must echo the new Loom operation ID while returning the same
canonical proposal and original expiry; normalized response arguments omit the
selector. Never allocate a replacement, extend expiry or deduplicate by equal
terms. Current frozen Loom fc6c574 supports this argument normalization; no Loom
change is needed. Implement and test it before claiming actual agent adoption.

Add bounded pending discovery for both interfaces, creation timestamp/provenance,
and compact pre-task pending/action context. Metadata remains untrusted and never
consent; new booking effects still recheck entitlement. Native continuation must
revalidate the selected canonical proposal and require a fresh confirmation click.
Existing tests that manually transfer known IDs do not prove real agent discovery
or use of a Loom-generated operation.

This is source component acceptance. Real embedded `/massage`, native/agent/both
mixed workflows, owned-stand rollout and unattended-model acceptance remain open.
No production deployment or publication occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
