# Avatar operator handoff runtime QA, 2026-10-19

## Scope and candidate

The schema59 / manifest mvp68 candidate passed both repair handoffs on the owned
avatar-admin-r1 stand. The unmodified release Dockerfile built a Linux/amd64 image:
`sha256:68d82bd9d01e15a6c04464e36555c4e9252acd4a273f93b2f96798a2a74292f4`.
The build/source freeze matched the reviewed operator UI gate. The bounded2GiB,
2CPU builder and its cache were removed after the build without OOM.

The stand used real PostgreSQL, Zitadel and the owned frozen Loom fc6c574 image.
Telegram was simulated. The Codex-fed provider supplied deterministic tool calls
and responses through the real Loom loop. This proves the infrastructure and
contracts; it does not prove unattended model interpretation, the actual Telegram
WebView or production readiness. No real Telegram message or primary deployment
was performed. No Loom developer checkout or stand was changed.

## Form to agent

The owner created two avatar artifacts through the candidate and real worker.
Mock Telegram accepted each document and disconnected before returning a receipt.
Both deliveries persisted as delivery_unknown and were not automatically resent.
A synthetic administrator had zinusya.admin in both API and Loom IAM projects.

The full shipped Mini App read the first delivery, prepared an explicit retry and
saved owner-bound recovery IDs. Preparation did not change the delivery. A later
explicit Telegram-simulator request started a real Loom run. Its prompt contained
the prior native proposal locator, marked as history rather than consent.
The agent read that exact proposal through a script read and adopted it under a
new operation using resume_proposal_id. The canonical proposal, arguments and
original expiry were unchanged; exactly one alias was added.

Loom required a fresh exact broker approval. The approval text reached the mock
Telegram chat before consent. A normal bot SIGTERM completed with exit0 in0.672s,
without OOM or SIGKILL. The same pending task, Loom run and canonical proposal
survived; the other stand services were unchanged.

After explicit free-text consent, the original operation committed one repair.
Its immutable result was pending; the worker later delivered the existing artifact.
A fresh full Mini App browser restored its saved operation and read that same
result, separately from current delivered state. This recovery issued no POST.

## Agent to form

A second explicit request made Loom prepare dismissal of the other uncertain
delivery. The exact approval reached the mock chat, with no delivery effect yet.
The browser found that actor-owned pending proposal, reviewed its original
operation and explicitly confirmed it. Selection made no prepare POST; confirmation
made exactly one confirm POST under the original operation.

The user then explicitly told the agent that the form had confirmed the dismissal
and allowed completion of that same operation. Broker completion returned the
identical saved result. Neither the delivery version nor audit result changed,
and no additional document was sent. This case includes that explicit reactive
follow-up; it does not claim proactive synchronization of an idle Loom run.

## Read-back and authorization

Final read-back found two artifacts, two deliveries, two completed canonical
resolutions, one adoption alias and two native action records. Four physical bot
tasks and two Loom runs completed. There were three sendDocument attempts and
three outgoing documents: the two accepted originals and one explicitly permitted
last-resort retry. No new avatar was generated during repair.

Original attempt IDs, receipts, actor and reasons remained in immutable results.
Canonical status reads returned those exact results. A nonadministrator was denied
native pending discovery and completed-operation replay through both native confirm
and agent confirm, all with403. The denied replays changed no resolution, delivery,
alias or native action row. The broader session/expiry/invalidation checks remain
covered by the separate native administrator source/PostgreSQL gate.

## Retained failures and cleanup

The first read used module_call and Loom correctly returned use_script_for_reads.
The original run continued through the documented script-read path. No replacement
task, proposal or delivery effect was created for this correction. This was a QA
caller error, not a Loom defect; no new Loom issue was filed.

An initial final-readback query used a nonexistent runs.status column. It was
corrected to runs.state and repeated without replaying business operations. The
failed SQL diagnostic is retained.

The first cleanup archived all three databases and nine service logs, then hit a
Windows access error while replacing its checkpoint file. The safety path stopped
the owned stand and retained its volumes. Recovery verified every archive hash and
all nine log files before destroying only this stopped project. The original
failed cleanup proof is retained alongside the successful recovery proof.

The nine temporary containers, project volumes and network are removed. Shuttle,
Loom and Zitadel dumps and private logs remain in the ignored evidence directory.
The primary eleven-container stand remains schema49/mvp56 with unchanged images,
configuration and preserved domain state. The new candidate is retained for later
acceptance or installation; it has not replaced the primary.

Local evidence: `.cache/shuttle-work/avatar-admin-runtime-v1-qa/` contains
runtime-acceptance.json, checkpoint.json, three browser result/screenshot sets,
private transcripts, three private dumps, cleanup-proof.json and
cleanup-recovery-proof.json. Build evidence is in avatar-admin-images-v1-qa;
source/UI evidence is in avatar-admin-ui-v1-qa. The final source-gate.json records
independent review and evidence hashes. The overall roadmap remains active.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
