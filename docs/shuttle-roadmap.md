# Current roadmap checkpoint, 2026-10-22

## Added acceptance requirements: identity onboarding

The [isolated synthetic-Telegram/real-IAM fixture](shuttle-prelink-real-iam-qa-2026-10-22.md)
now completes canonical-account creation/linking and full OIDC without the IAM
external-user registration form. Both provider-first/Mini-App-first paths,
sequential relogin and completed-login restart pass. Activation and final real
Telegram browser acceptance remain open; do not request another human Telegram
authorization until that flow is prepared.

The [specification](shuttle-spec.md) now also requires first-contact real-name
onboarding, reuse of filled names from an existing linked IAM account, agent-selected
Telegram name suggestions through a reply keyboard, free-text fallback, and later
name edits synchronized to Zitadel through durable versioned work. Technical
provisioning placeholders must not satisfy onboarding. Profile names and separate
legal registration details retain their distinct purposes.

Required behavioral paths: new user with a plausible suggestion; new user without
one; an existing identity with or without usable names; typed correction instead
of the keyboard choice; later correction through native UI or agent; cancellation,
restart and repeated start; IAM outage followed by recovery; consecutive edits
that must converge to the latest name without modifying another account or its
permissions. These requirements are recorded, not yet accepted as implemented.

The current installed candidate adds `mvp-92/schema72`: structured first/last
profile storage, atomic durable IAM-sync intent, River repair/retry and periodic
name verification, plus separate agent and Mini App name inputs. Focused
PostgreSQL/mock-IAM checks cover outage/restart, late older writes, scope,
immutable receipts and mixed agent/form corrections. Browser form QA with a
synthetic HTTP peer covers compound names, en/ru labels and the same write after
an uncertain reply and reload. Explicit provisioning provenance now permits
safe adoption of existing IAM names, preserving concurrent profile changes and
active name dialogs. Technical or unknown provenance still requires confirmation.
Stop the old application before migration 72 so old provisioning cannot bypass
the provenance ledger. This does not complete the first-use onboarding,
real-model first-contact dialogue acceptance. The newer primary candidate
installation is described below; ingress remains fenced.

New `/name` messages now use the agent dialogue. Context supplies untrusted
Telegram name/username hints, suggestion and confirmation rules; the Mini App
retains an agent-free name form. Scripted-provider checks are not model judgment
acceptance. Previously admitted native name tasks retain their recovery handler.

New `/start` messages without filled local first/last names now enter the agent
dialogue; dispatch can first reuse names from a verified existing IAM account.
Filled local profiles keep native `/start` navigation while Loom is down.
Duplicate updates retain their original task even after a profile is completed.
This is source and scripted-provider coverage, not real-model acceptance.

The real owned IAM adapter/store name-update gate also passed with a fresh
synthetic identity: an injected client outage, store reopen and newer edit
converged to the latest compound name while preserving other profile fields,
email, username and one provider link. The grant baseline was empty. This is
not a River/process-restart or primary installation claim; see the
[name synchronization QA](shuttle-profile-name-sync-qa-2026-10-22.md).

The subsequent isolated installation also passed the real River-worker gate.
Image `0d4457b6` from `74604b7e` migrated the owned synthetic prelink database
70→72, preserved its existing rows, retained a pending name during an actual
IAM pause, stopped gracefully in 0.546 seconds and recovered the latest edit
after application restart. A temporary 30-second authorization cache enabled the
outage request; its original zero setting was restored. The temporary candidate
container was retired without OOM. This isolated gate alone does not prove the
primary installation or real-model onboarding acceptance.

## Current candidate installation, ingress fenced

Bot source `74604b7e` / image `0d4457b6` is installed on the owned primary stand
with `mvp-92/schema72`. Loom API and sandbox now use published revision
`b2b8d429` / image `9ddd3175`, application schema 5. Backups passed temporary
restore and original-row/sequence comparison; migrations preserved original data
before workers started. Compose-declared environment, compared startup settings
and mounted-file digests matched the original; image-provided environment defaults
and unlisted container settings were not compared.

The first final observer failed because of a Python module/helper name collision;
a separate reviewed read-only observer passed without replaying migration or
replacement. Both receipts are retained. Telegram ingress and the counted model
provider remain stopped. A separate same-image mock Telegram stand passed one
actual `gpt-5.4-mini`/`low` decision: the model selected `request_name`, real Loom
executed it, and a durable expectation was created without saving Telegram hints.
The next model step remains unanswered. All eight requests in the bounded first
series are accounted for; complete dialogue acceptance requires another bounded
series. Next: complete that acceptance and resume ingress through a reviewed
continuation. Real Telegram authorization
remains deferred until the user is available. See the
[name synchronization report](shuttle-profile-name-sync-qa-2026-10-22.md).

## Previous accepted installation checkpoint

The previous accepted primary installation used application source `4f03e5ff`, `mvp-91/schema70`,
with both Loom runtime processes on published revision `34f46468`.
The [installation acceptance](shuttle-candidate-install-qa-2026-10-22.md) preserves
original data, identities, config and receipts and resumes the original ingress.
Its config-bind drift and explicit recovery remain in the report. The bot stopped
by SIGTERM in0.687 seconds. This is a local stand, not a production cutover.

The preceding primary installation ran source `3be6e40b`, `mvp-89/schema69`.
The [reviewed release](shuttle-reviewed-release-qa-2026-10-21.md) passes
[CI46](https://github.com/complynx/zns-chatbot/actions/runs/39664578385): 870
integration roots, 369 UI tests, generated SQL, lint and Linux/container build.
Its separate candidate check covers native menus/registration with Loom stopped,
exact name-expectation continuation through actual Loom and graceful restart.
The image-only primary replacement is accepted; original ingress, business data,
configuration and Loom state are preserved. No production cutover is included.

The preceding
[meal financial installation](shuttle-meal-primary-qa-2026-10-21.md) passes controlled
cutover, original ingress recovery, all18 installed assets, six anonymous auth
boundaries and preservation of old domain data. Source commit `f78f7096` passes
[full CI](https://github.com/complynx/zns-chatbot/actions/runs/39600531384):864
integration roots and367 UI cases. Authenticated meal workflow evidence comes
from the separately retired isolated stands; the primary HTTP check does not
replace it.

The following review fixes were validated before the current installation. Commit
`ab59d761` passed [CI43](https://github.com/complynx/zns-chatbot/actions/runs/39642330245):
867 integration roots, 368 UI cases, generated SQL, lint and Linux/container
build. Commit `3186ba69`, including bounded knowledge receipts, native payment
authority fencing, specialist queue metrics and run custody through prolonged
401 responses, passed [CI44](https://github.com/complynx/zns-chatbot/actions/runs/39650197024):
869 integration roots and 368 UI tests. Commit `a7ba9287` subsequently passed
[CI45](https://github.com/complynx/zns-chatbot/actions/runs/39655949519):870 roots,
368 UI tests, generated SQL, lint and Linux/container build. The subsequent
[graceful HTTP](shuttle-graceful-http-qa-2026-10-21.md) and
[payment-session expiry](shuttle-payment-session-expiry-qa-2026-10-21.md) fixes
have focused Linux/race evidence and are included in the current installation.
Their original reports retain the earlier source-only boundary. Source checks
alone do not prove installed behavior or production readiness.

The [full Go review and test-fidelity follow-up](shuttle-integration-review-qa-2026-10-21.md)
records complete static read coverage and the separate focused runtime gate.
Its source review and focused tests remain distinct from the separately recorded
installation and bounded workflow acceptance.

The preceding
[avatar source installation](shuttle-avatar-primary-qa-2026-10-21.md) is accepted
after the earlier knowledge and order-review preview installations. Full CI for
source commit `0599dfef` passes 835 integration roots and 357 UI tests. The bot
was replaced, the original ingress resumed, and domain data and Loom state were
preserved. The test-proxy privacy-header fix at `c4d3d2b4` also passed its
[full CI](https://github.com/complynx/zns-chatbot/actions/runs/39540930166): 835
integration roots, 357 UI tests and 20 simulator tests. The chronological records
below retain each earlier result; later success does not rewrite failures.

The [bounded model-assisted knowledge probe](shuttle-knowledge-model-qa-2026-10-20.md)
now passes four Russian/English current/history/glossary/missing-policy dialogues
with model-generated tool choices and actual Loom/IAM. All four replies are
delivered, the six knowledge tables are unchanged after seeding, and the nine
temporary services are archived/removed. Its 15 model turns use a synthetic
mailbox provider; this is not native-provider or broad model-quality acceptance.

The separate [operation-adoption consumer gate](shuttle-loom-adoption-acceptance-2026-10-21.md)
passes the supported knowledge suggestion handoff against published Loom
`34f46468`: native/agent continuation, exact consent, concurrent confirmation,
completed receipt replay, pending-operation restarts and rejection. The candidate
is application source `4f03e5ff`, schema70 / `mvp-91`; the candidate is now installed on the
primary through the separate accepted installation gate above. Its temporary nine-service stand was
archived and removed. Browser-source quota/retention is also implemented in this
candidate; see [its policy and acceptance limits](shuttle-avatar-source-retention.md).

## Next actions, in order

1. Preserve the accepted [knowledge operation handoff](shuttle-loom-adoption-acceptance-2026-10-21.md)
   and retain the accepted candidate installation epoch. The published Loom
   [revocation lifecycle](shuttle-knowledge-revocation-qa-2026-10-22.md) now passes
   the bounded actual-IAM method-loss, rights-restoration, graceful-restart and
   stale/fresh-consent path on a retired isolated stand. Uncertain dispatched RPCs
   and other revocation windows remain separate. The separate
   [abrupt-exit gate](shuttle-loom-abrupt-recovery-qa-2026-10-22.md) also passes
   automatic original-run recovery, unknown inference accounting and one final
   delivery on actual IAM/published Loom; other crash windows remain separate.
   [Loom #27](https://github.com/complynx/loom/issues/27) is merged and published.
   Nine focused Linux/race/PostgreSQL/OTLP tests passed independently, including
   consumed steering and restart; see [source acceptance](shuttle-loom-durable-tracing-qa-2026-10-22.md).
   Install the published image through the candidate procedure and finish
   Zinusya consumer tracing across its durable task and Loom queue. The earlier
   [bounded trace report](shuttle-tracing-qa-2026-10-15.md#open-trace-boundary)
   remains historical HTTP propagation evidence.
   Do not change the Loom developer's stand.
   Orders submit original-operation adoption is now in source acceptance; see
   [its contract and remaining gates](shuttle-order-operation-adoption.md).
   The authenticated browser import and durable inline WebApp launch button are
   implemented in source. Continue with actual Loom approval/browser acceptance
   while preserving the original operation and newer drafts. The primary is unchanged.
2. Complete deferred joint CA/ZitadelTG and real-Telegram Mini App acceptance
   when Daniel is available. Revalidate the retained configuration first;
   isolated HTTP/Edge results do not replace that joint check.
3. Meal financial checkout is implemented and installed, with no empty orders,
   one explicit common event proof-arrival deadline and unlimited quantities.
   The [submission contract](shuttle-meal-order-submission.md),
   [proof/review contract](shuttle-meal-payment-evidence.md),
   [bounded runtime QA](shuttle-meal-financial-runtime-qa-2026-10-21.md),
   [final-image browser QA](shuttle-meal-final-browser-qa-2026-10-21.md) and
   [primary installation](shuttle-meal-primary-qa-2026-10-21.md) record exact scope.
   The [controlled-agent payment QA](shuttle-meal-agent-payment-qa-2026-10-21.md)
   also accepts actual Loom proof submission and full reviewer consent, with one
   confirmed665minor payment, no capacity allocation and ordinary-owner denial.
   The stale-draft fix passes actual409 UI recovery, resave and one explicit order.
   [Full local application checks](shuttle-meal-release-qa-2026-10-21.md) and
   the application release's remote CI pass; original fixture/harness failures
   remain retained.
   The [dual-dependency recovery check](shuttle-meal-recovery-qa-2026-10-21.md)
   accepts a consent queued with Zitadel/Loom stopped, graceful bot restart,
   and completion of the original approval with one financial review. Other
   recovery windows and exact native-preparation adoption remain separate gates.
4. Finish real export/account/avatar migration and server-specific cutover
   acceptance before any separately authorized production deployment. Historical
   payment records stay reference material, not credits in the new live ledger.

## Knowledge acceptance boundary

General and event-scoped publication, named moderation, immutable revisions,
search/navigation and event archival have bounded source/runtime evidence.
Native browser editing has separate general and event acceptance with Loom
stopped; actual-Loom write paths use a controlled provider. See the
[general browser](shuttle-knowledge-browser-runtime-qa-2026-10-20.md),
[event browser](shuttle-knowledge-event-browser-qa-2026-10-20.md) and
[event runtime](shuttle-knowledge-event-runtime-qa-2026-10-20.md) reports.
These are not unattended model-quality, complete mixed-path or production claims.

# Historical progress records

The [event knowledge browser gate](shuttle-knowledge-event-browser-qa-2026-10-20.md) now passes native participant suggestion/reload, independent event-admin approval/edit, wrong-event admin controls and old/current revision reads in five fresh Edge contexts with real IAM and Loom stopped. One article/two revisions/three content operations and zero Loom runs are verified. All nine temporary services are archived/removed; primary installation, full CI and exact prepared-operation native-to-agent adoption remain separate gates.

The [moderator revocation observation](shuttle-knowledge-revocation-runtime-qa-2026-10-20.md) preserves zero publication after rights are removed and the bot is gracefully restarted (stop: 0.625 seconds). Actual Loom returns `tool_failed` and retains `approved`; current rights and the original operation are then explicitly read by the agent. This is a bounded safety snapshot, not full rejection/recovery acceptance. The linked proposal records missing diagnostic/lifecycle guarantees without attributing the generic error to a proven internal cause.

The [native-operation adoption investigation](shuttle-loom-native-operation-adoption.md) reproduces a remaining mixed-path gap: actual Loom reads a native preparation but a supported write call creates a distinct operation. No consent or business effect was sent. Exact prepared-operation native→agent handoff remains unaccepted; the linked document proposes the required generic Loom contract.

The [real-browser knowledge gate](shuttle-knowledge-browser-runtime-qa-2026-10-20.md) now passes participant suggestion with prepared-page reload, independent moderator approval/edit and original/current revision reads in four isolated Edge contexts, while Loom is stopped. It creates one article/two revisions/three business operations and zero Loom runs. The nine-service stand is archived/removed. Mobile/real-Telegram and exact prepared-operation browser→Loom adoption remain open; full remote CI and primary installation are still separate gates.

The [event knowledge runtime gate](shuttle-knowledge-event-runtime-qa-2026-10-20.md) now passes native suggestion → real Loom event-admin review → plain consent → one publication, followed by automatic archival and a historical moderator edit. Cross-event/current-versus-historical permissions, search transfer, original revision and review receipt are checked. The nine-service stand is archived/removed. This is native HTTP and controlled-provider evidence; browser editing and autonomous model quality remain separate. Primary remains mvp81/schema65 pending complete CI and installation.

The [avatar CI follow-up](shuttle-ci-followup-2026-10-20.md#avatar-integration-process-boundary) now uses the deployed leaf-worker boundary in HTTP fixtures, retaining production timeouts and race checks for the server. The selected Linux/PostgreSQL gate passes24 roots/30 cases and is archived/removed. A separate diagnostic passes the four failing CI avatar roots at0.5 CPU and is also archived/removed. Exact-source remote CI remains open; primary stays mvp81/schema65.

The [knowledge content runtime gate](shuttle-knowledge-content-runtime-qa-2026-10-20.md) now passes native-API suggestion → actual Loom moderator review → free-text consent after graceful restart → one publication and identical native replay. The fresh r2 stand is archived/removed; failed r1 expiry and a recovered Windows cleanup-receipt failure remain retained. This covers controlled-provider integration, not browser editing, separate-device handoffs or autonomous model comprehension. Primary remains mvp81/schema65 pending full CI.

The [binary compatibility gates](shuttle-knowledge-resolution-compat-qa-2026-10-20.md) pass83/82/83 and a separate83/81/83 on populated schema67, retaining original notice/audit rows and native writes made by each preceding binary. Both temporary stands are archived/removed; primary81/schema65 remains unchanged. Main installation remains open and must prevent synthetic fixture recipients from real-Telegram notice delivery. The [CI follow-up](shuttle-ci-followup-2026-10-20.md) records the later808/814 CI result. Same-source pool4/8 probes identify the two lock-observation failures; deterministic fixture capacity passes nine roots/13 cases. Four avatar failures and full remote CI remain open, so the primary installation gate stays closed. The runtime renderer is unchanged. Knowledge UI coverage is now included in CI (344 combined local UI cases pass).

The [complete bounded Linux gate](shuttle-linux-batched-full-qa-2026-10-20.md) now passes786 roots/1169 cases at commit387716bf, with its stand archived/removed and primary preserved. The separate [knowledge foundation](shuttle-knowledge-foundation-2026-10-20.md) and discovery/continuation follow-up pass12 critical roots/21 cases, strict lint and source review. The native UI is wired in source; mvp81 adds named moderator management and assignment recovery with26 focused knowledge cases (58 including existing Mini App lifecycle cases); six native HTTP/command integration roots also pass on PostgreSQL/race with mocked upstreams. The owned stand now runs schema65/mvp81. Real Telegram `/knowledge` opens the authenticated form; an ordinary user submitted and read a synthetic pending suggestion without publication or a Loom run. Real native-to-agent reads also pass with the controlled provider. Independent fixture publication/replay, corrected deployed read/outline/find and real Telegram published article/history reads pass. The original422 failure remains retained separately. The installed mvp80 fix also passes strict lint, full API tests and six PostgreSQL/race roots/nine cases; mvp81 is installed after graceful restart and decoded backup, with all eleven services preserved except the intended bot recreation. Actual IAM/native-agent HTTP moderator revoke/restore and same-receipt replay pass; this is not yet a model-driven Loom moderation dialogue. The [knowledge-notice source slice](shuttle-knowledge-notifications.md) adds bounded durable delivery in uninstalled mvp82/migration66, with25 PostgreSQL/race roots/42 cases and60 Mini App cases passing. The separate [full real-IAM/Loom/mock-Telegram gate](shuttle-knowledge-notices-runtime-qa-2026-10-20.md) now passes six delivery scenarios and controlled reactive Loom readback, including current-rights checks and three graceful restarts below one second. Main remains mvp81/schema65. [Exact delivery resolution](shuttle-knowledge-notice-resolutions.md) is now implemented in uninstalled mvp83/migration67: the knowledge and existing notification-resolution PostgreSQL/race selection passes 34 roots and 63 cases, with package tests, pinned sqlc and source reviews passing. Its [full real-IAM/Loom gate](shuttle-knowledge-resolution-runtime-qa-2026-10-20.md) now passes direct diagnostics, exact retry/dismiss, ordinary-user denial and one controlled Loom retry through full-terms presentation, pending-consent graceful restart and plain-text consent. All nine temporary services are archived/removed; primary remains schema65/mvp81. The linked content runtime gate now accepts one general native-API/agent moderation path. Browser editing, separate-device and event-scoped write dialogues, main installation and remote CI remain open.

The [bounded Linux follow-up](shuttle-linux-batched-qa-2026-10-20.md) now passes four critical roots/11 cases,316 UI cases and37 API/task roots. The preceding eight-batch run retains784/785 passing roots and one corrected false-positive assertion. A meal deadline test now runs on Linux after its accidental Windows suffix was removed. That786-root repetition passed at the snapshot above; installation of the reviewed UI fixes remains open; both temporary stands are archived/removed and the primary is preserved.

# Earlier accepted runtime gates, 2026-10-20

At the earlier schema64 / `mvp-78` stage, the primary had the
[payment-consent fix installed and binary rollback verified](shuttle-payment-consent-primary-qa-2026-10-20.md).
Eleven services, domain data, queues and the contents of 134 Loom runs are preserved.
The preceding [massage mvp78 installation](shuttle-massage-primary-qa-2026-10-20.md)
remains a separate historical gate.
The [payment-consent presentation fix](shuttle-payment-consent-presentation-2026-10-20.md)
now passes focused component checks and fresh source reviews. It restores the
original human terms without technical JSON and preserves reconciliation after
native/agent completion. Packaging and the
[owned runtime gate](shuttle-payment-consent-runtime-qa-2026-10-20.md) now pass:
two completed Loom runs, one 30 BYN credit, and three actual recipient-checked
notifications. The nine temporary services are archived/removed. The separately
linked primary gate now verifies fixed/original/fixed images, 17 installed HTTP
assets, three unauthenticated 401 boundaries, and original ingress recovery.
Manifest78/schema64 stay unchanged; the exact image digest identifies the fix.

The [actual token-expiry and dual-outage gate](shuttle-iam-expiry-dual-outage-qa-2026-10-20.md)
now passes separately: natural short-session expiry while IAM and Loom are down,
cold bot restart, three persisted authorization waits and three completed runs.
The original task/run/session survive and session version advances. Existing and
new users retain verified IAM identities and receive one reply each. Both owned
nine-service stands are archived/removed; the first QA observer failure remains
recorded. Multi-hour outages, business approval expiry and broader faults remain open.

The preceding [mvp77 primary installation](shuttle-pair-terms-primary-qa-2026-10-20.md) verifies binary-only77/74/77 rollback,
original ingress recovery and installed HTTP/auth/custody checks. [Payment installation, authentication fix and binary rollback](shuttle-native-pass-payment-primary-qa-2026-10-20.md)
are accepted. [Pair assignment installation](shuttle-native-pair-assignment-primary-qa-2026-10-19.md)
records the preceding schema63/mvp72 stage. The preceding [solo assignment](shuttle-native-pass-assignments.md)
four-path and [schema61/mvp69 installation](shuttle-native-pass-assignment-primary-qa-2026-10-19.md)
gates remain historical accepted stages.

The [native pair assignment workspace](shuttle-native-pair-assignments.md)
is implemented in source schema 63 / `mvp-72`, including current administrator
sessions, both recipient identities, shared history and both handoffs. Source
checks pass: the mvp71 baseline has 1037 Go roots and 285 UI cases; the mvp72
bounded agent-identity follow-up has 58 affected PostgreSQL roots, lint/sqlc and
fresh independent reviews. The approval-presentation follow-up passes 14
PostgreSQL scenarios, affected units/lint and final independent reviews.
The owned real-IAM/Loom/mock-Telegram/Edge four-path gate now passes: eight
passes, four assignment receipts and three completed Loom runs. A 0.703-second
bot stop preserves the original pending approval. Both temporary runtime stands
were archived and removed. Local-primary installation/rollback now passes;
whole mixed pair joining-through-assignment happy paths now pass on candidate mvp75 below. Participant
payment/top-up/ambassador runtime parity is accepted separately below. The earlier
[canonical foundation report](shuttle-native-pair-assignment-foundation.md)
records the preceding schema62/mvp70 component stage.

The [participant pass-payment source slice](shuttle-native-pass-payment-evidence.md)
now passes schema64 / `mvp-73` component gates: 64 PostgreSQL roots, 304 UI cases,
affected units, lint0, exact sqlc and a Linux cross-build. Three review rounds
closed a session-expiry write race. Its [owned runtime matrix](shuttle-native-pass-payment-runtime-qa-2026-10-19.md)
now passes all four participant paths: eight receipts/reviews, both handoffs,
lost-response recovery and a 0.828-second graceful stop. All temporary services
were archived and removed. The auth follow-up is installed at schema64/mvp74
after20 API unit roots,18 authorization cases,20 PostgreSQL roots, lint, Linux
build and actual primary rollback/HTTP checks.

The separate [display-name four-path runtime gate](shuttle-profile-four-path-runtime-qa-2026-10-20.md)
now passes on the unchanged mvp74 image: native with Loom stopped, agent and both
handoffs, including the original question/run across a 0.875-second graceful stop.
Nine profile receipts, two actual form writes and three completed Loom runs are
verified. The nine-service temporary stand was archived after writer shutdown and
removed. This does not close the whole registration/pair/payment journey.

The [complete native joined journey](shuttle-joined-native-runtime-qa-2026-10-20.md)
now passes from fresh participant registration through pair consent, exact native
two-pass assignment and both deposits/top-ups reviewed by an ambassador. Loom is
stopped throughout those actions. All four queued files later reach two real Loom
runs, with controlled acknowledgements and unchanged business state. The temporary
stand is archived/removed. Stop command wall time is4.125s; logged application
drain is10.54ms. Exact full process exit within1s is not proved by those separate
measurements. The [whole agent joined gate](shuttle-joined-agent-runtime-qa-2026-10-20.md)
now passes separately: two fresh registrations, one pair, two paid passes, four
reviews and15 completed real Loom runs with a controlled provider. The original
assignment approval survives restart; live matched Docker signal/exit events
measure0.530 seconds for this probe. Four exact original file captions are audited.
The nine-service stand is archived and removed; primary stays unchanged. Whole
mixed happy paths now also pass on [candidate mvp75](shuttle-joined-mixed-runtime-qa-2026-10-20.md): 22 completed runs, four paid passes and eight reviews across two fresh stands. Both stands are archived/removed. That mixed gate left the primary at mvp74. The subsequent mvp77 installation below is separate; broader fault/timing gates remain open.

The [structured native terms candidate](shuttle-pair-assignment-terms-2026-10-20.md)
is now packaged at mvp77. New preparations show original event/window/cap without
technical description IDs; legacy proposals retain their original description.
The component gate passes 1,061 Go roots (six recorded Windows skips), 310 UI cases,
two additional compatibility roots and five Linux process roots. The subsequent
[isolated binary runtime gate](shuttle-pair-terms-binary-runtime-qa-2026-10-20.md)
now passes both directions through real IAM and nine actual form invocations,
with one receipt and two unpaid passes. The temporary stand is archived/removed.
The candidate is now installed on the primary through the separately linked installation gate. Full Linux/race and real Telegram acceptance remain separate.

The [native initial-deadline and late-decision gate](shuttle-pass-deadline-runtime-qa-2026-10-20.md) now passes separately on mvp77:
real45-second timing, a graceful restart, two retained partially paid passes,
three proofs, two confirmations and one rejection. Total credited money is60.00
BYN, with zero Loom runs. The QA driver's read-only failure is retained; all nine
temporary services are archived/removed. Sales closing and agent-driven deadline
variants remain open.

The [whole native service-order gate](shuttle-orders-whole-native-runtime-qa-2026-10-20.md)
now passes on mvp77 from eligible fixture passes: real owner selection, full quote,
submission, original mock-Telegram files, capacity admission and ambassador forms
with Loom stopped. Two orders, three claims, two rejections, one25.68-BYN
confirmation and12 notices are verified. Original queued media and immutable
receipts are retained; the nine-service stand is archived/removed. The agent
gate below is separate; mixed order/reviewer and bounded owner-proof handoffs
pass below. Meals checkout and fulfillment remain open.

The [whole agent service-order gate](shuttle-orders-whole-agent-runtime-qa-2026-10-20.md)
now passes separately on mvp77: eight controlled-provider real Loom runs, two
orders, three claims, two rejections, one25.68-BYN confirmation and12 verified
notices. All original media tasks complete. Two QA observer failures retain their
original evidence and continue the same first task/run/order without a repeated
business effect. Nine services are archived/removed; primary stays unchanged.
Both [mixed order/reviewer workflows](shuttle-orders-whole-mixed-runtime-qa-2026-10-20.md)
now pass on mvp77:16 completed runs, four orders and six decisions across two
separate stands. All18 services are archived/removed with exit0 and no OOM.
Owner proof submission remains agent-driven in those two workflows. A separate
[owner-proof handoff gate](shuttle-orders-owner-proof-handoffs-qa-2026-10-20.md) now
passes both missing continuations: five completed real Loom runs, two original
claims and no duplicate effects. Its nine-service stand is archived/removed,
exit0/no OOM. The separate
[massage owner/specialist lifecycle](shuttle-massage-owner-lifecycle-qa-2026-10-20.md)
passes on candidate78: native booking, specialist agent reads,
exact owner agent cancellation after free-text consent, released-slot/native
history and three completed Loom runs. Original receipts and exact notices are
verified. The current and preceding failed nine-service stands are archived and
removed, exit0/no OOM; failed observer evidence is retained. Candidate78 also
distinguishes advance/final reminder text in en/ru. Its separate
[primary installation/rollback](shuttle-massage-primary-qa-2026-10-20.md) now passes,
with schema64, original data/queues, eleven services and134 Loom runs preserved.
The [original-consent outage gate](shuttle-massage-cancel-outage-qa-2026-10-20.md)
now also passes on78: one owner reply is durably admitted while local IAM and
Loom are stopped, then survives a clean bot restart while both remain unavailable.
The same run/approval resumes and cancels once. Three Loom runs finish; all nine
temporary services are archived/removed, exit0/no OOM. This is a short issued-token
outage, not token-expiry or all-fault acceptance. The separate
[agent payment closing gate](shuttle-pass-agent-closing-runtime-qa-2026-10-20.md)
now passes on78 from an explicitly assigned pass: an original early PDF/caption
after Loom downtime and clean bot restart, actual45-second sales cap, timely
30.00-BYN confirmation after closing and an explicitly reasoned late70.00-BYN
top-up. Four Loom runs and six notices finish; both temporary stands are
archived/removed, exit0/no OOM, with the first setup failure retained. Technical
payment-approval JSON is a recorded presentation follow-up. Meals checkout,
wider timing/fault coverage and other broader gates remain open.

Current stages and next order, retaining the broader requirements below:

1. Registration/pass whole native, agent and both mixed happy paths, primary
   installation/rollback and the bounded native initial-deadline/late-decision
   gate pass separately. Keep their version and acceptance boundaries.
2. Complete orders/meals and massage process parity and real multi-user form/
   agent acceptance, including administrative and specialist operations.
3. Complete Zitadel/zitadeltg linking and real Mini App/HTML acceptance. Joint
   browser CA trust/provider work remains deferred with Daniel; continue work
   that does not need it.
4. Close remaining payment timing/agent-failure variants, process, IAM/Loom
   recovery, observability, Linux/race and deployment gates before any separately
   authorized production cutover.

The earlier stage reports below are historical. Their run counts, installation
status and next-step sentences describe those stages, not the current runtime.

The [primary pair image and actual Telegram entry gate](shuttle-native-pair-primary-qa-2026-10-19.md)
now pass: candidate/old/candidate rollback, three subsecond bot stops,14 exact
assets, one delivered `/pairs` command and one revoked real Mini App session.
No business write or new Loom run occurred; primary remains schema59/mvp68 with
133 runs. A navigation-history omission was found and fixed in source with a
failing-then-passing behavior case, four PostgreSQL roots, lint0 and clean review.
Its follow-up image is ready but uninstalled. Next is that follow-up installation
and actual reactive-agent navigation-fact acceptance. Temporary QA containers
and volumes are removed; the owned primary has11 services.

The [isolated pair form/agent runtime gate](shuttle-native-pair-runtime-qa-2026-10-19.md)
now passes four inviter/target interface combinations and two same-owner
invite/revoke handoffs. Real IAM/Loom and actual browser forms yield12 operations,
four waitlisted pairs, no passes and six completed synthetic-provider agent runs.
A separate four-run restart check stops in0.610 seconds and preserves business
state. The nine-service stand is archived/removed; primary stays unchanged at133
runs. Next is controlled primary installation and real Telegram form acceptance.

The [native pair Mini App and `/pairs` entry](shuttle-native-pair-ui.md) now pass
240 UI behavior tests, nine PostgreSQL roots, affected-package tests, lint0 and a
Linux build. Explicit role consent and original-operation recovery share the
existing agent/native API. Independent reviews fixed two recovery defects; the
final fresh review is clean. This is an uninstalled component gate, not real
browser/IAM/Loom acceptance. Next is the isolated owned four-path runtime gate.
The temporary PostgreSQL stopped cleanly and was removed with its volume; the
primary remains unchanged at133 Loom runs.

The [native pair invitation API foundation](shuttle-native-pair-api.md) now passes
31 PostgreSQL root scenarios, all internal Go packages, lint0 and a Linux build.
Native/agent receipts share opaque operation keys and atomic session validation;
subsequent agent prompts receive bounded owner operation history. Independent
reviews found and fixed a lock upgrade and UUID-only recovery defect. This remains
an uninstalled component stage: pair UI/menu and full owned-runtime acceptance
come next. The temporary PostgreSQL and volume are archived/removed; its forced
cleanup stop is retained. Primary stays unchanged with133 Loom runs.

The [real WebView → reactive Loom profile follow-up](shuttle-profile-real-webview-qa-2026-10-19.md#subsequent-reactive-agent-follow-up)
now proves both manual profile receipts reach the next actual user-triggered
agent run as untrusted history. One owner-scoped API read returns the restored
name and one Russian reply is delivered; no business mutation occurs. Loom is
now at133 runs. The provider is controlled and synthetic, so unattended model
behavior and mutating handoffs remain unaccepted.

The [actual Telegram profile-name form gate](shuttle-profile-real-webview-qa-2026-10-19.md)
now passes native save, new-session persisted read and restoration of the original
name. Exactly two profile/action receipts and two revoked sessions are verified;
other data and132 Loom runs remain unchanged. This closes that real form-write
case without an agent; it does not close full registration/pass workflows or
native/agent continuation from these writes.

The [corrected Mini App landing gate](shuttle-miniapp-anchor-runtime-qa-2026-10-19.md)
now passes installed candidate/old/candidate rollback and actual Telegram initial
landing for Avatar, Saved selection, Meals, Massage and Payment reviews. All five
sessions were logged out and verified revoked. One extra native command delivered
without an agent; protected business/user/schema data and132 Loom runs remain
unchanged. Runtime is schema59 / `mvp-68`, eleven services. Full form writes,
avatar delivery and joint provider/CA login remain separate acceptance gates. The
subsequent avatar upload UI attempt was blocked by the browser file chooser;
no upload occurred, the session was revoked and its failed workflow result is
retained. This does not invalidate the accepted five initial landing targets.


The [real Telegram native/Mini App read-only gate](shuttle-native-miniapp-readonly-qa-2026-10-19.md)
now passes five structured commands without an agent run, actual Mini App login,
existing order/payment/proof retrieval and massage history with DST offsets.
Logout is independently verified as revoked. Business/user rows are preserved;
24 expired sessions were proved against the archive and legitimately pruned.
File export/download and full form writes are not accepted by this checkpoint.
The existing avatar screen is reachable; direct native start/avatar entries remain
next. No temporary container was created, and the eleven-service runtime remains
schema59 / `mvp-68` with132 Loom runs. Joint provider/CA activation stays separate.

The [prelink accepted-write backend gate](shuttle-prelink-remote-write-qa-2026-10-19.md)
now passes on an isolated four-service schema59 stand: lost accepted create/link
replies reconcile immediately or after two normal same-container bot restarts,
retaining each original fresh assertion, UUID and receipt. Three canonical users/
links/receipts, immutable replay and no extra accepted IAM write pass. Both DBs
and four logs are archived; temporary containers, volume and network are removed.
The primary remains unchanged at schema59/mvp68 with prelink disabled. Actual
provider activation, Telegram proof and the joint browser/CA check remain separate.

The [owned primary installation](shuttle-primary-upgrade-qa-2026-10-19.md) now
passes schema49→59 and runs `zinusya / mvp-68`. Normal bot shutdown took0.625s;
original data/receipts and peer services were preserved. A retained Compose parser
failure was recovered without repeating migration. Readiness, a fresh polling
cycle, eleven running services and the new installed runtime baseline pass.
The migration container/environment are removed; pre/post-upgrade archives and
logs are retained. This updates local installation status; historical uninstalled
candidate checkpoints below retain their original acceptance boundaries.

The [owned primary migration rehearsal](shuttle-primary-upgrade-rehearsal-qa-2026-10-19.md)
now passes49→59 on an isolated restore of the actual primary database. Original
columns across103 tables and the history-preview view, receipts and sequence
state are preserved; restore to49 and old-image migration startup also pass.
All five temporary containers and their volume were removed after archiving.
At that rehearsal checkpoint the primary remained schema49/mvp56. The subsequent
installation and notification readiness checks are recorded above.

The [shared order identity expansion](shuttle-order-kind.md) is implemented at
source schema54 / `mvp-60`: canonical service/meal draft versions are separate,
owner/event/kind links are enforced, legacy service receipts stay unchanged and
current service APIs cannot disclose or mutate unsupported meal identities.
Evidence stays service-only until its meal policy/authorization is implemented.
Three new PG roots and the corrected legacy manifest test pass;122 unchanged
neighboring roots passed the broad probe. All65 Windows/Linux unit roots, lint
and SQLC gates pass. No new image is installed; the primary stays schema49 / `mvp-56`.

The [frozen meal terms](shuttle-meal-terms.md) builder now preserves exact complete
composition, sold names, combo arithmetic and per-service windows independently
of catalog changes. Four new behavioral roots and all32 db/orders unit roots pass
on Windows/Linux, with lint0 and clean independent review. It adds no persistence,
HTTP or financial action; the existing runtime image predates this component.
The shared `service|meal` identity transition is recorded above; payable meal
submission still waits for the recorded financial/capacity policy.

The [shipped meal form](shuttle-meal-menu.md) now opens from native `/menu` and
shares drafts/receipts with the agent. Its ru/en controls cover incomplete lunch,
combos, optional dinner, clear, historical menus and original-operation recovery.
All190 UI tests, all36 selected PostgreSQL roots, all17 API and16 task roots and
lint pass; independent review is clean. A real mobile DOM fixture also passes
focus, hostile labels, language and response-loss/reload acceptance. Source stays
schema53 / `mvp-60`. The Linux/amd64 candidate is built and uninstalled after a byte-preserving
platform filename correction and61 passing Linux unit roots. [Actual IAM/Loom runtime acceptance](shuttle-meal-runtime-qa-2026-10-19.md)
now passes native-only with Loom stopped, agent-only and both handoffs. The
agent-to-native path crosses a normal0.640s bot SIGTERM restart. Four drafts/six original receipts reconcile;
all tasks/runs/deliveries finish. The nine-service QA project is archived and removed. Financial
submission remains separate; primary stays schema49 / `mvp-56`.

The [native order-review runtime gate](shuttle-order-native-runtime-qa-2026-10-19.md)
now passes confirmation with Loom stopped and original-run agent/form/agent
continuation on the uninstalled schema52 / `mvp-59` candidate. Each claim has one
canonical ambassador decision and its original held resources become occupied.
The nine-service QA project is archived and removed; primary remains schema49 /
`mvp-56`. Actual Telegram WebView and unattended model acceptance remain separate.

The [specialist upcoming-session workflow](shuttle-massage-specialist-next.md)
now passes both component gates (35 integration roots,26 UI tests, lint0) and
[real IAM/Loom runtime QA](shuttle-massage-specialist-next-runtime-qa-2026-10-19.md)
on a separate mock-Telegram stand. The Linux schema52 / `mvp-59` candidate is built
and uninstalled. Actual Loom/native preference handoffs, one soon delivery after
known429 + normal bot restart, and cancellation before retry pass. Bot stop took
0.765s with exit0. Its nine-container project and builder are removed after archiving;
a mock snapshot observer failure was reconciled without repeating the operation.
The primary stays schema49 / `mvp-56`. The subsequent
[mock entity echo fix](shuttle-mock-telegram-entities-qa-2026-10-19.md) passes all24 Linux
fixture/provider tests. Remaining infrastructure work and the jointly deferred
real WebView/CA/zitadeltg session stay open.

The [specialist instant workflow](shuttle-massage-instant.md) now has domain/store,
HTTP, shipped-form and [actual Loom/graceful behavior proof](shuttle-massage-instant-runtime-qa-2026-10-19.md).
The original task/run/proposal survives a normal bot-only stop/start; explicit
free-text consent produces one original booking. Human terms no longer repeat
internal JSON. Two QA observer failures and read-back reconciliation are retained.
Both actual Loom/native handoffs now pass on a separate disposable stand,
including original-run receipt replay and fresh native history. Runtime
revoke/regrant also rejects old consent without booking effects; one failed QA
observer and independent read-back reconciliation are retained. Original-receipt recovery after private binding loss and current shared-table
competition also pass actual Loom QA, with exact model-visible receipt checks.
Owner-reminder presentation and real WebView acceptance remain open.
Specialist-before-session runtime proof is recorded above. The primary stays schema49 / `mvp-56`; the built schema50 /
`mvp-57` candidate is uninstalled. All five temporary runtime projects and both builders are
removed; primary domain data/runtime are preserved.

The [two-customer massage browser gate](shuttle-massage-two-customer-browser-qa-2026-10-18.md)
now passes actual overlapping native confirmations for the same slot: one booking,
one409, recoverable loser form, explicit cancellation and fresh same-slot consent.
Confirmed terms and event-local messages match persistence; three distinct specialist
notices are checked. No agent tasks/runs or payments were created. The disposable
mock project is removed; the restored primary domain state is preserved.

The [owned-stand resume checkpoint](shuttle-reboot-resume-qa-2026-10-18.md)
restores the same eleven primary containers after the user-requested pause, with
business data preserved and the Loom run count remaining132. Normal retention removed expired terminal
River scheduling rows; the source-reviewed new observer distinguishes this from
loss of durable tasks. The bot remains schema49 / `mvp-56`. Two helper stop137
outcomes and the readiness/strict-observer failures are retained. This is separate
from exceptional SIGKILL recovery and production acceptance.

The [two-customer native capacity gate](shuttle-orders-capacity-browser-qa-2026-10-18.md)
now passes actual overlapping full-price submissions for the last two units:
one pending hold2, one exhausted without a hold, correct customer/ambassador notices.
Explicit native rejection releases the hold with zero credit and leaves the exhausted
claim unchanged. The actual forms create no additional agent tasks/runs. Real
IAM/Loom/backend plus mock Telegram; the disposable project is removed and primary
preserved. This does not settle D23 partial/cash/mixed-resource policies.

Restart priority: normal SIGTERM plus graceful shutdown (target1s, hard limit5s).
SIGKILL is exceptional. Preserve durable state and make uncertain outcomes
reconcilable through operation IDs, receipts, logs and database state; rare human
or agent intervention is acceptable. Do not let perfect automatic recovery from
exceptional failures displace core functionality.

The [order browser handoff](shuttle-order-browser-handoff-qa-2026-10-18.md)
now passes the full original blocked commit→native confirmation→same-run/session
continuation after explicit free text. Distinct customer/ambassador identities,
original proof/proposal, one25.68 BYN review, held2→occupied2 and correct notices
are independently checked. Two original Loom write IDs complete without a second
financial decision. One-shot browser reruns are refused before launch. This uses
mock Telegram plus the shipped form and real IAM/Loom; real WebView acceptance and
D23 policies remain separate. The disposable project is removed; primary preserved.

The [cross-person native browser gate](shuttle-cross-person-browser-qa-2026-10-18.md)
now passes separate payer/ambassador access with real IAM/Loom/backend and mock
Telegram: proof-byte verification, zero credit on preparation, one30.87 BYN
confirmation, deliberately lost successful reply and original-receipt recovery
without another confirmation POST. Native review created no agent task/run.
The reusable mock-only browser helper uses isolated contexts and no production
configuration. All temporary resources are removed; real WebView/zitadeltg and
the order reviewer agent-to-native confirmation remain open.

[Real Telegram native staff QA](shuttle-massage-staff-native-real-qa-2026-10-18.md)
now passes administrator grant/revoke/restore without an agent run, no-pass
specialist timetable/own-client/contact reads after removing temporary IAM admin
roles, one actual Loom read/reply, and same-container bot restart. Synthetic
fixture bookings and manual provider responses bound this acceptance. Temporary
admin grants were removed. The subsequent [real approval handoff gate](shuttle-massage-staff-mixed-real-qa-2026-10-18.md)
also passes an agent-only revocation and both form/agent directions: three canonical
effects, original proposal/expiry preserved, six tasks completed and nine expected
messages delivered. Its final fixture binding is revoked at version6, temporary
IAM grants are again removed and no task is pending. Complete live-controller
rollback, unattended model behavior and production acceptance remain open.

The [fresh order review handoff](shuttle-order-review-late-real-qa-2026-10-18.md)
now passes native-to-agent continuation and late ambassador review of timely proof:
new revision2, fresh consent, one 25.68 BYN canonical review and the same two soup
units held to occupied. The earlier failed/expired revision1 remains unchanged.
[Owner orders and pending evidence](shuttle-orders-owner-real-qa-2026-10-18.md)
also pass through the agent alone and agent/form/agent continuation: two orders,
two distinct PDF claims, four held soup units and zero confirmed payments at that
owner milestone. The ordinary `/orders` command did not start another agent run.
The subsequent [reviewer dialogue and cancellation QA](shuttle-order-reviewer-dialogue-qa-2026-10-18.md) now passes one
pure-agent payment confirmation with fresh free-text consent and cancellation of
another prepared review without changing its pending claim or holds. The native
handoff loaded the original workspace but could not complete: the browser tool
rejected Load proof input coordinates. Native confirmation was not accepted in
that real Telegram attempt; the later isolated browser gate linked above passes.
Current fixtures: one confirmed payment/two occupied units and one pending
claim/two held units. The [catalog image update](shuttle-catalog-contract-install-qa-2026-10-18.md)
used `mvp-55` with schema48; current runtime is schema49 / `mvp-56`.
Fault/outage and unattended model gates remain open.

The [real IAM positive-cache outage QA](shuttle-iam-positive-outage-qa-2026-10-18.md) now passes warm-cache 200,
cold-cache 503 and TTL-expired-cache 503 with own Zitadel stopped, then a fresh
owner read after restoration. Original settings/version and durable data were
restored. This is inbound API introspection caching. The subsequent
[real credential lifecycle gate](shuttle-iam-credential-expiry-qa-2026-10-18.md)
now passes the public Go identity client's real service/delegated issuance, warm
cache reuse while IAM is stopped, valid early-renewal fallback, actual expiry
refusal and fresh issuance after the same IAM restarts. It used a separate helper
and isolated90-second settings, restored all four settings, logged out its native
session and deleted the temporary session credential. The helper and nine-service
stand were removed. Primary runtime/business state
was preserved. The subsequent [actual-bot cold-start gate](shuttle-bot-cold-iam-restart-qa-2026-10-18.md)
now passes empty-cache startup and intake with IAM stopped, actual pending task
recovery across another same-container bot restart, automatic identity/delegated
session/run after IAM restoration, one manually inferred final reply and original
update replay deduplication. No task/identity/delivery state was seeded through SQL.
The nine-service stand was removed and primary runtime/business state preserved.
The subsequent [existing-run overlapping outage gate](shuttle-existing-run-outage-qa-2026-10-18.md)
now passes original task/run/session preservation while both IAM/Loom are stopped,
cold bot startup, and typed shutdown autoresume after IAM→Loom→provider restoration.
A deliberate bot barrier allowed provider namespace readiness before resume. The
first inference remained sponsored unknown; one new manually supplied inference
completed the same run with one final send. All nine temporary services were
removed. Primary River retention drift was proved exactly by archive-backed SELECT
reconstruction; business data/runtime remained unchanged. The subsequent
[reverse-order continuous-bot gate](shuttle-existing-run-reverse-outage-qa-2026-10-18.md)
now passes Loom/provider readiness while IAM is still stopped, the same bot process
waiting through dependency return, and original-run autoresume/final delivery when
IAM returns last. There is no second bot stop/barrier; all IDs and the continuous
bot epoch were preserved. All nine temporary services were removed; primary112
fingerprints matched except River leader bookkeeping, and all46 River job rows
were unchanged. The subsequent [IAM-first continuous-bot gate](shuttle-iam-first-continuous-qa-2026-10-18.md)
now passes the other restoration order without a second bot barrier or provider
restart: original task/run/session, unknown inference accounting and one final
send were preserved. Its temporary runtime was removed and primary state matched.
The [actual running-task renewal gate](shuttle-running-session-renewal-qa-2026-10-18.md)
now passes proactive same-session renewal before original expiry and continued
original task/run/model execution beyond it, with one final send and unchanged
service epochs. Original OIDC settings were restored; both the failed QA attempt
and corrected acceptance remain documented. The subsequent
[session-expiry/IAM outage gate](shuttle-session-fallback-outage-qa-2026-10-18.md)
found an unnecessary same-expiry reauthorization that produced `auth_unknown`.
A narrow fix passed its red/green regression, 13 focused tests, lint, two product
reviews and fresh real IAM/Loom candidate acceptance: actual expiry while IAM
is stopped, automatic same-session recovery and one final send. Both temporary
projects were archived and removed; primary state was preserved. The [owned primary installation](shuttle-session-fallback-primary-qa-2026-10-18.md)
subsequently accepted new→old→new binary starts against the same schema49 DB,
all112 fingerprints with precise River bookkeeping allowances, all46 job rows
and the original poller restart. The primary now runs that candidate.
The [abrupt Loom exit gate](shuttle-loom-abrupt-exit-qa-2026-10-18.md)
subsequently observed an orphan running River job and expired run lease 117
seconds after SIGKILL/restart. Recovery is not accepted; L04 records the evidence
and upstream contract proposal. Its temporary runtime was archived/removed and
primary state preserved. The subsequent
[local revocation/cache gate](shuttle-cache-revocation-qa-2026-10-18.md) passes
with real IAM JWTs: warm IAM-down200, exact local revocation, target503+cache miss
within2.62s of insertion while another administrator stays cached200, IAM-return
local403, original receipt/audit deduplication and stale Loom result suppression.
Four current behavioral tests also pass for both caches, lostCOMMIT/restart and
binding replay; real issued-cache eviction is not claimed. The fixture was
archived/removed and primary state preserved. External IAM revocation/cache
policy, uncertain reauthorization and the Loom crash-recovery gap remain open.
The [mock-provider lifecycle slice](shuttle-mock-provider-lifecycle-qa-2026-10-18.md)
now removes the provider namespace barrier: private TLS, independent provider
process, 31 passing Linux tests, two real IAM/Loom task deliveries across a Loom
restart and clean provider SIGTERM passed. All temporary resources were removed.
This removed the namespace barrier used by the accepted IAM-first gate above.


Specialist notification storage and reservation now pass separate component
checks: [global preference and transition intents](shuttle-massage-specialist-notice-store-qa-2026-10-18.md)
and [generation-fenced reservation/outcome handling](shuttle-massage-specialist-notice-delivery-qa-2026-10-18.md).
The reservation gate passes fourteen race scenarios. The subsequent
[Telegram worker and metrics mock gate](shuttle-massage-specialist-notice-worker-qa-2026-10-18.md)
passes twenty-two race scenarios, including service-start unknown recovery.
[Administrative diagnostics and audited retry/dismiss](shuttle-massage-specialist-notice-admin-qa-2026-10-18.md)
now pass thirty selected race scenarios. Shared [agent/native preference APIs](shuttle-massage-specialist-preferences-api-qa-2026-10-18.md)
pass thirty-five selected race scenarios. [Native notification controls and exact-request recovery](shuttle-massage-specialist-preferences-ui-qa-2026-10-18.md)
pass 24 specialist UI cases (176 tests across the full mini-app suite). [Reactive
preference history](shuttle-massage-specialist-preferences-context-qa-2026-10-18.md)
passes a 42-root race gate including service restart and steering. The subsequent
[mixed runtime preference gate](shuttle-massage-specialist-preferences-runtime-qa-2026-10-18.md)
now passes native HTTP → actual Telegram/Loom → native HTTP continuation: four
immutable receipts, one completed agent run and final reply, original effective
value restored, fixture binding revoked at version8 and temporary IAM grants
removed. Signed native bootstrap is synthetic; provider inference is manual.
The subsequent [real delivery gate](shuttle-massage-specialist-notices-real-qa-2026-10-18.md)
now passes new-booking and cancellation sends: two actual worker deliveries to the
current account, visible in Telegram Web, with event-local DST offset, fixed price
and contact only on the booked notice. The new fixture was cancelled, its binding
revoked and temporary grants removed; old bookings and protected business data
were preserved. This used the delegated API directly, with no Loom run.
The subsequent [isolated process gate](shuttle-massage-specialist-restart-qa-2026-10-18.md)
now passes actual429/retryable recovery after graceful stop (0.734 seconds) and
actual send_inflight → SIGKILL → startup delivery_unknown, with one accepted
mock effect and no automatic resend. It used real IAM/native booking operations
with explicit synthetic SQL event/pass/binding fixtures. Its nine temporary
containers, volumes and network were removed; primary runtime/business state
was preserved. The simulator now has a bounded, durable hold/release fault and
20 passing HTTP/operator tests.
The [running-worker generation gate](shuttle-massage-specialist-generation-qa-2026-10-18.md)
now passes real native mute/re-enable and revoke/regrant before deferred429 retries.
Both captured old intents were blocked while current settings/access were enabled;
a fresh-generation booking delivered once. No process restarted. All three fixtures
were cancelled, temporary IAM grants/sessions removed and the nine-service stand
retired. Primary runtime/business state was preserved.
The [never-attempted notification gate](shuttle-massage-specialist-pending-restart-qa-2026-10-18.md)
now passes native booking into a publicly SDK-paused queue, graceful bot stop in
0.766 seconds, same-container restart and one first delivery from the original
River job after resume. No delivery/job state was seeded by SQL. The nine-service
stand and two bounded SDK jobs were removed; primary runtime/business state was
preserved. This is a persisted queue-pause gate, not unpaused scheduling acceptance.
Browser Mini App controls and broader task/outbox process restart remain open.
The [mvp-56 candidate image](shuttle-specialist-notice-candidate-image-qa-2026-10-18.md)
is now [installed on our owned stand](shuttle-specialist-notice-install-qa-2026-10-18.md):
schema49 / `mvp-56`, fresh quiescent48 archive and actual verified restore,
quiet bot exit0 in0.688 seconds, image-only recreation and the same poller resumed.
Old business data, settings, eight Compose companions and counted provider were
preserved. A psql output mismatch stopped the first verification; its exact
quiet-output fix passed23 tests and two fresh reviews before same-migrator
continuation. The initial failure remains retained. Both terminal jobs and
temporary credentials were removed; the fresh fenced48 recovery DB/archive remain.

The [real-image copy rehearsal](shuttle-specialist-notice-schema49-rehearsal-qa-2026-10-18.md)
and [populated49 recovery component gate](shuttle-specialist-notice-populated-recovery-qa-2026-10-18.md)
remain separate preservation evidence, not full live rollback. Component clone
containers/volume/credentials were removed. Current owned runtime is schema49 /
`mvp-56`. The earlier [staff installation](shuttle-massage-staff-install-qa-2026-10-18.md)
and [catalog update](shuttle-catalog-contract-install-qa-2026-10-18.md) retain their
original schema48 acceptance records. Full live-controller rollback, broader task/outbox
restart and Mini App browser acceptance remain open. Running-worker generation
fences, specialist safe-retry/inflight process recovery, scoped preference
Telegram/Loom continuation and real booked/cancelled delivery now pass as above.

The [isolated domain gate](shuttle-isolated-domain-qa-2026-10-18.md) already covers
native-only registration, agent/mixed continuation, pass reads, specialist access
and booking/cancellation with real IAM/Loom and synthetic Telegram/model responses.
The [mock stands](shuttle-mock-telegram-qa-2026-10-18.md) are stopped with evidence
retained. [Readable specialist approval](shuttle-specialist-approval-readable-qa-2026-10-18.md)
and [actual PostgreSQL recovery components](shuttle-database-recovery-qa-2026-10-18.md)
pass their stated gates. Native specialist/admin form source acceptance is recorded
in [read-form QA](shuttle-massage-staff-ui-qa-2026-10-18.md) and
[administrator-form QA](shuttle-massage-admin-ui-qa-2026-10-18.md).

Next: complete agent-to-native reviewer confirmation when the browser exposes
a usable native control; use a new revision and fresh consent, preserving the
cancelled revision and earlier failures. Continue process/outage hardening
independently of that UI barrier. Specialist notifications and later parity workflows,
complete live-controller rollback, unattended-model behavior and production
acceptance remain open. Older dated component records retain their original
version and acceptance boundaries.

# zns-chatbot / Zinusya roadmap (Shuttle rewrite)

Owner massage domain/store, live availability and agent/native HTTP APIs have
[backend component acceptance](shuttle-massage-native-http-qa-2026-10-17.md).
[Native owner form source](shuttle-massage-native-ui-qa-2026-10-17.md) now passes
119 Mini App JS tests, including 16 massage cases, and its final independent review.
That UI milestone used schema46 / `mvp-50`; that rollout used schema47 / `mvp-51`; current runtime is schema49 / `mvp-56`.
The [native `/massage` entry source](shuttle-massage-command-qa-2026-10-17.md) also
passes seven Linux race roots, 121 Mini App JS tests and its independent review.
[Pending discovery backend](shuttle-massage-pending-qa-2026-10-17.md) now passes
19-root race QA and final independent review. [Reactive massage context](shuttle-massage-reactive-context-qa-2026-10-17.md)
now also passes10-root race QA and final review. [Native pending selection](shuttle-massage-pending-form-qa-2026-10-17.md)
now passes128 Mini App tests, including25 massage cases, and both source reviews.
The [owned rollout and real Telegram QA](shuttle-massage-runtime-qa-2026-10-17.md)
accept activation and native booking/cancellation. Pure-agent booking, a fresh
form-to-agent booking and agent-to-form cancellation completed with manual
inference. An earlier form-to-agent attempt applied once but its final dialogue
failed with `active_time_expired`; that failure remains preserved. A pending
native cancellation survived a bot restart and applied under its original key.
The [booking lost-response/clean-restart gate](shuttle-massage-response-loss-real-qa-2026-10-17.md)
passes. Other fault and service-outage paths, specialist lifecycle, specialist notifications
and unattended model behavior remain open.


Current acceptance: [four real Telegram happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
pass with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

Current owned runtime: schema49 / manifest `mvp-56`. Specialist Store/HTTP/forms
are installed; native staff reads and administrative agent/mixed happy paths
pass their linked scoped gates. Complete massage parity remains open. The earlier
[order-notice activation](shuttle-order-notice-delivery.md) installed the owner and
reviewer HTTP/forms, shared evidence/review state, reactive context and durable
notices. [Native-only order proof/review](shuttle-order-proof-native-qa-2026-10-17.md)
passed with actual Telegram and real IAM; the uploaded file acknowledgement used
a manual provider. [Native-to-agent continuation](shuttle-order-mixed-context-qa-2026-10-17.md)
failed on a Loom context pause before the consent resolver. That failure and the
expired consent remain preserved; no payment review ran from it. A subsequent
[fresh native-to-agent review](shuttle-order-review-late-real-qa-2026-10-18.md) passes
with revision2 and fresh consent, including review after the evidence deadline.
Owner intake/order handoffs now pass the linked scoped gate. Pure-agent and
agent-to-native reviewer decisions remain open.

A separate HTTPS counted fixture is now active on the owned Loom with the same
frozen image and verified durable-state preservation. Its counting is synthetic;
activation does not prove a model request originating from Loom, unattended model
behavior or real context limits. Browser access was explicitly reauthorized on 2026-10-17; the existing embedded
app opened and signed in again. The later massage rollout accepts the native owner form lifecycle within its documented scope. The [subprocess crash/replay gate](shuttle-order-notice-process-qa-2026-10-17.md)
now verifies uncertain order notices across a killed worker and actual queue-job
replay with real PostgreSQL and mock peers. Full executable shutdown, owned-service
outage/notification recovery and real Telegram mixed acceptance stay separate.
Massage booking domain/store, agent API, live availability and native HTTP routes have component acceptance; native form source, runtime activation and the real native owner lifecycle pass their scoped gates. Full agent/mixed acceptance remains open.

Earlier schema 40 [submitted-order acceptance](shuttle-orders-submission-real-qa-2026-10-17.md),
[response-loss recovery](shuttle-orders-response-loss-real-qa-2026-10-17.md) and
[shared-proposal handoff](shuttle-orders-shared-proposal-real-qa-2026-10-17.md)
retain their original limits. Later checkpoint sections below are historical.

The earlier schema 39 [local launch rollout](shuttle-miniapp-local-launch-qa-2026-10-16.md)
installed payment-review store/API/HTTP components and the explicit HTTPS-to-localhost
launch configuration. Real Telegram-issued initData, form save/reload and logout
passed in a top-level tab. After browser permission approval, Telegram Web embedded opening, authenticated
reads, price calculation, draft writes and logout/re-entry pass. Payment-review
happy paths and mixed handoffs now pass with a manual provider. Embedded critical
paths, UI follow-ups and unattended model behavior remain open.

Checkpoint sections below retain their original test counts and acceptance limits;
use the current runtime/source status above for today's stage.

Interaction update, 2026-10-16: Loom's conversational contract is merged at
fc6c574. The [adapter port](shuttle-agent-reply-keyboards.md) removes text tags and
client-side intent decisions. Breaking dialogue changes are allowed; native
commands remain available. Deterministic component QA and real-service acceptance
are distinct. The [owned runtime rollout](shuttle-conversational-runtime-qa-2026-10-16.md)
installed the adapter on schema36/Loom4; the later protected-form rollout uses
schema37/Loom4. Actual Telegram transport passed with a
manual provider; real-model/business acceptance and documented recovery/evidence
limitations remain open.

The protected-form slice now has component-accepted
[Mini App forms and backend](shuttle-miniapp-registration-details.md): shared private
mutation, atomic session checks, redacted context, historical receipts and
memory-only private drafts with explicit recovery. Node, Linux/race and isolated
browser QA are green. The release image, isolated36->37 snapshot and bot-only
owned activation pass, preserving all74 old tables and the eight companions.
That rollout reached schema37/key-enabled with real-IAM owner reads accepted;
[mixed real IAM/Loom form acceptance](shuttle-miniapp-registration-details-qa-2026-10-16.md)
now passes with actual Telegram ingress and a manual provider. Trusted WebView,
unattended model interpretation and remaining registration/pass workflows remain open.

The next [native event details entry](shuttle-registration-details-native-entry.md)
is installed on the owned stand: missing-data role clicks persist an atomic
event expectation, offer the existing Mini App and require an explicit role choice
after completion. Source review and scoped lint are clean;36-root Linux/race
component acceptance passes. [Actual Telegram/form API acceptance](shuttle-registration-details-native-qa-2026-10-16.md)
passes with Loom stopped. The later [solo prerequisite four-path matrix](shuttle-registration-details-paths-qa-2026-10-16.md)
also passes with a manual provider: menu-to-agent, agent-to-form/menu and agent-only.
Trusted WebView, unattended model decisions and remaining pair/admin/pass/payment
interfaces stay open.

Each slice ends with behavioral happy/critical-path checks, fresh independent
review loops, synchronized documentation and acceptance on the owned stand.
Green source checks do not substitute for real-service or production acceptance.
Open user decisions are kept in ignored .cache/shuttle-work/decisions.md.

Current execution priority: identity/zitadeltg and browser entry, Mini App plus the
Python HTML processes, full orders/meals and massage workflows, with process
hardening and Loom integration throughout. See the
[infrastructure priorities and form inventory](shuttle-infrastructure-priorities.md).
Additional isolated media/metrics checks must not displace these outcomes.
The next selected [orders draft/form slice](shuttle-orders-selection-plan.md) follows
architecture and senior Go consultation. Its [store and agent API](shuttle-orders-selection.md)
plus Mini App session API, ru/en form, manual context and native /orders entry are
implemented and installed on the owned stand at schema38. [Four-path runtime QA](shuttle-orders-selection-qa-2026-10-16.md)
passes with actual Telegram/real IAM/Loom and direct form APIs, using a manual
provider and locally signed proofs. Trusted WebView and unattended model checks
remain open. Owner-only orders and evidence-time reservations are agreed, and massage
should retain Python rules; remaining financial choices are recorded separately.
Submitted-unpaid orders have the later accepted paths linked above. Order evidence reads, shared reviews, native controls and durable notices are now
installed at schema45. Native-only acceptance passed; full agent/mixed financial
lifecycle acceptance remains open as described above. Massage owner forms and
native entry are installed with scoped real booking/cancellation acceptance. Full
agent/mixed acceptance, specialist lifecycle and notifications remain open.
The subsequent [native payment review](shuttle-native-payment-review-plan.md):
ambassador/event-admin proof inspection and exact confirm/reject through the Mini App,
with the same pending decision available to the agent. The existing review transaction
core has been extracted and passed [focused before/after checks](shuttle-payment-review-tx-qa-2026-10-16.md);
the [typed workspace store](shuttle-payment-review-workspaces.md) now passes
[47-root component QA](shuttle-payment-review-workspaces-qa-2026-10-16.md).
The [agent workspace API](shuttle-payment-review-workspace-api.md) now passes scoped
HTTP/race checks and independent reviews. The [native session transaction store](shuttle-miniapp-payment-review-store.md)
also passes component checks. The [HTTP/proof adapter](shuttle-miniapp-payment-review-http.md)
now passes90-root payment/Mini App race checks. These components are installed at
schema39. The [reviewer form](shuttle-miniapp-payment-review-form.md) now passes
source component and isolated browser checks. [Native entry and reactive action
context](shuttle-payment-review-entry.md) also pass component checks. Their full
candidate is now installed after isolated schema39 compatibility and image-only
rollout checks. [Four real-service happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
now pass with a manual provider; critical fault coverage and unattended behavior
remain open.

The earlier [prelink image/schema33 rollout](shuttle-prelink-main-qa-2026-10-16.md)
preserved existing data and kept prelink disabled. The later schema34 action-context rollout and current schema40 image also keep
it disabled. Main provider/backend activation and real
interactive login remain open; scoped fixture acceptance does not close them.

Mandatory gate for every key workflow: full buttons/commands/forms operation
without an agent, full agent operation, and continuation in both directions using
the same persisted state. Cover registration/passes and the other key commands,
including the role-authorized ambassador/admin paths. Structured name/profile
entry must not depend on Loom. Registration/pass menu parity remains incomplete;
API/tool checks are not its acceptance. See the
[required paths and acceptance matrix](shuttle-infrastructure-priorities.md#required-across-all-stages-buttonsforms-agent-and-mixed-continuation).

The [structured navigation foundation](shuttle-structured-navigation.md) adds
Loom-independent menu/profile/status reads and typed reply keyboards, with scoped
Linux race/review acceptance and native Telegram command/pagination QA while the
owned Loom was stopped. Full write forms and structured registration/mixed
workflows remained open at that navigation checkpoint; later scoped slices follow.

The earlier combined [Mini App profile form](shuttle-profile-entry.md) and
[quoted Telegram name reply](history/shuttle-native-name-replies-2026-10-16.md) image
passed scoped acceptance preserving schema33 and existing data. Native /name, quoted answers,
bot restart with Loom stopped, stale-answer refusal, both command/agent mixed
directions and the full agent name path passed. The real Loom provider was driven
manually; this is not unattended model acceptance. The new [name-entry contract](shuttle-telegram-name-entry.md)
uses agent interpretation for ordinary replies, retaining explicit commands/forms.
At that earlier profile checkpoint, assets were served exactly but trusted WebView
entry/form writes were unaccepted. The later embedded acceptance linked at the top
closes entry and scoped draft writes; it does not by itself repeat profile-name
writes. The [transactional Mini App action journal](shuttle-miniapp-action-context.md)
was introduced on schema34 and remains in the current schema40 image.
Locally signed proof, real IAM/form APIs, native Telegram ingress and real Loom
passed [scoped runtime QA](shuttle-miniapp-action-qa-2026-10-16.md), including exact
replay after bot restart and no proactive run. Its provider was manually driven.
Browser entry was unaccepted at that action-journal checkpoint; later embedded
entry acceptance is linked above. Unattended model behavior and remaining
profile-specific embedded writes are not closed by the earlier gate. See the
[scoped native QA report](shuttle-profile-name-native-qa-2026-10-16.md).
Full registration/pass menu/form parity remains incomplete. The working tree now adds a
[structured solo registration entry](shuttle-structured-registration.md): readable
event/role choices, delivered-message binding and the shared waitlist mutation,
with scoped Linux/race and independent review acceptance. Its earlier image
fffc16debed6 passed a schema34 image-only rollout preserving all65 table hashes
and a real-IAM owner read; later candidates retain that registration slice. The [native four-path acceptance](shuttle-structured-registration-qa-2026-10-16.md)
passed with real IAM/Loom and a manually driven provider, including a bot restart
with Loom stopped, duplicate reuse and opposite-role refusal. Administrator allocation and pair/payment forms remain open. Configured legal
prerequisites now pass the later [scoped solo path matrix](shuttle-registration-details-paths-qa-2026-10-16.md)
with a manual provider; remaining role/workflow gates stay explicit.

The [historical tagged-keyboard slice](history/shuttle-tagged-keyboards-2026-10-16.md)
had scoped native acceptance on owned candidate ded0e98a2245/schema34. Fresh real Telegram,
IAM and Loom checks covered question choice/text, menu/restart, stale-token
isolation, cancellation and approval reject/approve. Resolved keyboards were
removed before the final model response while the same parent was still running.
Approval used a synthetic leaf that refused execution; no business mutation
occurred. All367 root scenarios across five packages passed under Linux/race,
with unchanged inputs and scoped lint/review acceptance. Temporary QA containers,
builder/cache and disposable volumes were removed. Original application/fixture/
test failures and same-operation recovery remain recorded separately. The provider
was manually driven; unattended model behavior, broader registration/pass/admin
parity and trusted Mini App requirements remain open.

A cross-module audit fixed the event-context tier-pricing contract in manifest
`mvp-33` and recovery of accepted messages whose target finishes while Input
acceptance remains uncertain. Independent reviews, all 261 integration tests and
the remaining Go packages passed under Linux/race. The corrected complete image
passed isolated real-IAM startup and SIGTERM/task-recovery checks with synthetic
Loom/Telegram peers. A separate real-IAM/Loom receipt-contract probe also passed
through same-run recovery after an initial cancellation 503; it verified retained
Input replay across fresh delegation, typed terminal rejection and one effect. See
[message recovery contract](shuttle-message-recovery.md) and
[QA evidence and limits](shuttle-message-recovery-qa-2026-10-15.md).

The frozen `mvp-32` artifact has separate full-Dockerfile local acceptance:
bounded complete build, exact license/CA/binary inspection, isolated fresh-schema
startup and same-task restart recovery with real IAM and synthetic Loom/Telegram.
The main executable matches the earlier accepted binary byte for byte. Temporary
builders and runtime containers were removed; the original stand was unchanged.
This is a local artifact gate, not production deployment or a published release.
See [full-image QA and limits](shuttle-full-image-qa-2026-10-15.md). The corrected artifact above is separate from that historical image.

An earlier combined local verification covered all 251 enumerated integration tests and
other explicit Go packages under race, using fresh bounded test containers. It
found a test-IAM clock-step dependency; a fixture-only fix passed with a separate
future-issued-token rejection check. Production JWT validation is unchanged.
The initial OOM and 401 failures remain recorded in the
[combined QA report](shuttle-combined-qa-2026-10-15.md).

Unknown-size attachment reads now preserve an explicit Telegram cloud-size
refusal as HTTP 413 instead of 503. Behavioral race/lint checks, independent source
reviews and the owned real-cloud/owner-API before/after probe passed, using the
same existing synthetic file and restoring its temporary unknown-size metadata.
No new Telegram message or Loom run was needed. See
[scope and evidence](shuttle-cloud-file-qa-2026-10-15.md).

File source recovery is now accepted separately without changing the runtime.
Focused behavioral race tests cover unavailable fetch/content, service restart
and recovery of the same UUID and exact bytes. Real IAM/Telegram/Loom checks
proved explicit retry after a controlled invalid-source refusal and reimport
after expiry of a newly created temporary artifact. The initial QA result-decoder
failure is retained; no Telegram message or service restart was needed. See
[scope and evidence](shuttle-file-recovery-qa-2026-10-15.md). Automatic model retry,
unattended classification and production retention remain open.

Linux subprocess acceptance now covers the unchanged five-second watchdog: real
SIGTERM with blocked deferred cleanup exits 1; disarm survives past the timer and
then exits normally on release. Command-package race checks and strict lint
passed. This does not establish a whole-bot worst-case shutdown SLA. See the
[watchdog scope and evidence](shuttle-watchdog-qa-2026-10-15.md).

Drain-phase duration and fixed participant outcomes are now logged. Behavioral
HTTP/deadline/error checks, strict lint, independent reviews and an owned healthy
stop/start passed: drain 2.441 ms, Docker stop 519.941 ms, exit 0/no OOM, preserved
business/runtime state and successful IAM owner read after restart. This is one
healthy observation, not worst-case hard-watchdog acceptance. See
[diagnostic semantics](shuttle-shutdown-diagnostics.md) and [QA limits](shuttle-shutdown-qa-2026-10-15.md).

Actual-bot blocked-read shutdown/recovery is now accepted as a separate bounded
scenario. In a fresh isolated database with real IAM, synthetic Loom and a dummy
Telegram sink, SIGTERM terminated the process within conservative 233.8304 ms
and 229.6554 ms bounds. The same unfinished task/run/session resumed after restart,
with no second create, no outbox/send and unchanged original stand. Independent
harness reviews and final source/hash checks passed; temporary containers were
removed. This does not exercise the watchdog or actual Loom/IAM outage. See
[scope and evidence](shuttle-shutdown-fault-qa-2026-10-15.md).

Contextual JSON log correlation is now implemented and locally accepted. Valid
sampled and unsampled contexts add trace/span IDs through the existing slog
handler; startup logs without a span remain unchanged. Focused Linux race/lint
checks, independent reviews and the owned executable's rejected-callback diagnostic
path passed without new task/message or business-state changes. See the
[contract](shuttle-log-correlation.md) and [native limits](shuttle-log-correlation-qa-2026-10-15.md).

Seven operational alert rules are now prepared and locally accepted over existing
private metrics: expected scrape loss, independent snapshot failures/staleness,
uncertain receipts and overdue queue work. Twelve behavior cases, an actual
aggregate snapshot check and fresh independent reviews passed; CI validation was
added locally. Cloud installation and notification delivery remain pilot gates.
See the [runbook](shuttle-alerts.md) and [bounded QA](shuttle-alerts-qa-2026-10-15.md).

Latest bounded changes cover OTLP origin paths and recovery of HTTP-200
`waiting_auth` runs after bot restart. Focused behavioral race checks, real
Telegram/IAM/Loom recovery of the original task/session/run and selected-state
preservation passed. A separate corrected-image outage/restart retry reached a
Loom `module_discovery_unavailable` pause and required explicit operator Resume;
automatic acceptance remains failed. Both original reads completed once in the
same task/run after that intervention. Full causal tracing across Loom's async
queue also remains open. See
[tracing and recovery QA](shuttle-tracing-qa-2026-10-15.md).

The preceding large feature slice was [agent event configuration](shuttle-events.md).
All 242 integration race scenarios, other Go packages, strict lint and fresh
independent correctness/quality reviews passed. Owned real-IAM/Loom exact
create/replace, restart, original-receipt recovery and scoped preservation
checks passed. The accepted stand is schema 31 / manifest `mvp-30`; see
[evidence, retained failures and limits](shuttle-event-change-qa-2026-10-15.md).
Complete Telegram approval parameters also passed a subsequent actual Telegram
Web display/rejection check using real IAM/Loom/Zinusya and a harmless synthetic
RPC leaf. The full visible JSON matched its input across two messages; rejection
reached the same operation with zero execution. See
[the separate scope and limits](shuttle-approval-display-qa-2026-10-15.md).
The preceding [worker callback duration metrics](shuttle-worker-duration.md)
slice passed all 225 integration race scenarios, other Go packages and lint.
Owned native no-op callbacks, private/public exposure and restart reset passed
with exact fixture cleanup; see [its limits](shuttle-worker-duration-qa-2026-10-15.md).
The preceding slice is [agent local user revocation](shuttle-user-revocations.md).
The scoped store/API, exact approvals, self-revocation recovery and cache
invalidation have passed focused behavioral race checks and fresh independent
source reviews. All 221 integration race scenarios, other Go packages and strict
lint passed. Real-service exact approval, independent receipt recovery after
self-revocation, restart and previous-binary compatibility also passed. The
revocation acceptance checkpoint used schema 30 / manifest `mvp-29`; see the
[native limits and retained observations](shuttle-user-revocation-qa-2026-10-15.md).

## Current foundation

Durable messaging, IAM delegation, operator uncertainty diagnostics,
agent administration with exact approval, event catalogue, time-zone persistence, Markdown/entities,
telemetry and restart recovery are implemented. Native local IAM/Loom/Telegram QA
exists. Remaining bidirectional zitadeltg proof waits for browser CA import and
interactive verification with Daniel. No production cutover is claimed.

Naming now distinguishes project zns-chatbot, bot Zinusya and rewrite Shuttle.
An earlier review of the authored Go implementation found bot-scope
lookup/revocation bugs. Behavioral reproductions and fixes are included in the
subsequent verification gates.

The pair acceptance checkpoint covers invitation delivery, explicit Telegram pair consent,
native Loom exact paired assignment, partial payment confirmation, participant
uncoupling and original receipts after bot restart. See
[2026-10-14 native pair QA](shuttle-pair-qa-2026-10-14.md). Earlier dated checkpoints
below retain their original acceptance boundaries; this is the current pair status.
Participant-specific tier quotes are also implemented and accepted in a bounded
native IAM/Loom/Telegram scenario; see [tier QA](shuttle-tier-qa-2026-10-14.md).
Full stage 3 remains open. Next: deterministic allocation with the recorded ordering
choice, then separately decided cancellation/deadline policies. Profile/expectation
and file classification work can proceed independently of those operator choices.

Owner profile, explicit interface language and a bounded agent name expectation
are now implemented and accepted on the owned stand. Their [contract](shuttle-profile-expectations.md)
keeps Telegram metadata separate and preserves payment/registration state on
name expiry. See [profile checks and native QA](shuttle-profile-qa-2026-10-14.md).
Framed avatar JPEG generation is now implemented and accepted on the owned stand:
separate leaf renderer, owner-scoped durable bytes/receipts, document outbox,
restart replay and real Telegram delivery. See [contract](shuttle-avatars.md)
and [native QA and limits](shuttle-avatar-qa-2026-10-14.md). The optional transform foundation now passes bounded Linux/Windows checks and
PostgreSQL acceptance: pan, zoom, rotation, canonical replay and legacy byte
compatibility. See [transform QA](shuttle-avatar-transform-qa-2026-10-19.md).
[Shared avatar draft storage and HTTP APIs](shuttle-avatar-drafts.md) now pin
owner originals and share CAS saves/operation replay across native and agent
entry points. Current source is schema57/mvp65; these changes are not installed
on the primary stand. The [authoritative preview API](shuttle-avatar-preview.md)
now renders saved drafts without new tasks or delivery. The
[native visual editor](shuttle-avatar-editor.md) passed source and isolated-browser
checks with a mock HTTP backend. The [finalization storage foundation](shuttle-avatar-finalization.md)
now atomically snapshots artifacts and delivery intents. The
[task-free document worker](shuttle-avatar-delivery.md) is implemented and exercised
with real PostgreSQL/River and mock peers. The [explicit repair store](shuttle-avatar-delivery-resolution.md) and its agent HTTP tools are implemented. [Shared native/agent finalization APIs](shuttle-avatar-finalization-api.md),
owner artifact recovery/status/content and bounded reactive metadata are implemented;
The [Generate/send UI](shuttle-avatar-send-ui.md) now passes17 avatar and all207
Mini App behavior tests, plus14 isolated real-browser checks including JPEG download.
[Owned runtime/browser acceptance](shuttle-avatar-runtime-qa-2026-10-19.md) now passes
native-only with Loom stopped, agent-only and both handoffs with real IAM/Loom.
The form-to-agent probe was repeated with explicit draft identity checks after
review found a stale-response QA error; the rejected proof is retained. A normal
0.657s SIGTERM keeps the original agent task/run across agent-to-form continuation.
Both nine-service QA stands are archived and removed. Separate
[real IAM/Loom administrator repair QA](shuttle-avatar-repair-runtime-qa-2026-10-19.md)
now proves presented exact consent, one explicit retry, dismiss without resend,
nonadmin mutation denial, immutable replay and a0.688s normal bot restart.
Its accepted stand has two artifacts and three document sends (the third is the
explicit retry); both repair stands are archived and removed. Actor-owned
[proposal recovery reads](shuttle-avatar-proposal-recovery-qa-2026-10-19.md)
now pass15 PostgreSQL and19 Linux API checks, with bounded pending pages and
unchanged receipt-only status semantics (schema57/mvp66 source).
[Canonical proposal adoption](shuttle-avatar-adoption-qa-2026-10-19.md) is now
implemented in schema58/mvp67 source, with24 PostgreSQL and19 Linux API roots.
Agent prepare can bind a new operation to the actor's original pending proposal;
three concurrent operation IDs confirm one canonical effect/result. Native
administrator session fencing and routes are implemented below; actual
cross-interface runtime repair handoff acceptance remains next.
[Native administrator HTTP/session/context support](shuttle-avatar-native-administration-qa-2026-10-19.md)
now passes33 PostgreSQL roots and69 Linux roots in schema59/mvp68 source.
Both HTTP handoffs share one result; session expiry and known IAM invalidation
while waiting roll back confirmation. Native history is bounded to five prior
locators and withheld after role loss. The new migration's rollback also fences
referenced parent tables with NOWAIT.
The [native operator repair form](shuttle-avatar-admin-ui.md) now passes18 focused
and225 total UI behavioral checks,19 Linux API roots and12 owned headless Edge
checks; see [source/browser QA](shuttle-avatar-admin-ui-qa-2026-10-19.md).
It preserves owner-bound original operation/proposal IDs across response loss,
requires explicit exact confirmation and separates old receipts from current
state. Both read-only recovery and agent-origin canonical proposal selection
avoid replacement POSTs. A request that did not arrive can be explicitly repeated
with the same idempotency scope.
[Owned real-IAM/Loom repair handoffs](shuttle-avatar-admin-runtime-qa-2026-10-19.md)
now pass on the separate schema59/mvp68 image. Native to agent keeps one canonical
proposal with fresh exact broker approval and an adoption alias; agent to native
confirms the original operation, then returns the same result to the agent after
an explicit reactive follow-up. SIGTERM shutdown takes0.672s; the pending run/proposal survives the normal restart.
Read-back has2 artifacts,2 completed resolutions,1 alias,2 native actions,
4 completed physical tasks,2 completed Loom runs and3 document sends including
one explicit retry. All9 temporary runtime services are archived/removed.
The subsequent owned primary installation above runs schema59/mvp68. Actual
Telegram WebView and unattended model acceptance remain open. Common PDF/audio/video
handling remains in Loom. Historical preparation/storage/apply and current payment
expectations are also accepted below. Broader attachment classification remains
open; the allocator and financial lifecycle choices above
are still open. Production retention and avatar migration remain separate work.

The massage schedule foundation is now accepted on the owned stand, including
agent API/manifest and exact administrative approval. It introduced schema 25;
booking and live reservation availability remain later work. See stage 6 below.

Partner contact-code links are now accepted as a separate stage-3 entry slice.
Owner-only code reads, durable untrusted link context and no automatic business
effects passed review, all 177 integration race checks and lint. Real local
IAM/Loom tool dispatch, restart-stable codes and actual Telegram Web self-link
opening passed. Existing explicit invitation/target consent remains mandatory;
allocator and financial lifecycle decisions stay open. See
[contract](shuttle-partner-entry.md) and [QA limits](shuttle-partner-entry-qa-2026-10-15.md).

Private PostgreSQL pool metrics are also accepted as a bounded observability
slice: local acquired/idle/total/maximum gauges, no scrape-time database query or
connection acquisition. Source reviews, all 178 integration race checks and lint
passed. Owned native concurrent scrapes, public-port denial and bot restart passed
without business-state changes. See [semantics and evidence](shuttle-pool-metrics.md).
The subsequent unresolved-task snapshot is now accepted with schema 26's additive
partial index. It exposes fixed-state counts, age since acceptance and honest
failure/freshness metadata, with a shared 30-second cache and 250 ms query budget.
All 179 integration race checks, other Go packages, lint and source reviews passed.
Owned native timeout/recovery, fixture cleanup, restart, actual Goose rollback,
previous-image real-IAM read and reapply preserved baseline records and receipts.
See [contract and native limits](shuttle-task-metrics.md). Intentional user wait is
not time waiting for a worker.

The independent outbox snapshot is now accepted with schema 27's partial index.
It counts unfinished chunks and external delivery uncertainty, preserving task
metric names/behavior through shared private collection mechanics. All 181
integration race scenarios, other Go packages, lint and fresh source reviews
passed. Native table-lock failure/recovery, restart, exact fixture cleanup,
old-image real-IAM read after index rollback and reapply preserved selected
baseline rows and receipts, including Mini App configuration. See
[outbox semantics and evidence](shuttle-outbox-metrics.md). Worker callback duration
is accepted above. An importable thirteen-panel
[operations dashboard](shuttle-dashboard.md) is now accepted with source-derived
promtool cases, fresh source review and real Grafana query/browser failure and
recovery checks. The browser-discovered interpolation defect and prior QA failures
remain recorded in [the QA report](shuttle-dashboard-qa-2026-10-15.md). Cloud
activation and end-to-end business completion latency remain later work.

Root acceptance to confirmed final-response delivery is now a separate accepted
observability slice, including queueing, user waits and service interruptions.
Schema 32 preserves start/final-range/delivery timestamps; a process-local OTel
histogram records confirmed receipts on a best-effort basis. All 256 integration
race scenarios, other Go packages, lint and fresh source reviews passed. Owned
native receipt/sample matching, completed restart without replay and old-binary
read on the additive schema passed after explicit manual-provider recovery. The
original summary-helper error, provider timeouts and file-socket repair remain
in [the QA report](shuttle-response-latency-qa-2026-10-15.md). This does not close
automatic provider recovery, business-completion timing or production deployment.

Four process-local identity cache series are now accepted separately: trusted
positive introspection hit/miss and successful service/user renewal fallback
decisions. Architecture/Go consultation, behavioral source checks, lint and fresh
source reviews passed. All 181 integration race scenarios and other Go packages,
real IAM miss/hit, private concurrent scrapes, public denial and bot restart passed.
The temporary enabled-cache fixture restored the exact original TTL 0 settings;
both earlier verifier failures remain retained. See
[counting semantics and verification](shuttle-identity-metrics.md).

The River due-job snapshot is now accepted as a separate bounded slice: nine
fixed kind/state pairs, scheduling lateness, independent cache/health and no
payload-derived labels. All 184 integration race scenarios, other Go packages,
lint and fresh source reviews passed. Native locked-backlog metrics, real
promotion/drain of nine safe jobs, exact 14-job cleanup, restart and preservation
of 22 selected table hashes passed. That queue-metrics checkpoint used schema27;
no index was added by that slice. Two
QA harness failures remain retained. See [contract and evidence](shuttle-queue-metrics.md).
An independently observed [startup/stop leader lease delay](shuttle-river-startup-lease.md)
remains open. A separate owned native probe now confirms available work proceeds
under a synthetic abandoned 15-second lease and the same scheduled job completes
once after natural expiry and successor leadership. Exact two-job cleanup and
22-table baseline preservation passed without source/lease-timing changes. This
does not reproduce or fix the original cancellation race, or establish a maximum
production recovery time. Due backlog does not establish worker utilization or
a fast production leadership handover.

Committed upstream Loom operation/PDF fixes are now accepted on the owned stand,
replacing the earlier working-tree seal fixture. Exact approval survived a Loom
restart and duplicate input; a real Telegram-backed PDF passed text extraction
and visual glyph inspection. See [consumer evidence and limits](shuttle-loom-upstream-qa-2026-10-15.md).
Historical checkpoints below retain their original failures. Real Telegram audio
and video now separately pass exact source hashes, native metadata, frame/audio
extraction and visible replies with a manual scenario provider. See
[media QA and explicit recovery limits](shuttle-media-qa-2026-10-15.md).
Two incomplete MP4 cases are now accepted separately: container-only metadata
without streams, and a rejected incomplete header with a healthy control before
and after. Both real Telegram tasks completed with exact source downloads and
unchanged selected business hashes. The initial erroneous expectation of a
container-probe error remains retained. See [bounded malformed-media QA](shuttle-malformed-media-qa-2026-10-15.md).
A real 21 MiB Telegram document exposed a size refusal incorrectly returned as
HTTP 503 through Loom. Saved oversize metadata now rejects reading with HTTP 413
after authorization and before Telegram IO; the same opaque file passed the native
recheck without another upload. See [file-limit QA](shuttle-file-limits-qa-2026-10-15.md).
The exact unknown-size cloud-size refusal is accepted separately above.
Native voice/video-note processing is now accepted separately on the current
`mvp-33` image: real Telegram forwarded updates, opaque IDs, exact source hashes,
local Loom metadata/frame processing and visible replies passed. No microphone,
camera or paid transcription was used. The manual provider and retained harness
failures are described in [the QA report](shuttle-voice-video-note-qa-2026-10-15.md).
Unattended attachment interpretation, speech understanding, other cloud refusals
and other malformed formats remain separate gates.

Uncertain-task closure now has an agent exact-approval path with durable
deployment/actor-scoped proposals and original receipts. Independent reviews,
behavioral checks and native real IAM/Loom approval passed; previous-binary
compatibility retained schema 28 and audit data. It does not resend messages or
cancel remote runs. See the [contract](shuttle-task-closures.md) and
[bounded acceptance](shuttle-task-closure-qa-2026-10-15.md).
Application-wide settings changes now have an agent exact-approval path as well.
Settings/audit/result atomicity, actual lost PostgreSQL COMMIT acknowledgment,
cache-preserving status polling and historical receipt replay passed behavioral
checks and fresh reviews. Native IAM/Loom approval, two owned IAM outages, audited
restoration and previous-binary/schema-29 compatibility passed. See the
[settings contract](shuttle-settings-changes.md) and
[acceptance record](shuttle-settings-change-qa-2026-10-15.md).
Agent [user revocation](shuttle-user-revocations.md) is implemented in source;
all 221 integration race scenarios, strict lint and bounded native acceptance
passed. Further owner API calls are denied after self-revocation, and current
Loom pauses before another model turn. The successful tool result is recoverable
by another active administrator. See the
[QA boundary](shuttle-user-revocation-qa-2026-10-15.md).
Interactive CA/zitadeltg and outstanding business-policy gates remain open.

## 1. Solo registration and assigned-pass access

Configure per-event registration rules and bounded capacity. Register one local
user with an explicit event and dance role; preserve a waitlisted registration.
Allow authorized administrators to assign a price/currency snapshot atomically
within capacity. Read a user's own registration/pass; assigned-but-unpaid passes
qualify for enabled event modules. Recheck module enablement, ownership and pass
state at execution. Mutation receipts preserve operation ID, input digest and
original result; stale versions and changed retries conflict.

Expose the same business operations through the authenticated API and Loom's
durable write protocol; do not present operator PUT endpoints as agent writes.
Then resolve event context from explicit/pinned event, current/nearest eligible
event, or clarification when ambiguous. No automatic pair allocator is claimed
by this first bounded solo slice.

## 2. Confirmed payments and deposits

Use exact amounts and currency snapshots, separate evidence submission from
ambassador confirmation, and preserve a ledger of deposits/top-ups. Minimum
amount/share is event-configurable. Evidence must arrive before deadline;
confirmation may happen later. Overdue remains unpaid until an ambassador or
event administrator decides; no automatic forfeiture/cancellation. Match multiple
payment expectations by amount/context and ask when ambiguous. Never credit
money from the agent's classification alone.

## 3. Pairs, allocation and full registration lifecycle

Implement invitations, acceptance, paired assignment/cancellation, role balancing,
tiers/prices and ordering based on reviewed legacy behavior. Pair writes are
atomic in PostgreSQL. Preserve participant-specific price snapshots and explicit
human decisions; prove concurrency and deadline boundaries.

## 4. Files and avatars

Give Loom opaque file IDs and authenticated read tools rather than sending bytes
at run creation. Pin ownership and metadata, enforce bounded local policies and
handle expired Telegram access. The agent distinguishes payment evidence,
avatars and unrelated media. Reuse Loom's common PDF/audio/video tools; isolate
specialized avatar processing if external binaries are required. Stickers remain
unsupported; custom emoji use ordinary fallbacks until their cache stage.

Browser-original quota and expiry are implemented and locally accepted at schema 70 / manifest `mvp-90`; the primary stand is still on schema 69 / `mvp-89`. See [retention policy, rollback and acceptance limits](shuttle-avatar-source-retention.md). Telegram originals remain reference-only.

## 5. Import and cutover preparation

A known-key embedded historical registration read is now implemented with owner
authorization, full archive validation and no live-business writes. Focused race
and strict lint checks passed, as did owned real IAM/API/Loom acceptance with
manual test-provider dispatch and preserved prior history/business state. This is
not a completed migration. See the
[contract](shuttle-history-registration.md) and
[QA boundary](shuttle-history-registration-qa-2026-10-15.md).

Bounded candidate discovery is now accepted separately: owner-only listing of
structurally supported embedded claims, literal-key pagination, complete archive
validation and final IAM checks. At that discovery checkpoint, schema was32 and
manifest was `mvp-32`; the later [main cutover](shuttle-prelink-main-qa-2026-10-16.md)
uses schema33 and `mvp-33`.
Independent reviews, lint, all 259 enumerated integration race scenarios and other
Go packages have successful receipts. The initial payment-notification timeout
and subsequent diagnostic/recheck passes remain recorded. Owned real-IAM/Loom
reads discovered unknown keys and preserved prior history and live business.
Unsupported shapes are omitted and role-only objects can qualify; candidates
never establish current eligibility or confirmed money. See
[discovery scope and evidence](shuttle-history-discovery-qa-2026-10-15.md).

The first offline preparation slice is implemented: a strict, deterministic local
snapshot inspector with source checksums, bot/owner/conflict validation and no
runtime writes. See [contract and acceptance](shuttle-import-preparation.md).
It does not perform the import below. Legacy assistant history is partial, and
embedded historical pass records remain in untouched source files.
Verified private archive creation is also implemented and accepted on native Linux
and Windows NTFS. It preserves the exact inspected bytes and publishes without
overwriting a prior archive; see [bundle contract and QA](shuttle-import-bundle.md).
Receiving verification is now accepted separately: CLI, administrator streaming
upload and owner-scoped file-ID agent tool recompute validation without storage
or import. Full race tests, review loops and real local IAM HTTP acceptance passed;
see [receiver contract and limits](shuttle-import-receiving.md). Exact-approved
immutable PostgreSQL storage is now accepted separately, with atomic chunk rollback,
original receipts, native disconnect/restart QA and no live-business writes;
see [storage contract and QA](shuttle-legacy-archive-storage.md). Explicit historical
apply, source-key reconciliation and owner-scoped historical reads were not part
of that storage checkpoint.
The shared decoder and PostgreSQL chunk reader are now accepted as prerequisites,
with whitelist projections, provisional callbacks and native Loom script-based
stored validation; see [decoder scope and QA](shuttle-history-decoding.md).
Historical apply and bounded owner reads are now implemented as a separate slice,
with exact approval and atomic conflict handling. Reviews, full race/lint checks,
restricted-runtime-role proof and real owned IAM/Loom approval/read/restart acceptance
passed; see [contract and limits](shuttle-history-apply.md). Real export/account/avatar
migration remains open. Current payment expectations are now accepted separately:
bounded owner-only refresh, event-local deadlines and checkpointed reactive prompt
context alongside the existing name request. Three review perspectives, full race
checks, zero-issue lint and owned real IAM/Loom/Telegram PDF clarification/submission
and restart persistence passed; see [contract and limits](shuttle-payment-expectations.md).
The native provider was manual Codex, not an unattended classification evaluation.
Pending proofs remain awaiting human confirmation; expectation presence never
determines an attachment's purpose by itself. Bounded incoming pair invitation
context is now also accepted alongside name/payment expectations: reviews, full
race/lint checks and real owned IAM/Loom/Telegram clarification, unrelated continuation
and restart persistence passed. See [contract and limits](shuttle-pair-expectations.md).
Explicit consent remains mandatory; no pair is created from context alone.
The event-enabled orders catalog/read foundation below is now accepted. The
server-calculated owner quote with explicit complete pricing is also accepted;
allocator and production-import decisions remain open.

Import users, identity links, conversation history and avatars with repeatable
dry runs and reconciliation. There are no ongoing events to migrate. Preserve
historical registrations/payments as agent-readable records with provenance;
do not import them into the new payment ledger or create active passes. Complete
server-specific deployment/rollback gates before any authorized cutover.

## 6. Massage and orders

Add event-enabled resource bookings, meal/order processes, schedules, local
deadlines and rights through the same module boundaries. Carry the reviewed
legacy nuances forward without rebuilding the agent runtime inside the bot.

The massage schedule/candidate foundation is implemented and accepted: versioned
per-event configuration, exact administrative proposals/receipts and assigned-pass
owner reads. Candidate times distinguish treatment/occupancy and DST offsets;
they explicitly exclude live reservation availability. Fresh model/DB/API reviews,
all 173 current integration race tests, other Go packages and zero-issue lint passed.
Owned real IAM/Loom approval, unpaid-owner script reads, module disablement,
stale-version denial and restart/original replay passed. See
[contract](shuttle-massage-schedules.md) and [native QA limits](shuttle-massage-qa-2026-10-15.md).
Later [owner booking/cancellation and agent API component gates](shuttle-massage-agent-api-qa-2026-10-17.md)
now pass. Live availability, native forms, specialist authority, charging and
reminders still require their workflow acceptance; stage 6 remains incomplete.

The bounded orders catalog is implemented and accepted:
versioned per-event DB configuration, exact administrative writes and owner reads
requiring an assigned pass (including unpaid). Explicit service windows and sales
deadlines use the event's IANA zone; closing sales does not hide an entitled owner's
catalog. No cart, reservation, currency conversion or money confirmation is implied.
Fresh independent reviews, zero-issue lint, all 159 enumerated integration tests
and other explicit packages with race passed. Owned IAM/Loom/Telegram acceptance
covered exact admin approval, unpaid owner reads, disablement and bot restart.
See [catalog contract and acceptance](shuttle-orders-catalog.md) and
[legacy constraints](shuttle-orders-foundation-plan.md). The
[owner quote slice](shuttle-orders-quote.md) is also implemented and accepted:
explicit complete-pricing attestation, required per-portion/per-offer supplements,
bounded exact arithmetic and current owner/module/version/deadline checks. Fresh
review loops, zero-issue lint and all 160 current integration tests plus other Go
packages with race passed. Real IAM/Loom/Telegram returned 24.93 BYN; an edit and
restart preserved stale-version denial and a fresh 24.95 total. This is a read-only
calculation; unresolved order-write policy remains open. A manual provider summary
failure was corrected on the same accepted run, without resending the TG request.

## Earlier foundation: embedded account bootstrap

Architecture and Go consultations agreed on a bounded
[Mini App session plan](shuttle-miniapp-plan.md): verified Telegram initData,
durable replay-safe bootstrap and an opaque application session, with all IAM
credentials kept server-side. PostgreSQL persistence, HTTP routes and the minimal
UI are now implemented; see [the contract and pending gates](shuttle-miniapp.md).
Fresh review loops, zero-issue lint, all 164 enumerated integration race tests and
other explicit Go packages passed. Owned real-IAM/signed-fixture acceptance also
passed, including simultaneous IAM/Loom downtime with a bot restart, offline
logout and recovery of the same session. See [native QA](shuttle-miniapp-qa-2026-10-15.md).
At that checkpoint, real Telegram launch and iframe checks were deferred. Later
HTTPS-to-localhost and browser-permission acceptance is linked at the top;
interactive zitadeltg/CA acceptance remains separate. Order lifecycle,
allocator and import decisions do not block this authentication foundation.

The next owner status slice is implemented and accepted on the owned fixture:
[own registrations, passes and confirmed payment status](shuttle-account-status.md)
in Mini App and the agent toolbox. It includes waitlists, fully paid and ended
events without changing event choices or financial state. Focused race checks,
fresh review loops, twelve Node UI behavior tests and zero-issue lint passed.
All 168 enumerated integration race tests and other Go packages passed. Real IAM
and Loom script reads confirmed owner/admin isolation, pagination and restart
without business or event-pin changes; see [native QA](shuttle-account-status-qa-2026-10-15.md).
At that checkpoint, actual Telegram WebView acceptance was deferred; see the
later launch and embedded acceptance at the top of this roadmap.

## Later transport improvement: MTProto

Consider a separate MTProto bot transport after the current Bot API business
slices are accepted. Telegram's [messages.sendMessage contract](https://core.telegram.org/method/messages.sendMessage)
uses random_id to prevent message resending and supports bots. Persist that ID
with each outbound delivery before network IO and reuse it across retries and
restarts. Verify receipt/update mapping, session recovery and actual duplicate
suppression in owned-stand QA before claiming delivery guarantees. This does not
change financial operation IDs or ledger idempotency.

With the current Bot API, an uncertain send remains visible for reconciliation.
The user allows a second message only as a last resort when delivery cannot be
established and completion needs it. Check stored receipts and the actual chat
first where available; explicitly record possible duplication. Never replay the
underlying payment or registration to recover a message.


## Progress checkpoint: 2026-10-13, solo registration foundation

Implemented and reviewed: explicit registration rules, solo owner waitlist,
manual administrator assignment with role-capacity serialization and versions,
frozen price/deadline, durable original operation receipts, owner reads and
assigned-unpaid module eligibility predicate. Linux race/full PostgreSQL tests,
linter and real-Zitadel application-restart QA passed. See
[contract and evidence](shuttle-solo-registration.md).

Owner native Loom write/read/status adapters are implemented and reviewed. Direct
real-IAM acceptance passed; native broker write stopped before dispatch under
[Loom issue 3](https://github.com/complynx/loom/issues/3).

Permission-filtered administrator assignment with exact confirmation is implemented,
reviewed and accepted through direct real-IAM API calls, including an actual bot
restart. Review found and fixed a recipient-lock deadline race; the behavioral
reproduction failed before the fix and passed afterward. See
[assignment contract and evidence](shuttle-agent-assignments.md).

Event context selection and durable user/run choices are implemented, reviewed and
accepted with real IAM and an actual bot restart. See
[event context contract and evidence](shuttle-event-context.md).

Roadmap step 1 is not complete: Telegram registration flow must still be accepted
through the current test-bot chat. Native broker writes and exact administrator approval passed with the
[isolated upstream working seal fixture](shuttle-loom-seal-fixture.md). The original
issue 3 failure remains preserved; the upstream committed image is not certified
fixed. Telegram Web control currently times out, so its UI gate remains pending. Pair allocation and payments remain
later phases.


## Payment progress: deposit terms

Event-configurable amount/share minima and immutable assigned-pass deposit terms
are implemented and reviewed. Exact arithmetic, version/receipt preservation,
later event edits, restart and native real-IAM/Loom exact approval passed. See
[deposit policy contract](shuttle-deposit-policy.md). This checkpoint covers configuration and snapshot persistence. Evidence and ledger
progress is recorded below; step 2 is not complete.


## File dependency progress

Durable opaque attachment references, trusted Telegram message timestamps and
owner-authenticated info/fetch/content are implemented and reviewed. This part of
step 4 was brought forward for payment evidence. Supported media shapes, duplicate
intake, bot restart, source loss and revoked/foreign access passed behavioral QA;
real IAM/Loom imported a PDF through the broker using an isolated HTTPS Telegram
fixture. See [file contract and acceptance boundary](shuttle-files.md).
Real Telegram upload/read and bounded PDF text extraction now pass, including reuse
of the opaque reference after bot/worker restart. PDF-to-image visual QA fails on
missing fallback fonts (Loom issue #4), despite successful PNG format checks.
Avatars and audio/video processing were pending at that checkpoint; subsequent
bounded acceptance is recorded above. Production retention remains open. Evidence/
ledger progress is recorded below; agent file ingestion never credits money itself.

## Payment progress: event authority

Event-local ambassador/admin roles and global-admin exact-confirmation grant/revoke
are implemented. They have durable original receipts and actor/target/version
checks. See [authority contract](shuttle-event-authorities.md). At this authority checkpoint, payment evidence,
confirmed ledger and balances were pending; subsequent progress is recorded below.
Authority assignment alone never credits money. Three independent reviewers, full Linux race tests and real IAM/native Loom exact
approval/revocation/restart acceptance passed. Final lint passed with 0 issues. Step 2 remains incomplete until evidence and ledger acceptance.

## Payment progress: evidence and ledger

Pending evidence, human-confirmed deposits/top-ups, immutable decision audit,
remaining balances, exact approval/replay and permission-scoped proof reads are
implemented. Proof arrival uses persisted Telegram timestamps. Event choices expose
assigned payment expectations after sales closing. See
[payment contract and acceptance boundary](shuttle-payments.md).
Source review fixed first-payment top_up deadline bypass and missing decision
visibility to other authorized reviewers. Focused HTTP/PostgreSQL critical tests
passed, including the final full race suite and zero-issue lint. Fresh real-IAM/native
Loom exact payment approval and bot restart preserved one deposit ledger record
after the user's full Docker prune. Synthetic closed metadata was used; this does
not establish real Telegram proof upload or content understanding.
Historical native evidence remains on disk, but the old runtime DB was deleted by
that prune; current IAM/runtime fixtures are newly provisioned, not restored rows.
Ambassador notifications, common PDF/receipt processing and remaining lifecycle
work still need their own acceptance; this is not completion of all step 2.

### Notification reconciliation progress (2026-10-14)

Durable submitted/decision notifications and global administrator diagnostics are
implemented. Audited exact-confirmation retry/dismiss now has persisted five-minute
proposals and original operation receipts; it changes delivery only. Focused
HTTP/PostgreSQL race tests and independent correctness review passed, with zero-issue
lint. Native submitted/confirmed notification/restart QA and exact dismiss approval
now pass on our own stand; native unknown receipt was explicitly fault-injected.
See [native QA limits](shuttle-notification-qa-2026-10-14.md) and
[notification delivery and reconciliation](shuttle-payment-notifications.md).


### Native payment correction/top-up progress (2026-10-14)

A real Telegram PDF/caption flowed through owner-scoped Loom PDF text processing,
balance read and pending top-up submission. Native exact rejection preserved money;
an explicit follow-up resubmitted the same file with its original sent_at instant.
Exact confirmation then completed the synthetic pass: 30.87 + 92.58 = 123.45 BYN,
remaining zero, fully paid. Original receipt, three decision revisions and Telegram
receipt survived bot restart. This used a manual Codex mailbox, explicit event/pass
and synthetic money. See [native QA limits](shuttle-topup-qa-2026-10-14.md).
At this payment checkpoint, pairs/allocation and remaining file/expectation/
classification work were open. Current pair progress is recorded above.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
