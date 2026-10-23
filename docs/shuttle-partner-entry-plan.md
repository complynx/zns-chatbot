# Partner code links — bounded implementation plan

Architecture and Go consultations agreed to reuse directed invitations and the
existing own-account UUID. The useful outcome is: a user obtains a shareable link,
another user opens it, and explicit invitation/acceptance proceed through the
existing consent flow. Opening alone has no registration, invitation, hold, event
choice or financial effect. This is a contact code, not an already-created invite.

The person opening the link can become the inviter after choosing an event and
explicitly requesting that action following the allocation-hold explanation. The
code's owner is the intended target and must subsequently accept. No inference is
made that the owner actually shared the link; another person can construct it.

## Selected implementation

- Owner-only `zinusya_pairs.partner_code` returns the caller's existing UUID and
  optional `share_url`; it takes no target argument and exposes no other profile.
- Fetch `getMe` on demand with a short timeout, verifying configured BotID,
  `is_bot` and a bounded safe username before constructing a fixed `https://t.me/`
  URL. A failed/unverified lookup returns the plain code without an invented URL.
  This avoids a new startup setting, cache or Telegram startup dependency. The
  tradeoff is an extra Telegram read when someone requests a share link.
- Use `start=partner_<32 lowercase UUID hex characters>`. Telegram's
  [documented deep-link contract](https://core.telegram.org/bots/features#deep-linking)
  delivers this as `/start <payload>` in a private chat. The payload is 40
  characters, within the documented 64-character limit.
- Parse only an exact private text command, with no attachment caption, extra
  tokens or noncanonical/nil UUID. Preserve text/raw update and authenticated sender.
  Store a small optional reference in the already-durable normalized task JSON.
  Existing tasks without it remain valid; no schema migration is required.
- Append the reference as explicitly untrusted addressing context in the existing
  checkpointed Loom prompt, for both new runs and follow-up inputs. It does not
  authorize a name update, choose an event, authenticate another user or consent
  to any pair action. Ask for explicit event/invitation intent and refresh current
  invitation context before reusing the existing write.
- Preserve raw UUID sharing and every existing eligibility, operation receipt,
  hold, notification and recipient-only acceptance check.

No directory, username/TG-ID search, target profile lookup, bearer capability,
open-invitation aggregate, automatic registration, allocator or financial policy
is introduced. Stable links are public addressing information, not expiring secrets.
Old links may stop opening the bot if its Telegram username changes.

## Acceptance

Behavior checks must prove owner-only code reads and unknown target-field rejection;
verified bot URL generation and fallback without forged URLs; strict command parsing;
authenticated sender preservation; zero business effects from link intake; and
durable prompt context across duplicate intake, IAM/Loom outage, uncertain create,
restart and active-run steering/question input. Reuse the current test harness.

Complete the explicit invitation/target acceptance path and prove unrelated users
cannot accept.
Owned real IAM/Loom acceptance and an actual Telegram Web link opening must be
reported separately; a synthetic webhook or standalone Loom run cannot prove
client behavior.
Existing CA/WebView and financial-policy questions remain deferred.
