# Agent payment processing after sales closing, 2026-10-20

A bounded actual-time payment gate passes on schema64/mvp78: an original Telegram
proof sent before closing survives a bot restart and Loom downtime, then remains
timely when submitted and reviewed after closing. A later top-up needs an explicit
ambassador exception reason and consent. No application source changes are needed.

The disposable owned nine-service stand uses real local Zitadel, PostgreSQL and
Loom, synthetic Telegram and controlled model responses. Four IAM identities,
event/deposit/authority configuration and an explicitly API-assigned single pass
are prerequisites. This does not repeat or claim the whole registration journey.
No payment file, evidence, review or money is seeded.

## Real closing and original queued file

Before assignment, event configuration sets closing45 seconds after the current
clock, a120-second initial payment window, a100.00-BYN tariff and a30.00-BYN minimum
deposit with a25-percent minimum share. The assigned pass freezes the sales cap
as both its initial and final deadline: closing is earlier than assignment plus
120 seconds. The assigned deadline is never rewritten.

With Loom stopped, the owner sends a real mock-Telegram PDF and an explicit
30.00-BYN initial-payment caption. Its original Telegram date matches the stored
file send time, and one task/inbox row retain it before closing. No evidence,
review, credited money or Loom run exists yet.

The bot stops normally and the same container restarts while Loom stays stopped.
The original file, pass and assigned registration remain unchanged as the real
sales deadline passes. The bot's stop plus post-stop inspection takes1.703 seconds;
its logged application drain is4.99ms. These are separate observations, not exact
full-process signal-to-exit acceptance within one second. Stop exits0 without OOM.

The original Loom container then starts. Its actual model input contains the
original file ID and exact user caption on that original task. Payment tools
show the obligation as overdue, yet submission correctly marks the file timely
from its trusted send time. Submission remains pending and credits zero.

## Human decisions and late top-up

The ambassador reads the original evidence, file and balance through actual Loom
tools. The prepared decision shows30.00 BYN,100.00 remaining before the decision,
70.00 remaining afterward, deposit requirements and exact Minsk-local timestamps.
The original file was sent at07:49:22, before closing at07:50:06. Confirmation at
07:50:44 is accepted with an empty exception reason because the file is timely.
Only explicit free-text consent credits30.00 BYN. The deposit is satisfied;
70.00 BYN remains and the pass is not fully paid.

The owner next sends a distinct70.00-BYN top-up PDF at07:50:55, after the same
frozen closing deadline. Submission marks it late and remains pending. An actual
Loom save-review call with an empty reason reaches terminal failed state; it
creates no workspace/review and changes no evidence or credited money. The
ambassador explicitly supplies the late-case reason and approves the exact
prepared decision. Confirmation at07:51:26 credits70.00 BYN. The final balance is
30.00 +70.00 =100.00 BYN confirmed, with0.00 remaining and the original pass fully paid.
All quoted times are2026-10-20 in Europe/Minsk, UTC+03:00.

## Final evidence and manual checks

Final reads verify one unchanged pass and assigned registration, two immutable
original files/evidence rows, two original pending submission receipts and two
confirmed ambassador reviews. Four actual Loom runs and all bot tasks/outbox
entries complete. The expected failed empty-reason operation remains terminal.
Six notices are delivered: each proof goes to the administrator and ambassador,
and each confirmed decision goes to the owner. Actual recipient/text/entities
match stored payloads/receipts with no duplicate notice text per recipient.

Both original captions and file IDs remain in their actual model requests. During
review, Loom fetch descriptors match original byte counts and SHA-256 hashes.
After confirmation, protected reads as the ambassador return both original PDF
bytes unchanged. A separate read-only audit verifies exact event-local sent/deadline
text against the trusted timestamps. The transcript is inspected manually.

One presentation follow-up remains visible: payment approval questions append
raw technical workspace/review JSON after their complete readable terms.
`pendingApprovalMessages` sends this method through the generic approval renderer.
Removing that JSON must preserve canonical parameters, expiry, approval/hash and
free-text consent; this gate does not claim that presentation issue is fixed.

## Preserved failure, reviews and cleanup

The first stand fails setup before any registration, pass, file, evidence, review
or Loom run. The event API already creates a null deposit-policy row; the fixture
incorrectly attempts a second INSERT. Its SQL transaction rolls back. Original
error, read-only zero-effect observation, checkpoint and archives are preserved.
The corrected fixture supplies deposit in the full event API document instead.
Both source versions receive fresh ordinary/adversarial reviews; the final round
has no actionable findings. The fresh corrected runtime passes its first complete
execution. No Go test/build rerun or product fix is claimed here.

Both temporary stands stop all nine exact owned services with exit0 and no OOM.
Each stops writers before fully decoding and hash-checking three DB archives,
then removes its containers, network and volumes. Decoding is not fresh restore
acceptance. Cleanup remains one-shot with retained journals; uncertain partial
cleanup requires inspection instead of blind replay. Current ignored acceptance
is `.cache/shuttle-work/pass-agent-closing-v2-qa/acceptance.json`; the original
failed setup remains in the adjacent v1 QA root. The owned primary stays
schema64/mvp78, eleven services and134 original Loom runs. Production, the real
Telegram chat and Loom developer systems are untouched.

This accepts the assigned-pass agent closing/timing path, not every timing,
rejection, authority, interface or failure combination. Native forms, real
Telegram WebView, unattended inference, token expiry, long outages, fresh DB
restore and production readiness remain separate. The broader goal stays open;
next remove the observed technical approval JSON without weakening exact consent.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
