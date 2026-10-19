# Combined provider/API/IAM prelink acceptance

This owned fixture ran the actual candidate provider and bot images, PostgreSQL,
Traefik with verified local TLS, and the real `shuttle-live-test` Zitadel. The
Telegram issuer/JWKS and final IAM relay response were synthetic. No Telegram
message, browser login, real Telegram proof or Loom run was used.

Artifacts were the [recorded candidates](shuttle-prelink-images-qa-2026-10-16.md):
bot `sha256:2caee3da1ac4fa77d75b3e822438954d5c063b1a3faee717ecdb427897dbe06a`
and provider `sha256:867e7b02b84f8f6e5e928a441557ec8070600df6d90f469a7910c1fda3df5901`.
The fixture used a fresh schema33 database, separate named signing volume and
TLS gateway/network. A small protocol peer supplied only the synthetic Telegram
public JWKS, receipt-loss injection, final relay and unavailable Loom/Telegram
sink. Existing real-IAM credentials were used only by the application and the
owned-fixture setup/cleanup calls; no root CA private key was mounted.

## Passed behavior

- Invalid Telegram nonce returned401 before any prelink request, local account or
  final relay. The provider verified the signed synthetic Telegram proof itself.
- Provider-first login created one canonical local UUID and the same UUID in real
  IAM, then one explicit IdP link using the actual opaque subject, separately from
  numeric Telegram ID. The API's successful receipt response was deliberately
  discarded once. Provider retry used the identical assertion body; two requests
  produced one completed receipt, one account/link and one final relay.
- A second account first bootstrapped through the actual Mini App HMAC endpoint
  using synthetic initData. Provider login then resolved exactly that local/IAM
  account and added its explicit link. This is Mini-App-first API convergence,
  not Telegram UI registration or a bot/agent conversation acceptance.
- The final relay JWT's actual provider RS256 signature was checked against the
  public JWKS. Issuer, dedicated relay audience, opaque subject, reserved verified
  email and future expiry matched. No prelink-purpose claim was present there.
- A different opaque subject for the same numeric identity was denied with403,
  preserving the original link and withholding the final relay.
- Restarting actual bot and provider containers retained the canonical identity
  and provider JWKS/key. A new login succeeded for the same account. This is a
  completed-login restart check, not an interrupted callback recovery proof.
- Directly revoking the disposable local account made the next callback fail403
  without another final relay. This tests fresh local prelink authorization, not
  the administrative revocation tool or every token-cache invalidation path.

At the accepted observation there were two local users, three completed receipts
and one pending conflicting-intent receipt. Both bound IAM subjects equalled the
fresh local UUIDs. No extra link was accepted. The pending conflict was preserved
until disposal of the complete fixture database; no Down migration was forced.

## Isolation, failures and cleanup

Both synthetic numeric identities were confirmed absent by exact email/org
search before IAM mutation. The isolated IdP was never attached to a login policy;
automatic creation/update/email linking remained disabled. Deletion checked fresh
local UUID provenance and current IAM organization/email/link ownership.

Independent harness reviews fixed PostgreSQL startup permissions, insufficient
IAM cleanup ownership checks, GET/query handling of the real provider relay, and
resumable cleanup for successful and failed partial runs. The first native start
still failed: an `http.py` fixture shadowed Python's standard package, and the
fixture gateway's default UID could not read the leaf TLS key. Initial cleanup
also needed to handle an unmigrated database. These harness defects were fixed;
a fresh source review found no actionable findings before the second start.
The first attempt, errors and cleanup outcomes remain under ignored
`.cache/shuttle-live-test/prelink-combined-qa-attempt1/`; it is not relabeled PASS.

The second attempt passed. Bot/provider/gateway/peer/PostgreSQL all stopped with
exit0 and OOMfalse and were removed. Both real IAM fixture users and the new IdP
were deleted and fresh GETs confirmed absence. The exact labeled network/signing
volume were removed. All ten main stand container IDs/images/start times matched
before/after. Only the existing owned IAM received the explicit fixture mutations;
its audit history naturally remains. Main bot image/schema32/config were not
changed; the application prelink endpoint remains disabled there.

Ignored evidence is `.cache/shuttle-live-test/prelink-combined-qa/`. The reviewed
harness is `.cache/shuttle-live-test/prelink_combined_qa.py`, SHA256
`bd9dd06722a10d06f41e138b8bd74d3015927f0d123cc56cf962ff919d5c0456`. Its prepare/start/exercise/cleanup phases are separate. Preserve
recorded failures and reconcile the same instances after interruption; do not
blindly re-run start or overwrite an existing receipt. The checkpoint and bot
.env contain secrets and must not be published.

## Remaining gates

A later [resilience fixture](shuttle-prelink-resilience-qa-2026-10-16.md) adds
synchronized concurrent first logins and real IAM stop/recovery. Failure during
an accepted remote write remains separate; the original combined checks above
do not claim those additional scenarios. This stage also does not
establish real final IAM authorization/session-cookie relay, real Telegram Web
Login, browser trust or Mini App rendering/interaction. The later
[main bot image/schema33 cutover](shuttle-prelink-main-qa-2026-10-16.md) passed
with fresh backup/preservation/readiness checks and prelink still disabled;
main provider/backend activation remains open.
Production readiness/deployment is not accepted. Menu/form registration with
zero agent calls, full agent operation and mixed continuation is still required
and remains unimplemented/unaccepted as documented in the infrastructure plan.

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
