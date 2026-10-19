# Isolated registration and massage runtime QA: 2026-10-18

## Scope

Two QA agents used separate named mock-Telegram stands with the exact reviewed
schema48 / mvp-54 candidate, real Zitadel, PostgreSQL and Loom. Telegram and model
responses were synthetic; application endpoints, authorization, tool dispatch,
business writes and durable receipts were real. Primary shuttle-live-test stays
schema47 / mvp-51. No primary upgrade, real Telegram message, production change,
commit or publication occurred in this gate.

This is native HTTP/command and agent integration acceptance. Mini App API checks
used verified HTTPS and actual application sessions; this run did not render and
click the browser UI or prove a trusted Telegram WebView. See the earlier UI and
real-Telegram milestones separately. The reusable infrastructure and its narrower
acceptance are described in [the stand report](shuttle-mock-telegram-qa-2026-10-18.md).

## Registration and assigned pass

The qa-a agent created synthetic event/rule fixtures, with no direct SQL
registration or pass writes. The following implemented participant paths pass:

- Native /register event and role selection detects missing legal-name/passport
  data. Saving through the Mini App API leaves zero registrations. A later
  explicit role click creates one waitlisted registration. This whole path adds
  zero Loom runs; a duplicate click retains the same row and a foreign private
  callback is rejected with HTTP400.
- A fresh natural-language request registers another participant through actual
  Loom context and join tools, with synthetic model choices.
- Native name expectation and event selection continue through Loom name
  completion and registration. Native /profile, /registrations and the Mini App
  show the same saved facts. Reusing the original native role button returns the
  same registration rather than duplicating it.
- A fixture administrator receives real IAM grants and an exchanged user token.
  Its internal assignment API creates one pass; exact-key replay returns that
  original pass. An ordinary participant receives403 and another owner's read404.
  Native status, Mini App and real Loom registration/balance reads agree on
  123.45 EUR price, zero confirmed money and 123.45 EUR remaining.

All18 new qa-a tasks and all3 new Loom runs completed, with no pending model
request or unfinished task. Native participant role selection is a waiting-list
request; the organizer assigns the pass. This gate does not prove a native
administrator assignment UI, exact-approval agent assignment or payment review.
Those remain separate parity/acceptance work, not participant self-assignment
requirements.

A report assertion initially read pass.id instead of the actual nested
pass.balance.pass_id. The proposed retry was rejected automatically because its
old finalizer still included sends. It did not execute. The corrected finalizer
used saved views and read-only audits; no business request was repeated. A bool
callback receipt extraction and a fixture SQL column typo were also QA-driver
errors, not product failures. Their history remains in the ignored report folder.

## Specialist authority and massage operations

On qa-b, a separate fixture administrator and a specialist without any pass use
real IAM identities. Native preparation makes no binding effect. Applying without
exact consent fails400; explicit confirmation grants event-bound access and exact
replay retains one effect. Native and actual Loom staff reads agree on the
shared timetable, own-client detail and own contact references. Foreign detail
returns404 and the shared timetable omits foreign contact references.

A native revoke proposal is adopted by an actual Loom operation under a different
interface operation ID. It retains its original canonical operation and expiry.
After an explicit conversational answer, interaction resolution applies the
revocation. Later accesses check current authority. A historical operation receipt
still returns its original result after subsequent grant changes; it does not
represent current access.

Two historical bookings were SQL fixtures only. A separate participant booking
then used real native prepare, explicit consent and apply. Replaying it kept the
same effect. Staff HTTP and the same actual Loom run both saw shared3 / own2 rows.
Native cancellation, confirmation and replay changed the booking once to
cancelled; both reads then saw shared2 / own1 with the cancelled booking absent.
The final specialist grant was revoked. A fresh staff read returns404; a specialist
cannot access administrator tools (403). This proves the native business operation
and reactive agent read path, not an autonomous model's choice or actual client UI.

## Remaining gates and evidence

The qa-a report lives in .cache/shuttle-work/core-mixed-mock-qa/agent-a/; qa-b in
.cache/shuttle-work/massage-staff-mock-qa/agent-b/. Credentials, tokens, raw proofs
and diagnostic transcripts stay ignored. Synthetic model replies do not establish
unattended inference quality. Real Telegram/WebView, payment cases, administrator
assignment parity, full massage parity and production readiness remain open.
A full qa-b stop/start then retained all13 tasks, all14 outboxes and exactly the
same15 sendMessage attempts. The original unknown task and outbox both remain
delivery_unknown, without another send. Both A/B stacks were stopped after QA to
release memory; their data and evidence remain available.

The runtime gate used bot image76b7d89e6d6d, before a follow-up presentation-only
source fix. The actual specialist approval included a readable service-generated
summary plus redundant raw UUID/version JSON. Its Approve/Reject reply keyboard
had no operation tags, and a free-text response resolved the exact current
interaction. The follow-up suppresses the redundant machine block for that
canonical quote. Its [focused source and actual runtime acceptance](shuttle-specialist-approval-readable-qa-2026-10-18.md)
is separate from these business proofs.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
