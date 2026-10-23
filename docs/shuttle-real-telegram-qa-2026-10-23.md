# Real Telegram acceptance, 2026-10-23

## Versions and scope

The Mini App and first two provider attempts used Shuttle source `035f2a6b`,
image `bf1383e8`, IAM4.20 and published ZitadelTG image `8864dd80`. That provider
image does not expose an exact source revision. Five local services run with a separate
organization and restricted application database role. Loom, polling and paid
model ingress remain disabled. No production change is implied.

## Telegram Mini App: accepted

The agent operated Daniel's real Telegram Web session and launched the Mini App
from the testbot menu. Actual Telegram-signed initData passed server-side
validation. Profile read and refresh worked. Changing Automatic to Russian,
saving and reopening retained Russian; changing back restored Automatic.
Current-session logout revoked that session. Closing and reopening the Mini App
signed in again with the same canonical identity and restored preference.

The final restricted-role observation found one linked user, four active
sessions and one revoked session; tasks, inbox and outbox were zero. The initial
observation already had three active sessions. These counts do not prove one
session per click or revocation of every session. Independent review was clean
for this bounded scope. This path does not prove separate provider OIDC.

## Historical provider OIDC: recovered completion before the fix

This section preserves the earlier issue12-open checkpoint. The fresh R4
acceptance below supersedes only its missing clean-phone-return condition.

A separate loopback HTTPS QA callback used one scoped authorization-code client,
fresh state/nonce and S256 PKCE. Root used the actual IAM/provider/Telegram UI;
Daniel confirmed Telegram authentication. This QA receiver is not a product
endpoint and does not change the product's server-side token acquisition.

The first attempt displayed `invalid login state`. Provider logs show an earlier
completed relay, then a later POST rejected as not pending. Identical state and
cookie values across those requests were not established. Root opened the
previously observed existing IAM auth-request page with a browser GET, without
resending Telegram credentials or starting another authorization. The original
callback completed with exit0. Signature, issuer, audience, nonce and the exact
existing canonical subject were verified; application aggregate counts remained
unchanged. Independent review accepted recovered OIDC completion only.

Daniel requested a second attempt. It reused the scoped client, with a new
state/nonce/PKCE and separate journal. After phone confirmation, the page remained
on `Waiting for Telegram...`. The live DOM contained two hidden auth POST forms
and an enabled button. One completed provider relay was observed, without a
prelink or token-validation failure; the QA callback was still awaiting a code
at the observation. No browser warning/error was captured. Two forms do not prove
two dispatched POSTs. The original failure and recovered result remain separate.

After preserving this observation, root resumed the same second IAM request with
a browser GET. Its original callback also completed with exit0, verified the
same canonical subject and token checks, and preserved application counts.
Independent review accepted recovered completion only. Both original callback
processes are terminal and must not be replayed.

Clean local source `cabf96bf` lacks a submission latch in the shared init/auth
callback and re-enables login after one second. This explains the duplicate-form
defect, but does not yet prove the cause of interrupted return navigation or
exact source-to-image equivalence. [ZitadelTG issue12](https://github.com/complynx/zitadeltg/issues/12)
requests the narrow fix plus safe response-status/redirect-path diagnostics.
At that historical checkpoint, acceptance still required a real phone flow
reaching the final callback without manual IAM-page recovery. Increasing state TTL or disabling replay protection is
not an accepted fix.

## Local fix and expired R3 handoff (historical)

[ZitadelTG PR13](https://github.com/complynx/zitadeltg/pull/13) provides source
`2f38d154df1e70a1a5c010f9c209007d0a2a1d82` and local image
`9274c19f7bac288c351766d8f0ca71885322f1f7fa161f95e9bc08475a48e0d3`.
Exact-revision test/integration CI and root's independent critical-source review
pass. Registry publication is not needed for this local acceptance.

The author's controlled Edge loopback check reproduces a blocked `303` form
redirect chain: one POST, zero callbacks, a `form-action` violation and
`ERR_ABORTED`. A committed same-origin continuation document instead reaches
the other-origin callback through GET, without a policy violation. SDK and
credentials are synthetic. Two earlier failed DNS/route fixtures remain recorded;
the successful loopback check does not prove a real phone flow.

Root replaced only the owned local provider. The old container stopped with
exit0 and its logs were retained privately. The new image revision and UID100/GID101
were checked; configuration, signing key/JWKS, network IDs, application counts and
four other service fingerprints remain unchanged. Original runtime metadata and
controllers are retained as history, not live handles.

A fresh callback uses the existing scoped client with new state/nonce/PKCE.
Its pre-execution review found a malformed helper call; it was fixed and the
failed review retained before a fresh clean review. The callback started
successfully and the new provider page was handed to Daniel at `Click to start`. Readiness was
announced before opening `/start`. At that handoff uninterrupted real phone acceptance remained
pending; no original Telegram POST was replayed and no manual recovery may be
counted as a clean pass.

No phone confirmation or auth POST was observed for that handoff. The ten-minute
provider form expired and root parked the browser on the safe completion page,
which reports verification incomplete. This attempt provides no real-phone
acceptance result and is not a new product failure. The subsequent R4 below uses a fresh valid flow; this expired original callback
must not be replayed.

## R4 real phone completion: accepted; issue12 closed

The fresh R4 used the reviewed local9274/source2f38, the existing scoped client
and new state/nonce/S256 PKCE. Daniel confirmed authentication on the real phone.
The original callback77621 completed with terminal exit0 and automatic safe
completion navigation. No manual IAM auth-request GET and no Telegram POST replay
were used. ID-token signature, issuer, audience and nonce passed; the subject is
the same existing canonical subject and application aggregate counts are unchanged.
The visible completion page reports OIDC verification complete. Independent
bounded evidence review is CLEAN (receipt hash prefix defcfcfb).

[PR13](https://github.com/complynx/zitadeltg/pull/13) merged as
03614cbf67019bd987d80bf71d5b38bca26d6b97; comparison with reviewed
2f38d154df1e70a1a5c010f9c209007d0a2a1d82 contains no changed source files.
Daniel's production provider is
complynx/zitadeltg@sha256:a7936858cb4c6d66591741983a74a4cd2c9a4a1efd0ca929e55c36011f28c231.
Its /usr/local/bin/zitadeltg SHA256 is
b6f2ccf16b3d4325f8363f2b28ebddd840c7cd1ff5d5c130d088c2970c9d0d14,
bit-for-bit equal to the reviewed local9274 executable. The production image
has no source revision label; binary equivalence and empty source comparison
provide the stated provenance, not an assertion that entire images are identical.
Selected read-only observation reports running/zero restarts. Root performed no
production mutation for these checks; Daniel had deployed the fix.

[Issue12](https://github.com/complynx/zitadeltg/issues/12#issuecomment-6371945946)
is closed for this bounded callback/navigation condition. This local real-phone
flow does not claim a separate exercised production login, statistical browser
reliability, real Telegram voice, ASR or business/model acceptance. Earlier R1/R2
recovered flows, R3 unconfirmed expiry and original8864 failures remain intact.

Sanitized evidence under .cache/shuttle-work/direct-production-20261023/:
zitadeltg-deployed-binary-equivalence.root.json,
deployed-fixes-local-identity.root.json, and
local-testbot/telegram-oidc-qa-r4/{real-oidc-result.root,evidence-review}.json.
No private token/code/subject was read for this documentation update.

## Other open acceptance gates

One real ASR provider request has already passed; its allowance is exhausted.
Real Telegram voice ingress and delivery remain separate from that adapter proof.
The separate durable-full-transcript/reuse requirement now passes on exact
deployed Loom6b04/APIe7a8/worker0f74 with schema006 applied. The bounded local
consumer proof covers full text after temporary-source/WAV GC and actual API
restart, distinct-run reuse without a second ASR dispatch/charge, and current
human revocation without leakage. Controlled Responses/local ASR and reused
unchanged developer evidence retain their limits; no extra paid request occurred.
[Loom issue36](https://github.com/complynx/loom/issues/36#issuecomment-6371973515)
is closed. See [audio QA](shuttle-audio-transcription-qa-2026-10-23.md) for exact
receipts, preserved N1/N2 failures and completed cleanup.

The real-API walkthrough, remaining transport acceptance and separately
authorized production steps remain open. Prior unchanged acceptance is reused.
Private tokens, initData, cookies, profile identifiers and auth-code responses
remain outside public evidence.
