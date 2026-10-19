# Native pair primary install and Telegram entry QA — 2026-10-19

This report records the earlier native pair form installation. The later
[current-pair membership installation](shuttle-native-pair-membership.md)
supersedes its image and custody checkpoint. At this historical gate, the owned
primary ran the native pair form candidate. Its actual Telegram
`/pairs` command and read-only form/bootstrap/logout gate pass. This is separate
from the [six isolated native/agent paths](shuttle-native-pair-runtime-qa-2026-10-19.md).
No real Telegram invitation, acceptance or revoke was submitted.

## Installation

Installed image:
`sha256:7706bc58a464dd0dd0e3d8f85e36d32dd7666efec08644ef373ebf1e5100f6dc`.
The exact accepted binary was extracted and compared before switching. The avatar
helper and CA bundle matched the previous image. The image adds one bot binary
layer and its QA label; other runtime configuration is unchanged. The first
preflight stopped because the base image had no Labels object. That failed
inspection is retained; the corrected check handles an absent object and permits
only the expected added label. No runtime had changed at that failure.

The reviewed controller fenced the exact existing Telegram poller, drained work,
stopped the bot, and archived the existing database. It then exercised candidate,
previous image, candidate against that same database. No migration or database
restore was run. Canonical data, old communications, sessions, IAM/Loom/provider
and other service identities were preserved. Schema59/mvp68 and133 Loom runs
remain unchanged.

The three bot stops took0.593,0.594 and0.610 seconds, all exit0 and without OOM.
The original poller resumed only after all legs passed and a fresh polling cycle
was observed. Failure probes verify that failed final ingress verification fences
the exact poller once and preserves pending observation rather than replaying
start/recreate effects. Fourteen embedded public assets, including pairs.js,
match source exactly and retain no-store/nosniff headers. Inspection containers
were removed. The old image remains available for binary rollback.

## Actual Telegram check

Only the authorized current account in `@complynx_test_bot` was used. Exactly one
`/pairs` message created native task seq206 and one delivered reply with the form
button. It created no Loom run. The actual Telegram Mini App displayed:

- Pair invitations at the initial viewport position.
- The current owner's correct partner code.
- Current invitations loaded, with the event selector enabled.
- The invitation hold and historical-state explanation.

The heading bounds were approximately-0.054..26.946 pixels in a530-pixel viewport;
the existing one-pixel rounding tolerance applies. No invite/accept/decline/revoke
button or other business write was submitted. Sign out revoked exactly one new
owner session. The modal closed and the authorized chat remained selected.

Readback preserves all earlier session and communication rows. Only the expected
session, native task/inbox/outbox, conversation counters and one completed River
job were added or advanced; scheduler leader/queue metadata may vary. All business
and schema hashes match. The task has no run/auth-session binding, its inbox is
unique, its outbox is delivered to the authorized chat, and Loom stays at133.
The screenshot was displayed through CUA. Writing a PNG returned EPERM, so no
exported screenshot or local image path is claimed.

Install-controller and acceptance-evidence reviews have no actionable findings.
Ignored evidence is `.cache/shuttle-work/native-pairs-install-v1/` and
`.cache/shuttle-work/native-pairs-real-v1-qa/`. The current read-only custody helper
is `installed_primary_observer_v13.py`. No temporary QA container remains; the
primary still has11 services. No commit, push or production deployment occurred.

## Navigation-history follow-up

Source inspection found that the short native-command history omitted `/pairs`.
The fix adds `/pairs` to the existing owner/conversation-bounded whitelist.
A focused PostgreSQL behavior case first failed on the missing fact. After the
fix, four relevant PostgreSQL roots pass without skips, strict lint reports0
issues, and independent review is clean. The test preserves an awaiting question
through native navigation, Loom unavailability and bot restart, then resumes the
same run on an explicit reply. Native navigation creates no additional run.

The follow-up image is now installed:
`sha256:be425ce70c6e2a71c0e7aa1b1bc9d10139b39d3cb84c0881db86402f1e5125ee`.
A fresh source-pinned controller exercised candidate, previous image, candidate
against the same database. Its three bot stops took0.641,0.625 and0.593 seconds,
all exit0 without OOM. No migration or database restore was run. All14 public
assets still match source. Data, communications and the other service identities
were preserved at the installation gate; Loom still had133 runs at that point.
The independent controller review and ingress failure probe pass. The current
installation custody baseline is `installed_primary_observer_v14.py`; it predates
the reactive check below and cannot claim preservation of its expected new task.

Exactly one further real Telegram question asked which section had just been
opened, with an explicit instruction to change nothing. It created task seq207
and one original Loom run, bringing the total to134. The persisted run input
contains the `/pairs` native navigation fact. This verifies adapter input, not
model consumption or a delivered answer.

The controlled provider encountered history compaction before an ordinary model
turn. The observer refused to publish a final answer into a summary request.
No summary response was published in time: two summary attempts completed with
`platform_paid_unknown`, and the original run paused with `platform_retry_limit`.
At this initial checkpoint the bot task was `awaiting_service` with the same
run ID and no final provider reply. This failed attempt remains in the evidence;
it does not establish a Loom product defect.

The controlled provider was then extended to distinguish summary and model
steps. Its extractive historical summary contains neither `/pairs` nor the native
navigation marker. It answers only pending steps of the original run, publishes
responses exclusively without replacement, and verifies the exact native command
timestamp in the ordinary model request before publishing the final reply.
The provider and recovery controller received independent reviews. Controller
review corrected the mounted-config path and added restoration after a quiescent
unsuccessful recovery before any runtime mutation.

The configured sponsored-attempt limit2 remained exhausted across normal resume.
On this owned fixture only, the limit was temporarily raised to3 by stopping and
starting the same Loom container. The supported resume API continued the original
run. One summary and one model request then succeeded; no business tool was
called. The original task seq207 completed and exactly one outbox reply was
delivered. Fresh Telegram UI inspection showed: "Ты открывал раздел приглашений
в пару через /pairs. Ничего не менял." No replacement user message or run was
created; Loom remains at134 total runs.

The exact original configuration bytes and limit2 were restored with the same
container. Loom readiness returns200. All business, profile, Mini App session and
schema hashes match the baseline; old communication rows and other conversations
are preserved. Expected changes are the one task/inbox/outbox, its conversation
counters and completed River job. All11 container identities and images are
preserved; only Loom's start epoch changed during the temporary configuration
exercise. Loom history was compacted and is not claimed bitwise unchanged.
The new custody helper is `installed_primary_observer_v15.py`. Its first readback
rejected an inherited stale run count133; that failed check was retained and the
corrected epoch passes with134.

This closes the narrow native-navigation-to-reactive adapter/delivery gate after
explicit fixture recovery. It does not prove unattended model understanding,
summary quality, measured token costs or automatic recovery from an exhausted
platform-attempt limit. Provider token counters are synthetic. The complete
real-user registration/pass workflow matrix remains open.

Ignored evidence is `.cache/shuttle-work/native-pairs-history-v1/`,
`.cache/shuttle-work/native-pairs-history-install-v1/`, and
`.cache/shuttle-work/native-pairs-real-v1-qa/agent-context-v1/`. The source build
contains563 pinned files. The follow-up test PostgreSQL was archived, validated,
stopped with exit0 using a30-second DB timeout and removed with its owned volume.
Full registration/pass parity, broader same-owner workflows and interactive
CA/zitadeltg linking remain separate roadmap work.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
