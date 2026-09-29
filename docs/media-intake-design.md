# Agent-mediated media intake: bounded next stage

Repository inspection: 2026-09-25. Design only; no product changes, stand changes,
provider calls, or Functional QA report reads. References identify the current
implementation, not accepted media support.

## Current behavior and reusable seams

| Area | Repository evidence | Implication |
| --- | --- | --- |
| Telegram input | `platform/internal/telegram/telegram.go:35` defines `Message` with document and text, but no photo sizes or caption. `internal/bot/bot.go`, `parseUpdate` and `Handle`, classify documents as manual and call `handleProofDocument` before the model. | A document bypasses semantic interpretation. Photo and caption support must be added explicitly. The private-chat/known-owner checks already provide the ingress identity boundary. |
| Manual proof selection | `internal/bot/proofs.go`, `selectProofOrder`, validates current owner/order/version and unpaid/cash state. `submitProofDocument` copies `bot.proof_pending` into a per-update submission; absence means “select an order first.” | Existing pending selection is currently a routing instruction. Keep it as a hint and preserve the retry snapshot mechanism; do not automatically consume the next file as a receipt. |
| Safe Telegram download | `internal/telegram/documents.go`, `Client.Download`, resolves an opaque file ID through `getFile`, bounds metadata and actual body to 20 MiB, restricts path components, refuses redirects, and bounds timeout. | Reuse for document bytes and a normalized selected photo size. Never send the bot-token download URL to a model. This is the repository's limit, not a verified current Telegram service limit. |
| Immutable evidence | `internal/orders/proofs.go`, `UploadProof`, checks `can_book`, filename and byte limits; hashes owner, filename and bytes; inserts without overwrite. Migration `005_proofs.sql` owns `core.order_proofs`; `006_drop_proof_sources.sql` removes mutable message sources. | Preserve exact source bytes for review. Classification must not replace a stored receipt with OCR text or a recompressed image. |
| Evidence access | `OrderProof` joins the current order to its owner's proof and permits the owner or an authorized current event payment admin, in proof/paid state. `internal/api/proofs.go` serves bytes with no-store/nosniff; `internal/bot/proof_client.go` checks size and version/attempt headers. | Reuse current-order authorization for receipt review. There is no general public file-ID download API to expose to the model. |
| Proof transition | `internal/orders/service.go`, `Execute`, `authorize`, `loadOrder`, `startPayment`, enforce owner/can_book, version, event deadline, capacity, owned proof and transaction/idempotency. `finishPayment` separately handles admin acceptance/rejection and attempts. | Attachment is not payment acceptance. **Current `Command.validate` rejects agent-origin proof with `human_action_required`.** The stage must intentionally allow agent-origin proof under the same in-scope business checks, not disguise it as manual. |
| Amounts | `internal/orders/catalog.go` uses integer BYN cents. `internal/orders/instructions.go` derives the displayed RUB amount at its configured code ratio. | Use the same current authoritative payment amounts, currency and version for matching. Do not introduce floating-point matching or a separately guessed conversion rate. |
| Agent contract | `internal/agent/agent.go` has text/state/history and a 60,000-byte serialized input bound. `openai.go` sends a textual JSON user message to Responses with a strict proposal schema; no image/file content parts are built. | Adding base64 to today's JSON input is not multimodal support and would exceed the existing bound. Media transport and interpretation need explicit adapter work. |
| Codex stand | `internal/agent/codex.go` is synthetic-only, sends JSON on stdin, uses an ephemeral temporary directory and a schema, and disables tools including `view_image`. `scripts/model-codex.mjs` runs the native optional stand. | Current Codex integration cannot demonstrate visual classification. Do not infer visual support from the model name or CLI availability; keep the synthetic-only guard. |
| Functional UI | `internal/sandbox/documents.go`, `index.html` document form, and `app.js` provide file upload/download and document message rendering; no photo/caption message shape exists. | Extend this actual Telegram-like flow for photos, captions and clarification callbacks. An HTTP-only receipt test cannot accept this stage. |
| Avatar | No avatar storage, mutation or rendering implementation was found under `platform/internal`. | Classification may recognize an avatar request, but must say avatar saving is unavailable. It must not claim to set one or silently submit the image as a receipt. |

Paths in the table after the first row are relative to `platform/`.

## Smallest coherent behavior

1. Normalize text, photo and document updates into one owner-authenticated input.
   Pass caption and media to the agent before choosing a business action. A pending
   proof selection or profile question is context only. Preserve commands and
   typed callback actions as explicit requests.
2. Download once with existing safeguards. Validate content type from bytes and
   select supported image/document decoding paths; enforce pixel/page/input-size
   bounds before invoking a provider. Unsupported or unreadable files get a
   localized explanation or clarification, not a fabricated classification.
3. Ask for a typed proposal: receipt, avatar request, other, or clarification.
   Include only current authorized candidate orders and pending hints. Treat
   instructions visible inside an attachment as untrusted content. The model
   cannot supply an owner, invent a candidate order, invoke an arbitrary URL,
   accept a payment, or claim a write succeeded.
4. For a clearly intended receipt, use an explicit user-selected eligible order,
   or a unique reliable amount-and-currency match among current eligible own
   orders. Missing currency, uncertain amount, multiple totals, duplicate prices,
   or absent match requires clarification. A model confidence number alone is
   insufficient evidence. A single candidate does not turn an unrelated portrait
   into a receipt. A changed amount/version invalidates the prior match.
5. Render owner-bound server-created buttons when the attachment's purpose or
   target remains ambiguous: available eligible orders, “not a receipt,” and
   cancel as appropriate. Each button binds the immutable attachment, question
   generation and order version; the model does not emit callback data. A new
   question, retry, timeout or stale callback cannot attach another upload.
6. Commit the selected original bytes through `UploadProof` and a version-bound
   proof command. Recheck authorization/state/deadline/capacity at execution.
   Report only the actual API outcome. Leave the existing payment-admin review
   and accept/reject path separate; OCR never marks an order paid.

An unrelated question must be answered without consuming a pending attachment
choice. A later answer can resume that choice. If several unresolved attachments
exist, identify the attachment explicitly or render its buttons rather than use
an implicit “latest file.” Restart must preserve the same question and evidence.

## Required implementation boundary

- **Telegram and stand:** add caption and photo-size fields; route every free
  text/media update through the agent; extend fake upload/message rendering to
  exercise the same photo/file/caption distinction. Localize all new prose in
  the EN/RU catalogs and remove receipt-only labeling from generic upload UI.
- **One neutral media record:** add a Core-owned immutable pending-media table
  for owner, opaque ID, original bytes, detected type, safe filename and receipt
  time/expiry. A receipt-only table is not an honest staging location for an
  undecided portrait. Enforce the existing booking permission for the in-scope
  intake. No public download URL; authorized service-side resolution supplies
  bytes to the adapter. Keep binary bodies out of generic history and model-plan
  logs. Expiry must clean abandoned bytes without deleting attached receipts.
- **One persisted clarification record:** a Bot table binds owner, originating
  update, media ID, question generation, candidate IDs/versions, deadline and
  resolved result. Reuse `orderButton` token storage and the established
  per-update retry pattern where they fit; do not add a general workflow engine.
  Cache the typed interpretation before execution so delivery retries do not
  reinterpret a file. Callback replay returns the current outcome.
- **Narrow API addition:** owner-scoped upload of neutral media, authenticated
  internal byte resolution for classification, and promotion into existing
  immutable proof storage. Validate ownership during promotion. Proof command
  still passes through `orders.Execute`; broaden only the in-scope agent-origin
  proof allowance and keep existing review authorization. Retry after promotion
  must reuse the proof ID, never duplicate the order transition.
- **Model adapters:** introduce a bounded attachment descriptor/content path and
  typed media proposal in `Input`/`Plan`/validation/strict schemas. Keep compact
  redacted history separate from current attachment content. Extend both Remote
  transport and the real OpenAI adapter. Codex synthetic visual verification is
  a separate adapter capability check, not a fallback that silently drops media.
  Report unavailable visual interpretation truthfully.
- **Amount matching:** factor a shared current payment-amount projection from
  existing order/instructions code; expose only authorized eligible candidates.
  Re-fetch before applying the proposal. No speculative OCR payment verifier,
  avatar module, general file browser, or additional provider abstraction.

## Official verification required before adapter implementation

This investigation inspected repository code only. It does **not** establish
current provider/Telegram capabilities. Before implementing transport, verify
against official documentation and the installed CLI: Responses image and PDF
input content forms, supported model/media combinations, size/page limits and
retention behavior; Telegram photo/caption/getFile semantics and actual limits;
and Codex noninteractive image attachment support under this isolated configuration.
Choose supported formats after that check. Do not advertise arbitrary office-file,
audio, video, PDF or Codex vision support based on this design.

## Acceptance for one fixed build

- In EN and RU, an unsolicited receipt photo/document reaches interpretation,
  attaches to a uniquely matched authorized order, and remains only under review.
  Downloaded admin evidence equals the original bytes.
- A portrait sent while a receipt question is pending asks its purpose or states
  avatar saving is unavailable. It never becomes proof through pending context.
  A receipt sent during a name question does not overwrite the name.
- Two equal-price orders, unclear currency/amount, contradictory caption, and
  unreadable/unsupported media produce honest clarification without mutation.
  Typed buttons show current order amounts and target the intended attachment.
- Interrupt with a general question, resume later, restart, resend the same
  update, and race two answers. One intended transition occurs; no other pending
  work is consumed. Expired/forged/cross-owner buttons cannot write or expose bytes.
- Change an order/version/permission between classification and execution.
  Core rejects the stale or denied write; UI refreshes actual state. Manual and
  agent-origin proof submissions obey the same in-scope constraints.
- No receipt bytes, OCR text, passport/name values or token-bearing file URLs
  appear in unrelated model history/application logs. A receipt's embedded
  instruction cannot authorize payment acceptance or select another owner.
- Use real PostgreSQL transaction tests plus the Telegram-like photo/file/
  caption GUI and both fresh independent QA gates. Provider-backed visual cases
  must run on the actual supported adapter; scripted classification alone is
  insufficient. Skipped provider cases remain unaccepted.
