# Media provider capability check

Verified 2026-09-25 against fetched official documentation and installed CLI help.
This is transport research, not acceptance of the application's media behavior.
No credentials were inspected and no stand or database was changed. One synthetic
Codex image inference passed. The requested model remains `gpt-6-luna`.

## Responses transport

The official model page explicitly lists image input, text input/output, and
structured outputs for `gpt-6-luna`. Audio/video input is not supported. This
establishes documented modality support, not this account's access or receipt
recognition accuracy. [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)

Use a user message with separate `input_text` and `input_image` content parts.
Images accept an HTTPS URL, an inline base64 data URL in `image_url`, or a Files API
`file_id`. Supported encodings are PNG, JPEG, WEBP, and non-animated GIF. The
current guide lists a 512 MB request payload ceiling and 1,500 images; model/detail
resizing and patch budgets also apply. These are provider ceilings, not sensible
application budgets. [Images and vision](https://developers.openai.com/api/docs/guides/images-vision)

Recommended minimal request shape; retain the existing strict proposal schema:

```json
{
  "model": "gpt-6-luna",
  "store": false,
  "input": [{
    "role": "user",
    "content": [
      {"type": "input_text", "text": "Caption, authorized candidates and interpretation instructions"},
      {"type": "input_image", "image_url": "data:image/jpeg;base64,<bytes>", "detail": "high"}
    ]
  }]
}
```

PDF input uses `{"type":"input_file","filename":"receipt.pdf","file_data":"data:application/pdf;base64,<bytes>"}`.
Alternatively, use `file_id` or `file_url`. Vision-capable models receive extracted
text and page images. Each file must be below 50 MB; all files together may total
50 MB. `detail` accepts `auto`, `low`, or `high`. No fixed PDF page-count ceiling
was established by the fetched guide; impose a local page/token budget. Non-PDF
office documents extract text without embedded images, so they do not provide
equivalent receipt-image interpretation. [File inputs](https://developers.openai.com/api/docs/guides/file-inputs)

Choose inline bytes for this stage: it avoids a public media URL and a separate
Files API lifecycle. Never pass Telegram's bot-token download URL. JPEG/PNG
photos and image documents are the smallest common API/CLI scope. PDF API support
is documented; PDF parity with the isolated Codex adapter remains unverified.
Unsupported formats still require an honest agent-visible unsupported-media
descriptor and localized explanation; do not fabricate semantic classification.

## Retention boundary

Set `store:false` explicitly. This does not mean zero retention: default abuse
monitoring can retain content for 30 days; stored Responses have separate default
application-state retention. Files API objects persist until deletion or configured
expiry. Image/file safety-review exceptions and prompt-cache retention can apply.
Do not claim Zero Data Retention without verifying the project's approved controls.
These API rules do not establish retention for a Codex subscription run.
[Data controls](https://developers.openai.com/api/docs/guides/your-data)

## Telegram ingress

`Message.photo` contains `PhotoSize` variants; each has `file_id`, dimensions, and
optional byte size. Download with `file_id`, not `file_unique_id`. Caption is a
separate optional field for photos/documents. `getFile` returns the download path;
the cloud Bot API documents a 20 MB download maximum and URLs valid for at least
one hour. Calling it again refreshes an expired URL. Original filename/MIME may
not survive `getFile`; preserve message metadata and validate actual bytes.
[Message](https://core.telegram.org/bots/api#message),
[PhotoSize](https://core.telegram.org/bots/api#photosize),
[getFile](https://core.telegram.org/bots/api#getfile)

Implementation recommendation: choose by dimensions/declared bounds, rather than
assuming array order. Preserve the selected Telegram variant's downloaded bytes
as evidence; do not call them the camera original. Keep bounded actual-body reads
even when metadata is absent. Repository 20 MiB and documented Telegram 20 MB
must not be described as identical limits.

## Installed Codex

Read-only local checks returned `codex-cli 0.155.0-alpha.16.3` from
`C:/Users/ddriz/.vscode/extensions/openai.chatgpt-26.917.62051-win32-x64/bin/windows-x86_64/codex.exe`.
`exec --help` explicitly supports `--image <FILE>...`, stdin via `-`,
`--output-schema`, `--ephemeral`, `--ignore-user-config`, and `--model`.
Official CLI documentation also describes initial image attachments.
[Developer commands](https://learn.chatgpt.com/docs/developer-commands?surface=cli)

The current `platform/internal/agent/codex.go` passes text stdin only. Its disabled
`view_image` tool is distinct from a CLI initial image attachment. A candidate
adapter can write a validated synthetic image to its existing private temporary
directory and add `--image <absolute-path>` before the final `-`, while retaining
the existing isolation flags and `gpt-6-luna`. No tool needs to be enabled merely
to test this documented initial attachment path.

The isolated flags passed one live image inference probe with `--image` added.
The generated fixture contains a synthetic receipt total which was deliberately
absent from the text prompt. The structured result was
`{"kind":"receipt","amount":"137.42","currency":"BYN"}`: exact expected values.
There were no tool calls. The existing known code-mode-disabled diagnostic occurred
before the turn; the turn then completed normally. Evidence and reproducible runner
are `qa.local/media-capability-probe/{receipt.png,probe.mjs,schema.json,result.json}`.
The runner caps execution at 75 seconds and combined output at 200,000 characters.

Initial launches failed because the shell lacked a home directory and then because
the outer filesystem sandbox denied normal Codex state access. The successful
launch explicitly set `CODEX_HOME=C:/Users/ddriz/.codex` and
`USERPROFILE=C:/Users/ddriz`, with approved host execution; the nested Codex
read-only sandbox and all disabled-tool flags were preserved. Authentication was
used by Codex normally; this investigation did not open auth files.

This proves a synthetic PNG attachment is visible under the candidate CLI flags,
not adapter or application acceptance. Before accepting the stage, run synthetic
receipts and unrelated portraits through the actual adapter, verify caption
handling and structured output, and reject unexpected tool events. CLI PDF
attachment, production-data use, direct Responses account access, broad OCR
reliability, and application GUI behavior remain unaccepted by this report.
