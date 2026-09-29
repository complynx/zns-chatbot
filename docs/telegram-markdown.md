# Telegram Markdown conversion

`platform/internal/tgmarkdown` parses ordinary Markdown and emits Telegram
MarkdownV2. It replaces the Python assistant's conversion boundary:
`telegramify_markdown.markdownify(content=result.content, latex_escape=True,
normalize_whitespace=True)` in `zns-chatbot/plugins/assistant.py`. The old Python
constraint is `telegramify-markdown>=0.5.1` in `pyproject.toml`.

## API and transport contract

```go
text, err := tgmarkdown.Convert(source)
// On success: send text with parse_mode="MarkdownV2".
// On error: report the conversion failure or send the ORIGINAL text without parse_mode.
```

`Convert` returns `ErrTooLong`, `ErrInvalidText`, or `ErrUnsafeURL` for rejected
input. It returns no partial result. `Escape` quotes literal text outside code
and link destinations; it is not a Markdown parser or a link sanitizer. Do not
use conversion output as ordinary Markdown input again. The caller handles
Telegram message/caption limits; this package does not split messages or perform
network requests.

The Telegram client now applies this contract through `FormatSend` and
`PrepareSend`. `Send.NativeMarkdown` is explicit opt-in. `LiteralPrefix` and
`LiteralSuffix` keep profile names and other manual text literal while formatting
the model reply. Conversion failure sends the complete original text without a
parse mode. `VisibleText` enforces Telegram's 1–4096 UTF-16-unit text limit after
parsing; oversized messages return an error rather than being truncated.

Migration 025 stores reply provenance in `bot.interactions.native_markdown`.
Existing rows and manual/business replies default to plain text. Model replies
record their format in the same insert as their content. Rendering therefore
does not infer formatting from message contents or reinterpret old manual names.

`ParseV2` decodes the supported output grammar into visible text and UTF-16
entities. The fake Telegram service uses it for send and edit, persists entities
across restart, and replaces old entities on every edit. The GUI constructs DOM
nodes with literal text and allowlisted links; it never inserts message HTML.
This parser is a bounded implementation of the converter's output grammar, not
a claim to implement every Telegram MarkdownV2 extension.

Integration tests cover model delivery, unsafe-link fallback, persisted entities,
plain edits clearing formatting, non-BMP offsets, and message-length boundaries.
The isolated browser harness runs against a fresh test database and HTTP server:

```powershell
$env:MARKDOWN_BROWSER='1'
$env:NODE_BINARY='<path to node.exe>'
$env:BROWSER_CHANNEL='msedge'
go test ./integration -run '^TestMarkdownBrowser$' -count=1 -v
```

Set `TEST_DATABASE_URL` as for other integration tests. The harness verifies
formatted DOM, mouse clicks and emulated-touch taps on Telegram links, edited
links, literal HTML-like text, and mobile width. Screenshots are written to
`platform/test-results/markdown/`. It passed with headless Edge on 2026-09-26;
this evidence does not replace independent functional acceptance or a live
Telegram network test.

The input limit is 64 KiB; AST limits are 64 levels and 16,384 nodes. Output has a
256 KiB limit. Input must be valid UTF-8 and exclude unexpected control bytes.
These bounds apply before parsing and before returning output. They are parser
resource limits, not Telegram's message-size limits.

## Supported output

| Input                                            | Telegram output                                           |
| ------------------------------------------------ | --------------------------------------------------------- |
| CommonMark strong/emphasis, nested emphasis      | Bold/italic, with duplicate styles collapsed              |
| `~~strike~~`                                     | Strikethrough                                             |
| Inline and fenced/indented code                  | Code/pre, with literal backticks and backslashes escaped  |
| Headings                                         | Bold text                                                 |
| Lists                                            | Text bullets or escaped numeric markers, with indentation |
| Blockquotes                                      | Telegram blockquotes; nested quotes collapse to one level |
| Markdown links, reference links, angle autolinks | Telegram inline links                                     |
| Images with alt text                             | Alt text as a link; no image fetch or upload              |
| Raw HTML                                         | Literal escaped text, never HTML execution                |
| Tables, math, underline/spoiler syntax           | Ordinary text under CommonMark rules; no extra extension  |

Telegram does not permit code entities inside formatted text, links, or quotes.
The converter closes/reopens surrounding emphasis around inline code. Code in
links and quotes keeps its literal content without code formatting. It separates
ambiguous adjacent italic delimiters with an empty bold entity. These are explicit
compatibility choices, not complete parity with every Python extension.

URL targets are preserved after CommonMark backslash/entity decoding; the
converter escapes only `)` and backslash for Telegram's destination syntax.
HTTP and HTTPS need a host and must not contain credentials. The only accepted
`tg:` destination is `tg://user?id=<positive integer>` with exactly that parameter.
Other schemes, relative URLs, control characters, and whitespace are rejected.
The converter does not inspect remote pages, rewrite message/thread identifiers,
or assert that a user can access a private Telegram target.

Examples retained by tests include `https://t.me/name`, `https://t.me/+invite`,
`https://t.me/name/321`, `https://t.me/name/7/321`,
`https://t.me/c/123456789/321`, and `https://t.me/c/123456789/7/321`.
Query parameters such as `thread=7&single` are retained.

Protocol references:
[Telegram MarkdownV2 and entity nesting](https://core.telegram.org/bots/api#markdownv2-style),
[Telegram message and topic links](https://core.telegram.org/api/links#message-links).

## Dependency decision, checked 2026-09-26

Use **`github.com/yuin/goldmark v1.8.6`** for CommonMark parsing, with its bundled
strikethrough extension. Our renderer only consumes the AST. It does not use the
HTML renderer, dynamic extensions, remote content, or a second Telegram adapter.
The exact tag resolves to commit `e3e8a533aa19da2f296fcdb97b7674ae7d1d93a4`.

- License: [MIT at the selected tag](https://github.com/yuin/goldmark/blob/v1.8.6/LICENSE).
  This is within the requested license set. The
  [selected module file](https://github.com/yuin/goldmark/blob/v1.8.6/go.mod)
  has no third-party runtime requirements.
- Maintenance: [v1.8.6 release](https://github.com/yuin/goldmark/releases/tag/v1.8.6)
  was published on 2026-09-03 with a parser-extension fix. The repository also
  publishes v2 releases, including v2.1.5 on September 19. V1.8.6 is the chosen
  maintained v1 version, not a claim that v1 is the latest major version.
- Actual adoption: [Hugo's module manifest](https://github.com/gohugoio/hugo/blob/master/go.mod)
  directly pins Goldmark v1.8.6;
  [Hugo's rendering implementation](https://github.com/gohugoio/hugo/blob/master/markup/goldmark/render_hooks.go)
  imports its parser/AST packages. [Hugo's own documentation](https://gohugo.io/configuration/markup/)
  says Goldmark is its default Markdown renderer. These are implementation and
  product evidence, rather than star counts. They establish adoption by Hugo,
  not a measured count of independent users or bot-free popularity.
- Alternative examined: [goldmark-tgmd](https://github.com/Mad-Pixels/goldmark-tgmd)
  advertises Telegram MarkdownV2 rendering and Apache-2.0 licensing. Its existence
  alone did not establish independently verified maintenance and downstream use
  for this task. A small local renderer over the independently adopted parser
  keeps URL policy and Telegram nesting behavior in the tested project code.

Security check executed against the actual selected package:

```text
platform/tools.local/govulncheck.exe ./internal/tgmarkdown/...
No vulnerabilities found.
```

The scan used the live Go vulnerability database on 2026-09-26. This is a
point-in-time check, not proof that the parser has no vulnerabilities. The project
must repeat its full dependency and vulnerability gates when upgrading. Package
tests cover escaping, nested/code boundaries, URL delimiter injection, public and
private Telegram targets, unsafe schemes, invalid text, and resource bounds.
