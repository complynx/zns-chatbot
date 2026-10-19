# Natural question expiry: real Telegram QA

An unanswered English question expired naturally on the owned shuttle-live-test
stand, in the actual authorized @complynx_test_bot chat. Real Zitadel, Loom and
Telegram transport were used with a manual provider. No timestamps or lifecycle
rows were edited. This verifies question expiry, not approval expiry or model
understanding.

The current Telegram account explicitly requested a test question. For setup,
the existing disposable fixture administrator temporarily received harness-admin
in the owned IAM project. The current Telegram user's rights were not changed.
Original role grants and all Loom settings were saved first. PATCH through Loom's
normal admin API set question_timeout_seconds to 10; other setting values were
unchanged. The new run's settings snapshot retained 10. After question publication,
settings were restored through the API, the fixture's original grant was restored,
and the new unused IAM role was deleted. Settings version advanced normally;
all effective values match the original snapshot. Fresh IAM reads verify cleanup.

The real question tool published a question with two choices and free text.
Telegram displayed its text and keyboard toggle. No answer was sent or selected.
The ten-second deadline elapsed: Telegram displayed "This choice is no longer
active." and removed the keyboard. It did not acknowledge an answer. Loom and
the bot task ended failed, with wait_expired recorded in the bot's Loom snapshot.
That expected terminal timeout is not a crash or a successful business action.

Exactly one summary and one model step completed. Their IDs match the provider
requests handled before expiry; there was no new inference after expiry. The
only provider call was question. No business call was made. The same bot,
poller and eight companions (including Loom) remain running. No containers or
processes were created or restarted for this QA.

## Retained failures and scope

The initial preflight omitted an old terminal delivery_failed task from its
idle check and stopped before changing settings or rights. Its failure is
retained. After expiry, the keyboard-toggle locator had already disappeared;
individual choice captions were not visually opened or verified. The expired
text and absent toggle were observed directly. A direct native owner fixture
read of the run returned 403; final-state evidence instead uses the bot's actual
Loom response snapshot and read-only store queries. This is not a passing direct
API access check. A manifest probe first indexed optional methods, then counted
only static declarations; those incomplete reads were replaced by full discovery.

The installed mvp-39 manifest plus all three dynamic modules were then inspected
under the owned disposable administrator: 14 confirmed methods, zero forced
protocol methods. Current confirmed actions therefore retain agent dialogue
policy. Real protocol-button compatibility still needs its own fixture and is
not claimed here. Real approval expiry, cross-person review, unattended model
behavior, interactive zitadeltg/CA and production acceptance remain open. The
existing component tests are not relabelled as those real checks.

Evidence is in ignored `.cache/shuttle-work/dialog-expiry-real-qa/checkpoint.json`,
its preflight failure record and the standalone
`.cache/shuttle-work/dialog_expiry_real_qa.py`. Settings/rights were changed only
on the owned stand and restored. No commit, push or production change occurred.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
