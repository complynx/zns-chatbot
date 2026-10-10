# Go platform QA guide

This guide is for an explicit future resume. Development and QA were stopped by
Daniel on 10 October 2026. It grants no execution authority. The
[frozen state](go-platform-state-2026-10-10.md) records acceptance and limitations.

## Requirements and independent roles

Use [original conversational flows](qa/fqa-flows-20260930-plan.md),
[recovery/import requirements](qa/fqa-recovery-import-plan.md),
[active38 exact requirement archive](qa/go-platform-pause-2026-10-10/active38-original-requirements.md),
[architecture C–E](architecture-refactor-plan.md),
[manual Functional policy](manual-functional-qa.md) and the full migration gate.
Short matrix assertions index the original requirements; they do not reduce them.

The active partition is C15 = F01–F13,R15,R16; D16 = R01–R14,R17,R18;
E7 = I01,I02,I06,I07,I08,I09,E-RESET-REAPPLY. I03–I05 are historical,
explicitly superseded and NOT PASS. At the stop only F03,R01,I01,I06,I08 had
scoped acceptance. Every other active outcome is open, including full current
composition evidence for previously accepted slices.

Code QA is independent from the author, read-only on product/source, and reviews
correctness, authorization, transactions, dependencies, complexity, tests and
suppressions. Reuse exact unchanged-body coverage as permitted; inspect changed
producer/consumer interfaces. Source clean is not runtime accepted.

Functional QA is a separate fresh implementation-blind role. Supply original
requirements, public neutral contracts and admitted sandbox access only. Do not
supply product/helper implementation, author diagnoses, engineering maps, previous
findings, routing counters or this archive's troubleshooting narrative as the
reviewer's intake. The archive is for coordination and developer knowledge.
QA owns its evidence directory and may prepare instruments without product edits.

## Prepare the actual stand before the scenario clock

1. Establish exact current source/image/component IDs, bounded resources,
   readonly input hashes, actor mappings and sole output writers. Public identity
   receipts may disclose digests, never private environment values.
2. Verify that the QA role is actually active and its declared evidence directory
   exists. A queued message to a completed agent does not establish readiness.
3. Qualify the complete existing public observation path with a nonbusiness
   readback: typed fields, per-role required health, permissions and actual action
   connection. Missing healthcheck is explicit none; declared health is required.
4. Bind real Created/prearm/readiness observations to a finite current admission.
   A template, copied receipt or caller ACK cannot substitute for actual inputs.
5. Arm diagnostics before reproducing a failure. Keep errors bounded and private;
   use safe cell/step/actor/locale/pointer and validated operation IDs for correlation.
   Do not log message bodies, headers, URLs containing secrets or credentials.

Use the [FQA toolkit](../platform/scripts/fqa/README.md) and existing neutral
access contracts. No current browser endpoint or valid grant is shipped in Git.
A generic Compose recipe is a disposable developer sandbox, not a current QA
release and not permission to operate preserved data.

## Execute the original behavior

Applicable UI scenarios require EN/mouse, EN/emulated-touch, RU/mouse and
RU/emulated-touch in a Telegram-like UI: messages, keyboards/buttons, callback
ACKs, edits, uploads/downloads, history and fresh-session state. HTTP-only checks
cannot replace UI. Emulated touch is not physical-touch proof.

Cover normal behavior, retries and ordinary dependency failure, plus graceful
application replacement during processing. Existing tighter clocks remain. Normal
shutdown is at most five seconds, with a one-second target, and old writers must
physically finish before new writers start. Forced kill/timeout is FAIL. Precise
SIGKILL at speculative internal transaction points is optional unless a named
requirement or established high-impact risk makes it necessary.

Preserve authority cells: owner, ordinary user, event-specific and unrelated-event
roles, global admin without the domain role, and revoked authority as applicable.
Record why a cell is inapplicable. F06 requires withdrawal of the applicable role
OR publication consent; browser logout does not replace source/context retirement
for F07. Consent is bound to author/proposal/version/body/destination.

Locale changes must persist and govern later responses/actions. Old text need
not be translated retrospectively; old callbacks must remain safe and not block
new input. Stored UI locale and the language of a question/answer are separate.
Pending forms do not trap unrelated free text or media.

Finish and correlate one accepted action before switching actor/locale/card.
Separate exact ingress replay, restart after completion and effect-boundary
interruption. Business ACK does not prove completion. Uncertain accepted input
requires factual terminal/effect evidence; do not replay it or clear uncertainty
merely because the browser or operator closed.

## Capture and report

Reports state exact requirements, binding/commit/images, all cells, actions,
observable results, byte/hash comparisons, persistence/permissions, clocks,
resource/native closure and evidence limitations. Distinguish PASS, FAIL,
NOT_RUN/BLOCKED/UNAVAILABLE and SKIP. Skipped, deleted or optional checks are not
passed. Developer execution of QA-authored instruments is developer evidence,
not a new independent Functional verdict.

A failure identifies expected/actual behavior, minimal observed reproduction,
evidence and impact. An observer timeout before input is not a product defect.
Preserve the first error; attach rejection handlers before waiting on other
asynchronous UI work so diagnostic/done receipts are not lost.

Actual native closure requires terminal exit, both EOFs, kernel reap, no force
and no retained work, plus exact owned-resource absence and actual outer-process
completion in the original window. Owner-transcribed tool metadata is not an
independently exported provider trace. State that limit rather than invent proof.
Late current absence cannot turn an expired cleanup or failed original run into PASS.

When using a fresh noninteractive Codex CLI role, keep the QA full report separate
from `--output-last-message`. The latter overwrote F05's report with its final
summary. The detailed report was recovered from the completed author command;
its retained provenance and limitation are in the archive. Do not overwrite
another reviewer's evidence to repair this mistake.

## Stage and final completion

Both independent QA gates and mandatory automated checks must pass the original
stage scope before advancement. Full E requires full accepted/released C,
protected immutable inputs, sole bulk writer, importer-free runtime, permanent
history/proof-byte/identity preservation and whole-target recovery. Never reset
held data to obtain green evidence.

Run complete final composition and quality checks; verify real model/ASR,
Telegram, Zitadel least privilege and payment/provider behavior using only the
authorized test boundaries and explicit spend limits. Synthetic fixture PASS
does not accept external integration. Include backup/restore/rollback and all
remaining parity obligations. Neither a scenario count nor a successful build
can complete the whole goal.

Written by root (model not exposed/Codex)
on behalf of Daniel Drizhuk
