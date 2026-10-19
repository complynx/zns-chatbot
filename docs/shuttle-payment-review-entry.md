# Native payment-review entry and reactive context

Current acceptance: [four real Telegram happy paths](shuttle-payment-review-real-qa-2026-10-16.md)
pass with real IAM/Loom and a manual provider. Embedded critical paths, UI
follow-ups and unattended model behavior remain open.

## Source behavior

`/payment_reviews` is a structured command. It needs current verified IAM identity
and event reviewer access, but does not call Loom. Eligible reviewers also see
it in `/menu`. It opens the Mini App reviewer section; an ordinary participant
gets an access explanation. The HTTP API checks authority again for every read
and decision. A disabled form gets a localized unavailable reply.

New submitted-proof notifications carry a Mini App link with event/evidence IDs.
The link selects authorized evidence, including events outside the first page.
It carries no approval or session credential. Opening or forwarding it cannot
confirm a payment. A pending uncertain operation in the same owner's browser tab
is recovered before a different notification target is opened.

Completed Mini App call receipts form the action history; there is no extra
journal table. At the next explicit agent task, at most five earlier receipts
are added, newest first. Fields are operation, event, evidence, workspace IDs,
immutable workspace revision, kind, original outcome and completion time. No
amount, free-text reason, file bytes or session credential is included. Workspace
replacement inserts a new UUID/revision row; earlier revisions are not updated.

The read is restricted to the current actor, deployment bot/organization,
enabled registrations module and current event reviewer authority or verified
global administrator role. Revoked event access removes its context. Tasks with
no native review history avoid an additional IAM role check. Form calls never
create or resume a Loom task by themselves. The prompt marks history untrusted
and explicitly denies treating it as consent or a new instruction. Continuing a
user request requires reading current authorized workspace/operation state.

## Verification and limits

The bounded Linux race gate passed48 integration roots /66 pass events and
12 task roots /34 pass events with real disposable PostgreSQL and mock peers.
No failures or skips occurred. All848 frozen source inputs matched after the
run; all ten main stand services retained their fingerprints. The test container
and PostgreSQL volume were removed after saving terminal receipts.

The gate covers native review launch and access revocation with Loom unavailable,
retaining an agent question across a bot restart, notification read-only links,
bounded/redacted context, immutable historical revision sequence, explicit-turn
routing, HTTP session/proof boundaries, existing notifications, structured menus
and the vertical recovery paths. All54 Node browser tests pass, including exact
notification targeting and uncertain-operation recovery priority.

Two initial review findings assumed workspace revisions mutate in place. Source
inspection disproved that assumption: prepare inserts a new UUID and revision;
only state changes in existing rows. The sequence assertion now verifies all five
returned historical revisions. No query change was made for those false positives.
Earlier compilation/lint failures and passing results remain distinct local files.

Evidence: ignored `.cache/shuttle-work/payment-review-entry-qa`, plus final Node
and lint outputs under `.cache/shuttle-work`. The full candidate is now installed
on the owned schema39 stand after a clean isolated compatibility probe and
image-only rollout. Real IAM owner reads pass; the actual Telegram command denies
a participant without reviewer rights. Native-only, agent-only and both pending
handoffs now pass in the real Telegram iframe with a manual provider. Embedded
critical paths, cross-person review, UI follow-ups and unattended model behavior
remain open. No production readiness is claimed.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
