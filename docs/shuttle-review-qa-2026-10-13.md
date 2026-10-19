# Zinusya rename and full-code review — 2026-10-13

Project identity is zns-chatbot / Zinusya; Shuttle remains the rewrite codename.
All changes are local working-tree changes on codex/shuttle, not a new CI result.
No production or Loom developer checkout/stand was changed.

## Rename and certificate gate

Three fresh independent textual passes reviewed naming; the final passes also
covered reusable-root TLS. No substantive finding remained. Runtime service and
module IDs, administrator roles, project import path, telemetry and the linter's
local import prefix use the chosen names. Storage/queue keys remain compatible.

Actual IAM/Loom acceptance invoked modules.zinusya_administration.settings and
verified Zinusya's name and purpose in the model's script-tool description. Run
f482c56e-49c4-471f-b1f7-ec85156c0e0f completed. Repeated owned IAM provisioning
passes and avoids unchanged-grant updates. Initial wrong search endpoints and
the unchanged-grant 400 were found and corrected; they are not acceptance passes.

A reusable root signs the local wildcard leaf. Real TLS hostname verification
passed for host.docker.internal, auth.zinusya.localhost and bot.zinusya.localhost.
Browser trust import and interactive zitadeltg acceptance remain deferred until
Daniel is available. No browser warning was bypassed or trust installed silently.

## Full-code review loop

Pass 1 used three independent reviewers: correctness/security/recovery,
quality/readability, and API/render/deployment. Verified API findings were fixed:
subject lookup now includes bot and organization, and foreign-bot revocation
returns 404 without effect. Both behavioral HTTP tests failed on the original
code and passed after the narrow changes. Native Zitadel acceptance also passed.

Reserved question timeout and restart-only worker concurrency were already
specified in the operator guide; they were not treated as missing runtime
features. API schema/tool descriptions now repeat those limits. A misleading
message-promotion comment now describes the proven-no-effect requirement.

Pass 2 found a sequential deployment change could dispatch old queued work with
new bot credentials. Migration 00004 and startup now pin bot ID, IAM organization
and issuer before recovery/worker start. A retryable-delivery test rejected none
of the three changed configurations before the fix; after the fix each returns
the specific scope error, sends nothing extra and preserves the original retry.
Restoring the original deployment completes it. Pass 3 was independently clean.

The old second-instance fixture used another bot ID while expecting successful
restart against the same schema; it now models a second instance of the same
deployment. Separate changed-scope tests cover the newly enforced boundary.

## Verification

Full Linux Go race checks passed for all production packages. After the fixture
correction, the full integration race suite passed. Golden golangci-lint reports
zero issues; sqlc generated the scoped lookup/binding queries. The existing two
poller behavioral tests passed. Staged diff remained empty throughout review.

Owned-stand migration preserved messaging counts 2 users / 8 tasks / 8 outbox
rows. Guarded startup bound the existing schema. The release container refused
a fake foreign bot configuration, then the original configuration was restored.
Actual IAM account lookup and foreign revocation checks passed again. The guarded
image is the owned build represented by manifest-list digest
sha256:01944cba20eb5c5d2233e2c26104b49f50859b63e4154ffd9fc2583bdf2b91b1.
Private evidence remains in ignored .cache/shuttle-live-test.

These checks cover the current foundation. Registration/pass/payment/file
features, production cutover and concurrent external Loom control are not claimed.
Follow [the roadmap](shuttle-roadmap.md) and [deployment scope](shuttle-deployment-scope.md).

Written by Codex (gpt-6/Codex desktop), zns-chatbot developer
on behalf of Daniel Drizhuk
