# Real Telegram acceptance, 2026-10-23

## Versions and scope

The isolated local testbot uses Shuttle source `035f2a6b`, image `bf1383e8`,
IAM4.20 and published ZitadelTG image `8864dd80`. The provider image does not
expose an exact source revision. Five local services run with a separate
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

## Provider OIDC: recovered completion; clean return still open

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

Clean local source `cabf96bf` lacks a submission latch in the shared init/auth
callback and re-enables login after one second. This explains the duplicate-form
defect, but does not yet prove the cause of interrupted return navigation or
exact source-to-image equivalence. [ZitadelTG issue12](https://github.com/complynx/zitadeltg/issues/12)
requests the narrow fix plus safe response-status/redirect-path diagnostics.
Acceptance still requires a real phone flow reaching the final callback without
manual IAM-page recovery. Increasing state TTL or disabling replay protection is
not an accepted fix.

## Other open acceptance gates

One real ASR provider request has already passed; its allowance is exhausted.
Real Telegram voice ingress and delivery remain separate from that adapter proof.
Daniel also requires durable full transcripts after temporary source expiry,
with no repeated ASR dispatch or charge for an already processed file. Accepted
Loom does not meet that new requirement: full text is a TTL artifact, bounded
step text is not full retention, and a new call has no completed-result lookup.
[Loom issue36](https://github.com/complynx/loom/issues/36) owns the correction and
local stub/counter checks. No additional paid ASR request is needed for them.

The real-API walkthrough, remaining transport acceptance and separately
authorized production steps remain open. Prior unchanged acceptance is reused.
Private tokens, initData, cookies, profile identifiers and auth-code responses
remain outside public evidence.
