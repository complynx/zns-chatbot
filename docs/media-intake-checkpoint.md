# Photo and document intake: acceptance scope

Current implementation is awaiting independent acceptance. Audio/video and video
range refinement are a separate in-progress worker integration.

- Photos and documents enter semantic interpretation. A selected receipt order or
  pending name is a hint; neither consumes the next uploaded file automatically.
- PNG/JPEG images reach the actual model as images, with their captions. Other
  document formats currently reach the agent as an unsupported-format descriptor;
  their contents are not claimed to have been read. Explicit purpose/order buttons
  can still submit the exact original file as a receipt.
- A clearly intended receipt with one exact current BYN/RUB amount match can be
  submitted for review. Duplicate amounts or uncertain amount/currency require a
  choice. A selected order from an earlier receipt button does not resolve a known
  duplicate amount by itself. An exact order ID in the current message is explicit
  selection. Server-side version and business checks run before attachment.
- Avatar requests truthfully report that avatar processing is unavailable. They
  do not become receipts. General questions leave unfinished attachment choices.
- Later model turns receive the pending question and current visible choices,
  plus recent authoritative media actions with manual/agent origin. Completed
  receipt notices point to the current order card instead of asserting an old
  payment state. Attachment bytes are not repeated on unrelated questions.
- Choice buttons bind the owner, attachment and order version. First choice wins;
  retries reuse interpretation and the immutable original bytes. A stale choice
  is rejected and requires a fresh request. OCR never approves payment.
- Separate attachment cards preserve the original message while showing current
  purpose/choice/outcome. New UI notices and buttons are available in EN/RU.
- Neutral bytes expire after 24 hours; completed proof copies remain in the
  existing authorized receipt store. Image bytes and extracted prose do not enter
  unrelated general model history.

## Public sandbox controls

`POST /lab/photo?user=101&filename=synthetic.png&caption=...` uploads raw PNG/JPEG.
`POST /lab/document` accepts the same parameters for arbitrary document bytes.
Both require the standard `X-Sandbox: 1` header. Captions are bounded to 1024
Unicode code points. The GUI provides file input, document/photo selection and
caption, then displays image preview/download and caption.

Deterministic model fixtures recognize `receipt 35.00 BYN` and `avatar` captions;
this is not vision validation. Independent image interpretation must use the
optional real `gpt-6-luna` Codex stand. See [stand launch](codex-model-provider.md)
and [FQA kit](../platform/scripts/fqa/README.md).

Core API: `POST /v1/media?filename=...` raw bytes; `GET /v1/media/{id}` metadata;
`GET /v1/media/{id}/file` bytes; `POST /v1/media/{id}/proof` proof-store copy only.
All use the authenticated owner. Copying into proof storage alone does not change
an order or approve payment. The order proof command enforces current access,
version, capacity and deadline for both manual and agent origins.

Receipt download uses the order resource, not the Telegram intake ID:
`GET /v1/order-events/{event}/orders/{order}/proof` returns metadata;
`GET /v1/order-events/{event}/orders/{order}/proof/file` returns original bytes.
Both require the authenticated owner or currently authorized payment reviewer.
File responses carry `X-Order-Version`, `X-Payment-Attempt`, `X-Proof-Id`,
attachment filename, `Cache-Control: no-store` and `X-Content-Type-Options: nosniff`.
Use the FQA client's `binary: true` option for byte equality. Displayed
`tg-media-*` IDs identify Telegram intake cards, not neutral API attachments.
