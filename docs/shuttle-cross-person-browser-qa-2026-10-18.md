# Cross-person native payment review: browser QA

The owned isolated stand passed native payment review with separate payer and
ambassador accounts on schema49 / `mvp-56`, using the current bot image
`sha256:46a9abfd65e3f7d9138f7e106a640997551fa5f191bbff428c3b1486c32c03eb`.
Actual shipped HTML/JS/CSS, real local Zitadel, Loom and PostgreSQL were used.
Telegram transport and its SDK were synthetic; the file task used a manually
answered provider request. This is browser/backend proof, not Telegram WebView,
unattended model or production acceptance.

## Scope and observed behavior

The disposable `zinusya-mock-browser-review` project had its own nine services,
credentials, CA, databases, mailbox and loopback ports21510..21515. Three synthetic
users were bootstrapped normally. Only the fixture administrator received a global
IAM role. The payer received an assigned unpaid123.45 BYN pass; the separate
reviewer received ambassador authority for this event through the normal exact
proposal/confirmation API. The reviewer had no participant pass or global role.

A synthetic PDF was registered with mock Telegram, accepted through normal bot
intake and completed through real Loom with a manual response that called no
business tools. The payer submitted its actual accepted file reference as pending
30.87 BYN initial payment evidence. The form did not invent a file locator.

A fresh headless Edge155.0.4283.45 process used two isolated browser contexts.
Only the exact Telegram SDK request was fulfilled with a fixture exposing genuine
signed synthetic initData. Page traffic was restricted to the stand's localhost
HTTPS origin; the application assets and native financial APIs were unchanged.
TLS verification was relaxed only in these disposable contexts, without installing
browser or system trust. The browser was closed after each attempt.

- The payer discovered no reviewer events. Known evidence detail and attachment
  routes returned403; confirmation using the reviewer's exact known proposal and
  access to its completed operation returned404. The payer's confirm control was
  disabled. A hidden operation was not interpreted as a financial failure.
- The separate ambassador saw the payer's name and event-local Europe/Minsk times.
  Loading the protected attachment returned bytes with the original PDF SHA-256.
  Native preparation left confirmed money at zero.
- One explicit native confirmation reached the server successfully. The browser
  fixture deliberately discarded its successful response. The form retained the
  uncertain request rather than sending a replacement operation.
- A real page reload recovered the original operation receipt and the same
  completed workspace. There was exactly one reviewer confirmation POST and no
  retry POST during recovery. Completed controls were disabled.
- Independent SQL checks bound the single persisted review to this evidence,
  payer/pass, reviewer ID/subject, ambassador authority, confirmed decision,
  amount3087 and exact reason. The one completed native commit matched the
  browser's operation UUID, workspace, actor, source and original result. Only
  that workspace was completed. The authorized payer balance read reported
  confirmed3087, remaining9258 and deposit satisfied; the pass was not fully paid.

The one file-ingestion task/run was the baseline before browser work. Native
review created no further task or Loom run. All nine disposable service epochs
remained unchanged throughout the business scenario. This does not separately
prove this new cross-person case with Loom stopped.

## Retained attempts and review

The first QA assertion expected403 for a foreign confirmation; the API correctly
hid that workspace with404. Zero reviews existed at that point. Reopening a new
browser context with consumed initData also failed; new contexts require fresh
signed launches and their own nonce. A duplicate Windows/slash hash-map key and
read-only observer query/envelope errors were corrected separately. Failed
observations were retained and never counted as accepted runs or product bugs.

The first prepared decision expired at13:13:59.881063Z during QA review. The
native form explicitly prepared revision2 at13:15:01.665862Z using the current CAS.
Revision1 remains historical and uncredited. Lost-response recovery above refers
to revision2, not a claim that the first preparation completed. Confirmation was
not replayed while correcting the final read-only balance observer.

Fresh independent correctness and safety reviews accepted the browser/helper
slice. A later reviewer requested stronger independent persistence assertions;
those were added and a fresh final review found no actionable findings. Node
syntax checks and the actual browser gate passed. No Go source, schema,
dependency, production configuration or primary runtime changed. After acceptance,
only the helper TLS comment was clarified; the retained accepted copy and hash
delta prove all noncomment lines are equal.

Evidence is under ignored `.cache/shuttle-work/cross-person-browser-v1-qa/`:
`checkpoint.json`, `browser-result.json`, `workspace-timeline.json`,
`source-gate.json`, two screenshots, retained failures, private runtime snapshots,
logs and archives. Signed launches and tokens remain private. The standalone
fixture driver is `.cache/shuttle-work/cross_person_browser_v1.py`; the accepted
browser script is `run-v3.cjs`. `browser.cjs` is reusable test-only infrastructure.

## Preservation and cleanup

Both custom-format database archives have readable headers/TOCs; no database
restore was exercised. App archive308753 bytes has SHA-256
`7535d9044e77f9fb0c09178af0b6bf17f2d0e6a537cdf7b6080a84dbea7d5bda`;
Loom archive92286 bytes has SHA-256
`0de026a22bdf5e4a15490283a8c3e8bfe281e39197106dee45d67384c161f48e`.
The exact temporary project has zero remaining containers, volumes and networks.

The primary bot/poller, companion epochs, configuration and source guard passed.
Of112 primary raw fingerprints, only River leader/queue bookkeeping changed;
all46 River rows and canonical queue fields remained equal. The primary retained
132 Loom runs. No real Telegram message, production action, commit or push occurred.

Next: use this isolated browser boundary for the still-open order reviewer
agent-to-native confirmation, with distinct customer/reviewer identities. That
order gate, real WebView trust/launch and deferred zitadeltg acceptance remain open.

Written by Codex (gpt-6.1-sol/Codex desktop), zns-chatbot (Shuttle) developer
on behalf of Daniel Drizhuk
