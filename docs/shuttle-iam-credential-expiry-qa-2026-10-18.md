# Real IAM credential cache and expiry QA — 2026-10-18

Accepted for the current Go identity client against a new disposable
`zinusya-mock-credential-renewal` Zitadel instance. Real service and delegated user
tokens were reused during an outage, allowed as fallback only while valid,
rejected after expiry, and replaced after the same IAM container restarted.
This is client integration evidence, not a bot task/run recovery acceptance.

## Fixture and controls

The stand had nine resource-limited services, independent database/IAM/CA/fake bot
credentials, and pinned local schema49 / `mvp-56` bot, Loom and file-worker images.
A synthetic user501 opened a native session using signed mock initData and real
IAM. No administrative user grant or domain fixture was required.

The documented [OIDC settings API](https://help.zitadel.com/configure-oidc-token-lifetimes-in-zitadel)
accepts duration strings and requires all four settings. The driver read the
actual instance settings, retained them, set access/id token lifetimes to90s while
preserving both refresh settings, then read all four values back. The original
access/id lifetimes were43200s; refresh idle/absolute lifetimes were2592000s and
7776000s. These instance-wide changes affected only this disposable IAM.

A separately built Go1.27.2 helper called the repository's public identity.New,
ServiceToken, UserToken, Verify and CacheStats methods. It did not edit the cache,
sign tokens, alter host clocks or replace network responses. Tokens stayed in
process memory; evidence contains only SHA256 digests, expiry, fixture subject/actor,
counters and timestamps. A240-second context bounded the complete probe.

The helper ran as user10001 with read-only root/config/source/control mounts,
dropped capabilities, no new privileges,96MiB memory/swap, one CPU and32 PIDs.
Evidence used a separate writable bind. Root controlled lifecycle of the exact
owned IAM ID; helper file barriers were published atomically with exact LF bytes.

## Observed behavior

1. Service issuance and delegated user issuance succeeded online. Verify checked
   the real delegated JWT's fixture subject and actor. Both observed credential
   expiries were about89 seconds after the warm checkpoint, within0.1 seconds of
   each other.
2. Only the owned IAM container stopped. The same client returned exactly the same
   service/user credentials from warm cache, with zero renewal-fallback counters.
   A new client without cached discovery/credentials returned ErrUnavailable for
   both requests. This cold check also exercises missing discovery during outage.
3. About20 seconds before the later expiry, both requests still returned the
   original credentials after failed renewal. CacheStats recorded two service
   fallbacks and one user fallback: user delegation also requested its actor token.
4. After the later expiry plus one second, both credential requests returned
   ErrUnavailable instead of reusing expired values. Verify rejected the original
   signed delegated JWT with ErrDenied. No expiry or cached timestamp was fabricated.
5. The same IAM container was started and its real discovery readiness checked.
   The same identity client acquired new service/user tokens, each with a changed
   digest and later expiry. Verify again confirmed subject/actor for delegation.

The helper exited0 without OOM. All other eight services retained their IDs and
start epochs during the IAM cycle. No bot process restart was part of this gate.
The driver restored all four original OIDC settings and read them back, logged out
the native session, and deleted its temporary session credential file. There were
zero Telegram messages, zero bot tasks and zero Loom runs on the disposable stand.

## Review and retained evidence

Initial correctness/safety review found bad newline escaping, non-atomic file
publication and recovery coupled to successful completion. These were fixed before
any runtime fixture/settings mutation. A focused local serialization check then
caught Windows CRLF conversion; binary marker writing produced the exact expected
LF bytes. Failed checks and findings remain retained locally.

Fresh independent correctness and safety reviews of the corrected diff were clean.
One intermediate reviewer disclosed using read-only shell access despite the
no-commands instruction; a new no-history correctness reviewer repeated the review
with filesystem reads only and found no actionable issues. The trusted source gate
pinned fourteen source/module/helper-executable/configuration files before effects.
An independent recovery action can restore the exact IAM and original settings
without accepting a failed probe; that failure branch was source-reviewed, not
runtime fault-injected by this successful gate.

The accepted application database archive is296231 bytes; its SHA256 was retained
and rechecked. The archive, source gate, exact lifecycle journal, original manifest,
sanitized helper evidence, final mock transcript and terminal identities remain
ignored local artifacts. This is not an IAM backup or restore acceptance claim.

## Cleanup and limits

The one terminal helper and all nine stand services were removed after ownership
checks. No OOM occurred. Both owner-label and Compose-project queries found no
remaining containers, volumes or network. Generic ignored fixture configuration
and credentials remain; the per-user native session credential was removed.

A fresh primary-stand check matched112 raw fingerprints except River leader/queue
bookkeeping. Its bot/poller fingerprints and Loom run count were unchanged, and
task/outbox quiet checks passed. Production and the Loom developer's stand were
untouched. Nothing was staged, committed, pushed or deployed.

The [inbound positive introspection cache gate](shuttle-iam-positive-outage-qa-2026-10-18.md)
remains separate. This gate covers the real identity client and actual JWT expiry;
it does not prove revoked-token rejection while cached, bot restart with an empty
credential cache, concurrent renewal on real IAM, or combined Loom/IAM outage
recovery of an existing bot task/run. Those require their own bounded scenarios.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
