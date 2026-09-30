# Synthetic provider delay controls

The optional Go fake hook runs only when R104_CONTROL_KEY is supplied. It must
remain outside the six managed runtime components. The product Telegram URL is
http://fake:8080; no Python forwarding proxy is used. Each stand's loopback UI and
control ports are recorded in allocations.json. No production configuration may
use this hook.

## Public control contract

- GET /control/state returns bounded state and journal events.
- POST /control/arm accepts positive numeric chat_id/message_id, text_sha256
  (SHA256 of exact decoded replacement UTF-8 text) and mode before_apply or
  after_apply_loss. Send X-R104-Control with the separate synthetic secret.
- POST /control/release accepts exactly one held request. An arm acknowledgement
  is not a hold; observe held_before_apply or applied_response_held first.
- There is no reset/rearm. Restart only the owned provider with a fresh case name
  and exclusive journal. Provider restart durability is outside this control.

Selection follows the actual fake mux, token check, bounded decoder and original
nonmutating destination/text admission. Nonmatching calls retain ordinary fake
behavior. before_apply holds before mutation/save. after_apply_loss holds only
an actual successful matching response, then deliberately loses that response.
No hook edits product attempts, receipts, queue state, source authority or grants.

The selected request survives caller/app death, subject to the separate provider
lifetime. Hold bound is 600 seconds; released mutation bound is 20 seconds. A live
positive control must release within the original application's request deadline.
Unknown remote work remains unknown. Journal failure/expiry invalidates evidence;
operator observations never manufacture product success.

## Resource and filesystem boundary

Data admission permits four connections before HTTP header parsing and four
handlers. The separate control listener reserves two connections, bounded header,
body and response deadlines, and a 1024-byte body. Real response capture is 2MiB;
the journal is bounded to 512 events of 512 bytes. One lifetime journal worker
owns writes/sync/close, with a one-second per-operation deadline; no retry worker.
Journal uses an absolute clean path, an opened parent directory and exclusive
basename creation. Payload text, keyboards and secrets do not enter the journal.
Selected mutex acquisition observes provider cancellation/invalidation without a
waiter goroutine. Ordinary requests retain the original mutation/save lifecycle.

## Required independent acceptance

Run each boundary separately in EN and RU through legitimate Telegram-like UI:
before-apply managed replacement with same-chat blocked and independent-chat
progress; after-apply lost response; current source revocation; live matched
positive response followed by the next same-chat operation. Capture actual card
IDs/text/buttons/edit counts, journal ordering, attempt/receipt/FIFO state and
old/new six-component plus database-session retirement. HTTP-only checks do not
replace UI acceptance. Never free an uncertain lane with SQL or blind retries.

Also prove real SQL contention: hold an owned SHARE table lock on bot.fake_state,
observe an ordinary save blocking with the actual mutation mutex, then release
selected work while preserving its original deadline. After selected failure,
release only the owned lock and prove no late selected mutation. A fresh positive
case releases contention before the deadline and completes exactly once. Local
mutex/listener/journal tests supplement this SQL/UI evidence, not replace it.

Native developer tests, race tests and lint precede fresh Git commit review.
No runtime/SQL/UI acceptance is claimed by this document.
