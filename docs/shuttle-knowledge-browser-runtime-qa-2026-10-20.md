# Knowledge forms in a real browser, 2026-10-20

The isolated `kb-browser-r1` gate passes the native-only general-knowledge path
in headless Microsoft Edge with Loom stopped throughout all four browser phases.
It uses the deployed mvp83/schema67 HTML and JavaScript, actual isolated Zitadel
and mock Telegram. This closes the selected desktop form path; it does not close
all browser-agent handoffs, mobile usability or complete feature acceptance.

The candidate image is
`sha256:3614e37ae58ddf011429a2b01b6bf14b0c418730363591317d100bc63eb6c5f8`.
The nine-service temporary stand is archived and removed. Primary remains
mvp81/schema65, with no production change.

## Accepted behavior

Five synthetic identities and one named moderator are prepared through the
existing fixture APIs. Loom is then stopped gracefully. The remaining eight
services stay available. All following knowledge writes come from actual form
interactions, not direct API helpers:

1. The participant submits a synthetic article proposal. Before confirmation,
   the page reloads and restores the same operation ID, proposal and normalized
   arguments. Consent is unchecked again. Confirmation submits those original
   terms exactly once and returns a pending suggestion.
2. A distinct moderator opens a fresh browser context, reads the pending proposal
   and confirms approval through the form. One article and revision 1 are created.
3. The moderator uses another fresh context to edit that article. The preview
   contains the exact correction; confirmation targets the same article at
   revision 1 and produces revision 2. Current reads show the correction.
4. The participant opens a fourth context, reads the current correction, opens
   revision history and reads the unchanged original revision 1, then returns to
   current revision 2. The moderator edit control is hidden for this participant;
   this last phase sends no prepare or confirm requests.

There is one article, two revisions and three knowledge business operations
(suggest/review/edit), with zero Loom runs. Each write has one preparation and
one confirmation. Each browser signs out and closes its own context/process.
The four phases report no uncaught page errors. Screenshots are retained; the
participant's desktop full-page view was inspected. This is not mobile-layout
or general usability acceptance of the complete combined workspace.

## Isolation, evidence and limits

The existing browser fixture substitutes only Telegram SDK initData, signed for
the disposable bot. It permits the stand's HTTPS origin and blocks unrelated
requests. Certificate verification is relaxed only for that disposable browser
context; no CA is installed and no user browser configuration changes.

A passive route records and fsyncs each original prepare/confirm body before
forwarding through that origin guard. Real responses are retained before further
confirmation. No knowledge response is stubbed, and no request body is rewritten.
The Python owner records its Node child; a timeout targets only that live child's
process tree and retains an uncertain failed result, without replaying writes.
The happy path passes; forced timeout/process termination was not exercised here.

Ignored evidence under `knowledge-browser-runtime-v1/` includes per-phase
results, before-dispatch journals, four screenshots, `readback.json` and
`cleanup-proof.json`. All writers stop before final Telegram state/journal and
three fully decoded PostgreSQL dumps are exported. Containers, networks and
volumes are removed; primary preservation is checked. Source/controller review
closes in two passes, before runtime.

This is a desktop Edge test with synthetic identities and a mock Telegram SDK,
not real Telegram Web or separate physical devices. It covers general knowledge;
the [event runtime gate](shuttle-knowledge-event-runtime-qa-2026-10-20.md)
separately covers event scope and automatic archival through native HTTP and
controlled actual Loom calls. The [general content gate](shuttle-knowledge-content-runtime-qa-2026-10-20.md)
separately covers pending agent-consent graceful restart.

Those gates do not prove that Loom can adopt an already-prepared native action
with its original operation ID. That exact prepared-operation handoff remains an
explicit integration follow-up. No autonomous model-quality or full CI success
is claimed here. No factual community material is seeded.
