# Avatar transform foundation acceptance — 2026-10-19

This is source acceptance of the optional transform/leaf/API stage, with source
schema 54 and manifest `mvp-61`. The installed primary stand remains schema 49
and `mvp-56`. No candidate image was installed, no deployment was performed,
and no real Telegram messages were sent in this stage.

## Scope and result

The leaf supports frame-relative translation, uniform zoom and clockwise
rotation after EXIF orientation. Identity keeps the old renderer path and exact
JPEG digest `31a754361b05145895831fdd0e57e801a8442c7caff8127d116f3c59ea077728`
on the retained synthetic fixture. Explicit identity preserves operation replay.
Equivalent half-turns share a canonical request; changed transforms reject under
the same operation ID. The parent and database reject incorrect worker provenance.

All eight selected PostgreSQL integration roots passed on the final source:
three transform scenarios and five existing avatar delivery/ownership/recovery
scenarios. Each test uses an isolated schema on a separate bounded PostgreSQL.
The existing scenarios include uncertain Telegram delivery after restart and
source loss after commit. They use mock peers; they are not a real Telegram or
real Loom acceptance claim. The QA database dump and logs were retained before
its container and volume were removed.

Final Linux unit checks passed 59 roots: avatar 7, renderer 7, database 12, API 17
and task 16. The 25 database-dependent roots skipped without a DSN in this unit
command; they are not counted as PostgreSQL coverage. Windows avatar checks
passed 10 roots with four POSIX-only process fixtures skipped. Strict project
lint reported zero issues.

A separate Linux SDK probe built and ran the actual companion executable and
its parent adapter. It checked raw legacy stdin, edited framed stdin, explicit
identity framing, canonical provenance and invalid transform exit 2. Its JPEGs
were retained; the edited output was visually inspected. This is component QA,
not Mini App or user workflow acceptance.

## Review and retained failures

Two independent initial reviews examined correctness/security and code quality.
The quality review found aliasing when shrinking patterned photos. A focused
test reproduced it; a source-bounded Lanczos prefilter fixed it. A fresh final
review found no actionable defects. Initial lint findings were fixed. The first full Windows avatar command exposed
two POSIX fixture lookups made before their platform skips. Those guards were
corrected; Windows then passed 10 roots with four skips, and a Linux rerun passed
all 14 avatar/renderer roots without skips. A test-only compile typo during this
fix is retained as a failed attempt. The source gate reconciles these later
test-only edits with the unchanged production code from PostgreSQL/CLI checks.

The first CLI QA assertion incorrectly assumed a small transform must change
the digest of a photo whose visible region was uniform. Worker provenance was
valid; the probe changed to a shift exposing a different visible region and
passed. The original failure is retained separately. No product workaround was
made for this fixture error.

Ignored evidence lives under `.cache/shuttle-work/avatar-transform-v1-qa/`:
`pg-v2/checkpoint.json`, `linux-unit-v2-checkpoint.json`,
`windows-unit-v4.jsonl`, `lint-v3.log`, `lint-v5.log`, `cli-v2-checkpoint.json` and the source
gate. Earlier failed and superseded attempts remain separate. Own SDK and
PostgreSQL containers were removed without OOM. Primary images, configuration
and domain state were preserved after the user-reported reboot.

## Remaining work

This stage has no native editing UI, persistent edit draft, durable original
upload or task-free avatar delivery. Existing generation still requires an
active owned agent run. Next, share owner-scoped sources/drafts across agent
and native entry points and add a durable native send path. Accept native-only
with Loom stopped, agent-only and both direction changes before claiming full
legacy editor parity. Retention and deletion policy remain separate decisions.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
