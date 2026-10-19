# Readable specialist-access approval: 2026-10-18

## Change and scope

An actual isolated specialist-access approval contained its canonical human
summary followed by redundant JSON with event/user UUIDs and technical versions.
The task presentation now treats the nonempty service-generated description for
zinusya_administration.set_massage_specialist_binding like the existing complete
massage booking/cancellation descriptions. Telegram displays those human terms
and its ordinary reply keyboard. Exact arguments, argument hash, current input,
operation/proposal identity and backend consent checks remain internal.

Missing/whitespace-only descriptions keep the literal argument fallback. Other
operations retain their existing complete-parameter presentation. This is a
presentation change, with no migration or wire-contract version change.

## Verification

A focused Linux Go1.27.2/PostgreSQL race gate passed4 root scenarios /14 test pass
events, with zero failures or skips. It covers actual task/outbox presentation of
human terms and empty-description fallback, generic full-parameter chunking,
existing massage quote/free-text/restart behavior and specialist consent denial.
The configured linter reports0 issues using explicit local Go1.27.2 and a Linux/
amd64 target. Fresh independent source review found no actionable findings.

The first host lint attempt timed out and was not accepted. A second failed
because automatic toolchain verification was disabled with GOSUMDB=off. Inspection
showed PATH selected Go1.27.0; the successful run selected the already available
Go1.27.2 executable/GOROOT with GOTOOLCHAIN=local. Both earlier failures remain in
ignored logs. No product code changed to resolve these environment failures.

The unmodified release Dockerfile built Linux/amd64 from856 frozen release files:

sha256:72acda129fa105c9830de1bb8a1c6ca28616aecb8f1823c981e0853d94e7ada3

The local image is63,607,949 bytes. A dedicated builder used at most2GiB/no swap,
two CPU and one concurrent build step; no OOM occurred. Its container/cache was
removed after the successful build. The disposable race containers and their
private database volume were removed as well.

## Actual IAM/Loom acceptance

A fresh qa-presentation stand used that exact image, real IAM/PostgreSQL/Loom,
synthetic Telegram and the counted manual model provider. Native preparation
created no access. The agent adopted the original proposal and expiry. The actual
bot text exactly equals its canonical human description, with no JSON, technical
versions, UUIDs or operation tags. The reply keyboard contains Approve and Reject.

The synthetic administrator then replied exactly: Yes, grant that access.
There were no IDs in this reply or the initial human request. Actual Loom
interaction resolution used the latest accepted input and the pending approval's
unchanged arguments/hash. It created one binding effect at version1, one canonical
operation and two interface aliases. The same run and both tasks completed.
All3 expected outgoing messages were delivered: the approval, ordinary response
acknowledgment and final result. A driver initially expected only2, then corrected
that assertion using the saved real effects; no send or effect was repeated.
No model request or user operation remained pending.

The disposable qa-presentation containers, volumes and network were destroyed
with evidence retained. The two reusable QA stands remain stopped with their data.
All10 primary shuttle-live-test service fingerprints remain unchanged. Its bot
still runs schema47 / mvp-51; this local schema48 / mvp-54 image is not installed
there. The Git index remains empty. No commit, push or production change occurred.

Evidence: .cache/shuttle-work/massage-binding-readable-v1-qa/ for source/race/lint;
.cache/shuttle-work/massage-binding-readable-images-v1-qa/ for release provenance;
.cache/shuttle-work/massage-binding-readable-runtime-qa/ for actual runtime proof.
Raw proofs and credentials remain ignored. This gate does not prove browser UI,
Telegram WebView, unattended model quality, full domain parity or production
readiness. Earlier business-flow acceptance used the previous image and remains
separately recorded in [the domain report](shuttle-isolated-domain-qa-2026-10-18.md).

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
