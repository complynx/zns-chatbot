# Functional QA kit

Use public contracts, not product implementation, for acceptance. This kit removes
transport/setup boilerplate; QA owns expectations and decides what passes.
Get exclusive sandbox writer ownership before mutations. Do not rebuild during QA.
All identities, credentials, receipts and payment data here are synthetic.

From `platform`, after `npm ci` and starting Compose with `compose.qa.yaml`:

```sh
node scripts/fqa/cli.mjs doctor
node scripts/fqa/cli.mjs state alice
node scripts/fqa/cli.mjs orders alice
node --test scripts/fqa/client.test.mjs
python -m unittest discover -s scripts/fqa -p 'test_*.py'
```

`doctor` checks Docker access, fake Telegram and authenticated Core access before
fixture creation. Default addresses are literal loopback 8090/8091. Windows uses
Docker `desktop-linux`; `new Fixtures({context: 'name'})` selects another context.
The browser uses installed Edge with `BROWSER_CHANNEL=msedge`, or Playwright's
Chromium by default. Install it with `npx playwright install chromium` if needed.
No production endpoints, redirects or real Telegram messages are supported.

For an isolated stand with a different disposable signing key, pass
`{ui, api, signingKey}` to `new SandboxClient(options)` or
`QARun.create(name, options)`. Use that stand's `ZNS_AUTH__SIGNING_KEY` value.
The key stays out of request evidence; literal-loopback and redirect restrictions
still apply. This option is only for synthetic stand credentials.

## Own fixtures and evidence

```js
import { QARun } from './scripts/fqa/run.mjs';
import { operationKey } from './scripts/fqa/client.mjs';
const run = await QARun.create('payments-review-1');
const order = await run.createOrder('alice', { extras: { preparty: 0 } });
const response = await run.client.action('alice', {
  event_id: order.event_id,
  order_id: order.id,
  version: order.version,
  name: 'cash',
  payment_admin: 'bob',
  origin: 'manual',
  key: operationKey(),
});
await run.save('cash-result.json', response);
await run.cleanup();
```

Artifacts live in `platform/test-results/fqa/<run-name>/`: manifest, UTF-8 request
and response journal, cleanup results, caller evidence, screenshots and traces.
Names are plain filenames. Runs cannot overwrite an existing manifest. Credentials
are not logged. Evidence contains synthetic user data; keep artifacts local.
Run one operation at a time per manifest. Creation intent is persisted before
the request; resume recovers a lost response by replaying the same operation key.
Definitively rejected creation is recorded and skipped during cleanup; only
ambiguous failures remain recoverable. Cleanup does not retry a rejected creation.
Use a new key for a distinct action; retain the original command/key for retries.

Public library API (all asynchronous methods return promises):

| API                                                    | Contract                                                                                                |
| ------------------------------------------------------ | ------------------------------------------------------------------------------------------------------- |
| `QARun.create(runName, options?)`                      | Create a new journal under `test-results/fqa/runName`.                                                  |
| `QARun.resume(runName, options?)`                      | Load an existing journal; returns a `QARun`.                                                            |
| Run `options`                                          | `{ui?, api?}`: literal loopback HTTP origins. Defaults 8090/8091.                                       |
| `run.createOrder(subject?, choice?, event?)`           | Create and journal an owned fixture; defaults `alice`, `{}`, `sandbox-festival`. Returns current order. |
| `run.cleanup({cancelProofs?: boolean}?)`               | Return cleanup outcomes; retain paid orders and, by default, proofs.                                    |
| `run.save(filename, value)` / `run.artifact(filename)` | Write JSON / return artifact path. Plain filenames only.                                                |
| `client.request(surface, route, options?)`             | Surface `api` or `ui`; route starts `/`. Returns `{status, headers, body}` even for HTTP errors.        |
| Request `options`                                      | `{subject?, method?, body?, binary?}`; defaults `alice`, `GET`. Body is JSON.                           |
| Response `body`                                        | JSON for JSON content types; otherwise UTF-8 string, or `Buffer` when `binary: true`.                   |
| `client.json(surface, route, options?)`                | Same arguments, but throws for non-2xx and returns body directly.                                       |
| `client.state(subject?)`                               | Fake Telegram messages/history and processed cursor.                                                    |
| `client.orders(subject?, event?)`                      | Collect all public API pages; no assumed first-page limit.                                              |
| `client.order(subject, event, id)`                     | Read one current owned order.                                                                           |
| `client.action(subject, command)`                      | Exact JSON command to `/v1/order-actions`; returns status/headers/body.                                 |
| `client.processed(subject, updateID)`                  | Wait for handling of this update; return observed state.                                                |

Subjects: `alice`, `bob`, `visitor`. Network/timeout/JSON parsing failures throw;
they are not HTTP denials. No mutation retries are hidden in the client.

```sh
node scripts/fqa/cli.mjs cleanup payments-review-1
node scripts/fqa/cli.mjs cleanup payments-review-1 --cancel-proofs
```

Cleanup touches only orders created through that run. It re-reads current state,
uses version/attempt-bound actions and preserves paid orders. Proofs remain unless
`--cancel-proofs` is explicit. Missing orders are harmless. Locked/revoked/stale
operations fail visibly. Never reset unrelated records or reservations. No bulk
database delete exists. Preserve the manifest if cleanup is interrupted.

## Manual Telegram, mouse and touch

```js
import { openTelegram } from './scripts/fqa/browser.mjs';
let ui;
try {
  ui = await openTelegram(run, { subject: 'alice', touch: true });
  const update = await ui.send('/orders');
  const state = await run.client.state('alice');
  await run.save('orders-state.json', { update, state });
  // Scope repeated button labels to the exact message/card, not the whole chat.
  // await ui.act(() => ui.click(ui.card(messageID).getByRole('button', {name: '...'})));
  // await ui.download(actualAttachmentLink, 'orders.xlsx');
} finally {
  try {
    await ui?.close();
  } finally {
    await run.cleanup();
  }
}
```

`openTelegram(run, {subject?, touch?, channel?, headless?})` returns a driver:
`page` is a Playwright `Page`; `click(locator)` uses click or tap; `card(messageID)`
returns a `Locator`; `send(text)` and `act(asyncAction)` return the accepted
Telegram update. `download(linkLocator, filename)` clicks a real attachment and
returns `{filename: suggestedName, path: savedPath}`. `close(label?)` saves the
screenshot, browser errors and trace, then closes the browser. Startup failures
close the browser too. Keep fixture creation inside the outer cleanup scope.

`act` observes the actual browser `/lab/input` response and waits for
`processed_cursor > update_id`. The cursor advances after bot handling completes;
it is not a count of messages or edits. `history` exposes stable `id` and
`update_id` for correlation. Identical repeated denials therefore have distinct
completion evidence. UI polling may lag: additionally wait for the expected DOM
with Playwright locators. `act` handles text/callback inputs; for uploads, Mini App
saves and contacts use their actual responses and explicit state/DOM predicates.
`waitFor(sample, predicate, {label})` provides bounded polling and includes the
last sample in timeout errors. It does not swallow errors or retry mutations.

For Telegram language initialization, `POST /lab/input` accepts an optional
`language_code` string (at most 64 bytes), for example
`{user: 101, text: '/language', language_code: 'ua'}`. Use the toolkit UI request
with its `X-Sandbox` header. It becomes the update sender's Telegram language;
it must not overwrite an existing explicit preference. Start with an empty
preference fixture to exercise initialization. This field is a local transport
fixture, not a production API identity claim.

Run mouse and emulated touch separately. Browser traces, console errors and
screenshots are captured at `close`, including failed scenarios. Touch emulation
does not prove physical-device behavior. HTTP-only success is not manual UI proof.

## Specialized fixture controls

```js
import { Fixtures } from './scripts/fqa/fixture.mjs';
const fixtures = new Fixtures();
fixtures.preflight();
try {
  fixtures.apply('export-fixture', { EXPORT_FIXTURE_ADMIN_ENABLED: 'false' });
  // Assert revoked access through public API and UI.
} finally {
  fixtures.apply('export-fixture', { EXPORT_FIXTURE_ADMIN_ENABLED: 'true' });
}
```

Use existing public `fixture` and `export-fixture` controls documented in
`platform/README.md`: age, deadline, capacity, administrator country/membership,
historical saved choices, isolated oversized batches. Values pass as environment
strings, not shell code, preserving Unicode JSON. Inherited fixture variables
are removed. Unsupported variable names fail before Docker. Record original
settings and restoration commands in evidence before changing global fixtures;
`finally` cannot restore after process termination. Paid evidence stays retained.
`fixtures.restart(['bot','fake'])` restarts without rebuilding or deleting data.

Payment action names: `cash`, `proof`, `country`, `accept`, `reject`,
`cancel_proof`; all existing-order actions require current `version`;
`country`, `accept`, `reject`, `cancel_proof` also require current `attempt`.
`proof` requires an uploaded owner-bound proof ID. The client never repairs stale
commands automatically: intentional negative tests must stay negative.

## Independent XLSX evidence inspection

```sh
python scripts/fqa/inspect_workbook.py test-results/fqa/run/orders.xlsx > workbook.json
```

Standard-library ZIP/XML inspection, independent of the product's Excelize writer.
Emits sparse cell coordinates, types, text, formulas, sheets, panes and filters;
resolves worksheet relationships and shared/inline strings. No formula evaluation,
Excel display formatting or Strict OOXML. Unsafe/excessive packages are rejected.
Use decimal arithmetic for monetary expectations. A JSON API error is not an XLSX;
negative requests remain structured responses through `client.request`.

Next stand improvements, not yet provided: fault targeting by Telegram method and
recipient (current 429 is next-request), crash barriers at send/commit boundaries,
and real test Zitadel. Request these when acceptance depends on them; skipped
scenarios are not passes. RU/EN product acceptance remains mandatory.
