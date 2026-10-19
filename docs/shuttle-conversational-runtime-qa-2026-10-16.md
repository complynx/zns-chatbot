# Conversational runtime acceptance, 2026-10-16

This is scoped owned-stand acceptance with real Zitadel, merged Loom and Telegram,
and a manual mailbox provider. It does not prove unattended model interpretation,
full business workflow acceptance or production readiness. No production or Loom
developer checkout/stand was changed. Publication remains on hold.

## Candidate and source gate

The bot runs image5a2dc5829acf on application schema36, manifest mvp-36. Loom runs
imagea75493893b4d on schema4, built from exact merged commit
fc6c5747a4359ff94a58071054e124ba1449cc89. The full immutable image IDs and hashes
are in ignored .cache/shuttle-work/conversation-images-qa/checkpoint.json.
Both unmodified release Dockerfiles built successfully;707 bot release inputs and
1276 upstream files were recorded. The dedicated bounded builder/cache was removed.

The adapter passed fresh independent source reviews and lint with zero issues.
The final Linux/race gate used real PostgreSQL and mock peers in six fresh bounded
container groups:556 ordinary test roots plus the default FuzzRender seed root,
1084 passing test events. The deliberately skipped TestWatchdogHelper parent is
executed through its watchdog subprocess scenarios; there were no unexpected
skips or failing test events. All805 frozen inputs and ten original stand
fingerprints stayed unchanged during this gate.

Evidence: .cache/shuttle-work/conversation-full-matrix-v2-qa/continuation-checkpoint.json.
Unit tests passed before a five-second PostgreSQL cleanup observation expired.
The abort handler subsequently observed PostgreSQL exit0/noOOM and removed the
exact resources; the separate unit reconciliation receipt retains this failure.
Other groups used a30-second cleanup observation. All group resources were removed,
with no test OOM. This is disposable database cleanup, not application restart QA.
Earlier single-process OOM, obsolete contract assertions, omitted fuzz inventory
and failed cleanup receipts remain preserved separately; none was relabelled.

## Data and deployment

Both real stand databases were restored into disposable copies. The actual images
migrated bot34->36 and Loom3->4 with all pre-existing table hashes preserved.
Clone containers exited0/noOOM and were removed without a persistent clone volume.
Evidence: .cache/shuttle-work/conversation-schema-qa/checkpoint.json.

Before the owned cutover, ingress stopped and tasks, unresolved deliveries and Loom
runs were checked quiescent before and after stopping the two applications. Fresh
private custom-format backups were retained. Migration preserved existing data.
Only the two application images changed; configuration, mounts, limits and seven
companion instances were preserved. Docker Desktop bind aliases were canonicalized
for comparison, with raw mounts retained. The same poller container was restored
and a fresh successful polling cycle observed after the real-IAM owner profile read.

Startup changed only River leader leases and queue activity in each database.
Every other table hash, including River jobs, remained identical. Queue names,
creation and pause instants, row counts and metadata were compared with the backup;
only updated_at is exempt. This fixture requires empty-dict queue metadata and
fails closed on nonempty metadata. Failed stricter startup verification remains
recorded. Evidence: .cache/shuttle-live-test/conversation-main-cutover/.

## Actual Telegram checks

Only the authorized current account in @complynx_test_bot was used. Two question
conversations and five inbound tasks checked the following:

- Reply-keyboard buttons display plain Вариант А and Вариант Б. Clicking the latter
  sends exactly that text; one steering input reaches the original run.
- During the second question, /menu completes through the native route, with no
  Loom run for that command. The later user input includes this exact menu request's
  timestamp in the manual-navigation context.
- The free text Отмена, я передумал; ничего не выполняй. reaches that same second run
  as steering, with no client-side cancellation/choice parsing. The manual provider
  completes the dialogue without a business action.
- Each accepted receipt ID/context matches the single corresponding run_inputs row
  and the bot's durable control body. Twelve protected business table hashes and
  application/companion fingerprints remain unchanged.

Evidence: .cache/shuttle-live-test/conversation-telegram-qa/checkpoint.json.
Browser-visible final responses were checked. A keyboard screenshot was displayed
inline during QA; filesystem saving was rejected by the browser tool's read-only
permissions, so no screenshot file is claimed in this report.

## Recovery anomaly and evidence limitation

A separate direct API probe negotiated agent mode, rejected legacy answer with
409/use_steering, and accepted steering plus an immediate identical-key replay.
It then entered waiting_auth despite an unexpired, unrevoked session. Reads with
that session and a fresh subject both succeeded. The cause is not established.
Standard same-session reauthorization1->2 recovered the SAME run; exact final text,
original receipt ID/context and one accepted input were verified. No replacement
run or Telegram message was used. See ignored
.cache/shuttle-work/loom-conversation-auth-investigation.md. This is a recovery
limitation, not a claim that an IAM token expired or a confirmed Loom root cause.
The two actual-bot Telegram conversations completed without this manual recovery.

The Telegram QA helper initially reused a provider-response function that wrote to
the direct probe's global report path. Its original accepted checkpoint bytes were
lost. The exact overwritten artifact and original pre-recovery failure were retained;
no original receipt was reconstructed. The helper now writes only its own report.
Fresh read-only API/DB checks, plus replay of the original accepted input key, verified
the same completed run, final text, session version2, receipt/context and one input.
Use .cache/shuttle-live-test/conversation-contract-probe/verified-after-helper-overwrite.json
for this current-state evidence. The canonical checkpoint explicitly marks artifact
overwrite, and the cutover retains the earlier expected hash plus the follow-up.

## Remaining acceptance

Real-model RU/EN interpretation, actual business approve/reject and protocol-bound
operation notifications on this merged runtime remain separate gates. Native/form
registration and payment workflows, protected legal-data forms and continuation,
trusted browser/OIDC linking, orders and massage remain on the infrastructure roadmap.
The legal-data module's encryption key is not enabled on this stand. No browser
trust installation or real Telegram OIDC acceptance is implied by this rollout.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
