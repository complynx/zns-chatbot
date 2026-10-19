# Archived tagged-keyboard design and evidence

Captured before the breaking conversational port on 2026-10-16. All statements
below describe that historical snapshot. They are not the current client contract
or acceptance of natural-language intent interpretation. See the current
[dialogue guide](../shuttle-agent-reply-keyboards.md).

# Agent dialogue and external operation notifications

## Owner clarification, 2026-10-16: target behavior

The agent owns interpretation of conversational replies. A question, approval or
cancellation request in the active dialogue is ordinary user text. Reply-keyboard
captions are plain suggested replies. Neither visible tokens nor hidden text tags
identify a task, question, choice or operation. Clicking a caption and typing the
same words have the same meaning. The latest pending question and conversation
history provide context; the transport must not infer approval from a keyword.

The user may approve in their own words, refuse, ask a question, add conditions,
or request cancellation. For example, `Approve, but only for one person` is a
qualified instruction, not an unconditional approval. `Cancel` is sent to the
agent as text; the agent decides what to stop from context and calls a scoped
cancellation API. If the target is unclear, the agent asks before acting.

The displayed keyboard belongs to the latest dialogue question. Resolving or
replacing that question updates/removes it. Pending dialogue context survives
restart and a switch between agent and native interfaces.

A notification outside the agent dialogue uses buttons attached to that specific
operation message. Such callbacks carry structured operation identity separately
from text, validate the current owner/permissions/business state, and do not steal
the conversational keyboard. Native registration/menu forms remain available;
their structured actions and receipts are exposed to the agent as context.

Business safety stays in the API: authorization, exact operation parameters,
preconditions, expiry, idempotency and recovery. Agent interpretation cannot bypass
those gates. An amended action needs a new preparation/consent binding. Unknown
outcomes reuse their original operation and idempotency key.

## Integration dependency and acceptance status

This target is **not implemented or deployed**. The earlier direct label-to-control
patch was withdrawn because it still interpreted user intent inside the transport.
The live owned stand remains on the historical tagged implementation below.

The inspected Loom source in `internal/store/execution.go` accepts `steering` text,
but its `addInput` only queues a waiting run for `answer` or `approval`. Approval
inputs accept only `approve`/`reject`; question text can be disabled by AllowText.
We need a Loom contract for processing ordinary user text while awaiting approval
or a question. Pending effects must remain blocked until the agent has processed
conditions and the authoritative API has validated the intended operation. Sending
steering without waking the waiting agent does not meet this requirement.

Acceptance must use real Loom for: caption and typed equivalent; qualified consent;
refusal; cancellation by caption and prose; ambiguous cancellation; clarification
followed by approval; restart while waiting and lost Input receipt; external
operation notification during a dialogue; native/agent continuation. Do not count
button-protocol or manually scripted provider checks as proof of interpretation.

## Historical tagged-keyboard implementation and acceptance

The evidence below describes the previous design. Its text-tag and direct
label-to-control rules are superseded by the target above. Keep these records for
recovery and rollout history; they are not acceptance of the clarified behavior.


The implementation sends agent question choices and approvals through Telegram
ReplyKeyboardMarkup. The owned stand now runs candidate ded0e98a2245/schema34,
following an image-only cutover from reply-keyboard image ba5a4c6af799.
All 65 application tables, configuration and eight companion instances were
preserved. A real-IAM owner profile read changed no table; the same Telegram poller
resumed successfully. Scoped native question and approval acceptance passed with a manually driven
provider; this is not unattended model or production acceptance.

## Choice binding and recovery

Each label has readable text and a reserved short suffix. Its64-bit SHA-256
identifier includes the parent task, full question/approval presentation and the
specific choice. Identical labels remain distinct. Approval identity includes
complete literal arguments; all argument chunks precede its keyboard. Interface
language selects Approve/Reject or Подтвердить/Отклонить. The suffix is an operation
reference, not a secret or an authentication credential.

Hash material canonicalizes JSON object order and exact numeric value. PostgreSQL
JSONB reordering and equivalent decimal/exponent spellings keep the same identity;
different integers beyond floating-point precision remain distinct. This changes
identity calculation only; displayed literal approval arguments are not rewritten.

Reserved suffix text becomes a control intent before command parsing. A stale,
unknown or malformed choice never starts a new agent run or becomes free-text
steering. The authenticated owner/conversation and routed target are checked;
the current Loom prompt, expiry, exact label and delivered outbox keyboard must
all match before the first Input is checkpointed. A bare Yes does not approve an
operation. Free-text question answers retain their existing AllowText, prompt-ID
and expiry checks. Existing inline callback formats continue to be accepted.

The receipt records a hash of actual wire markup. A pending or delivery_unknown
presentation, missing hash, or suppressed keyboard does not authorize a choice.
A user may see a button just before its receipt commits; acceptance stays closed
until delivery is established. No speculative repeat send is introduced.

Once Input has an uncertain outcome, recovery preserves its body and idempotency
key even if the current prompt changes, expires or the target finishes. Only a
known-unapplied attempt is revalidated against the current presentation. A control
with no active target is never promoted into a new agent request.

## Chat-wide lifecycle

The parent run owns keyboard presentation and cleanup. A new prompt replaces its
keyboard; disappearance, expiry, completion and cancellation remove it through
durable output. Expired prompts do not regenerate choices. Stale-choice errors
do not remove the current prompt's keyboard. Menu reads preserve unresolved agent
choices; navigation commands remain available by typing them. Registration event
and role buttons keep their separate inline interaction.

Reply-keyboard sends use bounded fixed conversation-lock stripes within the
single fenced application instance. The gate covers sending and receipt commit.
The latest actual keyboard delivery, rather than task creation order, determines
whether an old cleanup can remove it. Outdated markup can be omitted while the
message text is delivered; its receipt records the actual markup hash, so stored
original choices cannot falsely become proof of presentation. Database transactions
are not held across Telegram HTTP calls.

Telegram has no compare-and-set operation for chat keyboards. An uncertain old
HTTP send may arrive late and temporarily display an obsolete keyboard. Its token
still fails current-prompt validation. This implementation does not claim strict
ordering across concurrent application controllers.

## Rollout and acceptance

No new schema, dependency or external service is required. Existing outbox records,
legacy inline callbacks and frozen Input requests remain readable. New reply-choice
intents require a compatible binary. Before rollback, stop ingress, drain/reconcile
tasks/outbox and remove or reconcile all presented agent keyboards. Queue draining
alone does not clear client-side keyboards. Delayed new-format labels must still
be recognized and refused by the rollback binary or an explicit ingress filter;
an older binary without that guard can interpret them as new requests or steering.
Keep ingress stopped until a compatible image/filter is available. Do not discard
accepted controls or delivery_unknown records.

QA captures the complete release/test input set before execution, including
embedded migration SQL, avatar PNG and Mini App assets, and pins the manifest hash
through acceptance and cutover. Candidate build inputs must match that receipt.

Behavioral acceptance must cover choice and text answers, approve/reject with full
arguments, menu navigation while a question is pending, stale/delayed/foreign
choices, expiry, restart, uncertain Input with changed or completed target,
legacy inline callbacks, and cleanup ordering. The focused real-PostgreSQL/race
approval and cleanup probes passed. The full run failed when its disposable
PostgreSQL exhausted a 1 GiB tmpfs; its failed receipts are retained. A fresh run
uses an isolated disk-backed volume and keeps the same memory limits. That run
completed with one failing authorization-recovery test. A focused diagnostic
confirmed that the test cancelled a renewal before its receipt was saved: the
result was correctly `auth_unknown`, while the test expected expired-grace refusal.
The test now separates deadline preservation from expiry after a restart without
an in-flight renewal. Lost-receipt coverage remains separate. Independent review
found no issues with this test change. The fresh run passed 356 root scenarios
across internal/db, internal/api, internal/task and integration, with no failures
or skips; the three authorization scenarios also passed 10 repetitions each with
the race detector. Source inputs stayed unchanged. During post-test cleanup, the
disposable disk-backed PostgreSQL exceeded its five-second shutdown allowance and
was killed without OOM; this does not prove graceful PostgreSQL shutdown. That
cleanup failure is retained separately from the successful test receipts. At that checkpoint, real
Telegram/IAM/Loom acceptance remained required.
Native questions have passed in the authorized current-account Telegram chat:
choice and free-text answers, menu preservation, same-bot restart, stale-token
isolation and cancellation. Approval rejection reached the same Loom operation
with zero executor calls. The first approval attempt reached the exact operation
and argument digest once, but the synthetic leaf returned HTTP 409 instead of a
bound RPC result. Loom correctly kept that outcome unknown. This is a fixture
protocol failure, not clean approval acceptance; the original failed receipts
remain. The fixture now returns bound rejection/status results and persists its
receipts. The original failed run is retained separately from its subsequent recovery and
from the subsequent fresh clean approval run reported below.

The same-run recovery subsequently passed through an explicit owner resume and
standard Status lookup, with no extra run, Input or execute; its fixture was
removed. The initial failed acceptance remains failed. Live inspection also found
an application defect: Loom retains `question.answer` and `approval.decision`, but
the client in the then-installed candidate ba5a4c6af799 omitted them. A resolved keyboard can
therefore remain until its expiry while the run continues. That candidate is not
accepted for this lifecycle requirement. The fix in candidate ded0e98a2245 preserves these fields,
clears resolved prompt state, and filters resolved questions and approvals in both
keyboard and legacy callback handling. Local client/task tests, scoped lint and
independent source review passed. New behavioral coverage checks early removal,
stale answers, a new question after approval, and uncertain frozen Input replay.
The full image ded0e98a2245 built from 688 unchanged release inputs with zero
worker OOM events; its dedicated builder/cache were removed. The first fresh
Linux/race run passed 366 root scenarios and failed one existing delivery-order
test. Its setup changed mock runs before a run was created. A ten-repeat diagnostic
recorded two empty-run failures and eight existing-run passes. The test now waits
for the persisted running run ID before injecting the question. Independent review
accepted the scoped barrier; production source is unchanged. Failed receipts remain
under reply-keyboard-resolved-qa/ and reply-keyboard-delivery-diagnostic/. Their
containers and disposable volume were removed; PostgreSQL required a forced stop
without OOM during failed-run cleanup. This is not graceful PostgreSQL acceptance.
The distinct final Linux/race run passed all 367 root scenarios /583 root-and-
subtest pass events across internal/loom, internal/db, internal/api, internal/task
and integration, without failures or skips. The delivery-order barrier passed ten
repetitions, and the three focused lifecycle roots passed separately. All 778 QA
inputs stayed unchanged, matching the image release inputs. Strict scoped lint
reported zero issues. The final Go and PostgreSQL exited0/noOOM; PostgreSQL used
an explicit immediate post-test stop, which is not graceful/recovery acceptance.
Exact containers and the labelled disposable volume were removed. Receipts remain
under reply-keyboard-resolved-final-qa/. The fresh image-only cutover preserved all65 application tables, settings and
eight companion instances; a 2,295,749-byte backup was saved. The real-IAM owner
read changed no table, and the same poller resumed a fresh successful cycle.

Fresh native acceptance passed on ded0e98a2245 with real Telegram, IAM and Loom:
three question runs covered choice/text, menu, same-bot restart, stale-token
isolation and cancellation. For both accepted answers, an actual-wire removal
receipt and empty question_id were observed while the parent was still running,
before the final model response. There were three runs and two Inputs, nine
terminal local tasks, no business-table changes and no remaining active task.
Receipts remain under reply-keyboard-resolved-native-qa/.

The fresh approval run also passed. Both full literal parameter sets were shown
in two chunks before the keyboard. Reject produced zero executes; approve produced
one execute with the expected operation ID and argument digest. The synthetic RPC
leaf refused that execute with a bound result; the operation became failed and the
model received exactly {"status":"failed"}. Both decisions removed their keyboards
while the parent was running and before the final model response. Business tables,
IAM permissions, Loom configuration, bot/poller and companion instances stayed
unchanged. Its64MiB fixture was removed. Receipts remain under
reply-keyboard-approval-v2-qa/. The earlier failed acceptance stays failed; this is
a separate run. No real financial or privileged business operation was executed.

The implementation cannot prove unattended model choices through a manually
driven provider.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
