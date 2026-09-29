# On-demand agent skills

Both real providers, OpenAI Responses and the opt-in local Codex CLI, use the
same model-selected skill pipeline. `Model.Plan(ctx, Input) (Plan, error)` and
the public plan contract includes nullable `knowledge_action` and `script_action`
plus `history_action` and `registration_action` (ten fields total). The model remains `gpt-6-luna`.
Scripted fixtures remain deterministic and do not make skill-selection calls.

For each ordinary planning attempt:

1. The provider sends a short catalog of skill IDs and **use when** descriptions,
   plus the current bounded user input, history, authoritative state and any
   actual images. No domain instruction bodies are in this request. The model
   returns `{"skills":["profile"],"reply_language":"en"}`, for example.
2. The host rejects malformed selections, unknown IDs, duplicates, paths, extra
   fields, more than ten IDs or more than 512 bytes of selection text. An empty
   list is valid for general conversation. IDs resolve only to embedded,
   allowlisted Markdown resources; there is no filesystem path chosen by the
   model and no network fetch.
3. The provider sends the same bounded input with the brief global permissions
   and output contract, existing language/persona rules, and **only the selected
   skill bodies**. The model returns the normal typed Plan. Existing validation,
   catalog checks and host-side authorization still apply.

There is no keyword router or automatic loading of every media skill. The model
may select any needed subset, including all ten for an unusually broad request;
the host does not force that choice. A pending task alone is not a reason to
select its domain. Wrong or missing skill selections can reduce answer quality,
but can never authorize an action. The host remains responsible for identity,
ownership, permissions, versions, deadlines and idempotency.

| Skill ID | Use when |
| --- | --- |
| `knowledge` | Event/general facts, suggestions, review cards and owner-private memos |
| `history` | Earlier conversation, manual/API changes and gaps in recent context |
| `registration` | Festival passes, partner invitations, payment contacts and permitted queue administration |
| `scripting` | Calculations, aggregations and transformations of explicit JSON data |
| `booking` | Service catalogs, slot selection and booking workflow explanations |
| `orders` | Festival orders/extras, order exports and payment instructions |
| `profile` | Own legal-name questions, changes, introductions and pending name tasks |
| `receipts` | Current image/file purpose, receipt evidence, proof history and pending receipt choices |
| `av` | Current voice/audio/video, transcripts, timestamped frames and inspection results/budgets |
| `stickers` | Sticker/custom-emoji artwork observations and their meaning in the current message |

Bodies live in `platform/internal/agent/skills/*.md`. Existing detailed domain
instructions were moved into these resources. Current-evidence reminders are
included only with the relevant selected skill. In particular, video sampling,
inspection limits and frame instructions are not in the global planning prompt.
The brief catalog describes when AV is needed without loading those details.

## Bounds and preserved behavior

Each ordinary attempt uses exactly one selector and one final planning call.
There is no recursive skill-reading loop, fallback that loads all skills, or
retry on malformed selection. A failed selector fails the attempt closed.
The selector has at most 15 seconds within the existing overall provider
deadline: 30 seconds for OpenAI, 60 seconds for Codex, or any earlier caller
deadline. OpenAI selection output is capped at 256 tokens; final plans retain
1600 tokens. Codex selector stdout and stderr are each bounded to 64 KiB of
CLI event data, followed by the same 512-byte decoded selection limit. Its
final-call output bounds remain unchanged. Selected instruction text is limited
to 24 KiB. The ten-item limit permits every catalog skill without granting an
unbounded prompt budget.

The current utterance is still repeated last **within** the total input budget.
The real-provider input still omits GUI locale; current text or successful
speech determines conversational language. The existing persona rules are
included only in final conversational planning, not asset-description tasks.
Skill selection does not strip images, successful speech, pending choices,
inspection history or persisted remaining inspection budget. Each AV refinement
attempt selects skills again against its updated evidence. The host's inspection
budget is unchanged; skill reads do not grant extra video inspections.

Canonical `AssetTask` requests bypass skill selection entirely. They keep the
single-call English description prompt, no persona, no conversation context and
no business actions. The asset-description version/cache contract is unchanged.

Codex still uses an ephemeral temporary directory, read-only sandbox, explicit
output schema, no project instructions and disabled shell, browser, apps, plugins,
memory, multi-agent, code-mode and skill-search features. It does not execute a
host skill tool or read repository skill paths. The application loads the
allowlisted skill text and supplies it on stdin. CLI events that indicate tool
use remain rejected.

## Verification boundary

Focused tests cover actual two-request OpenAI transport, fake Codex subprocess
selection, no unselected domain bodies in final prompts, general conversation
with zero skills, malformed/duplicate/path selections, cancellation before the
planning call, preserved transcript/refinement state and unchanged action
validation. Canonical assets retain their isolated prompt tests. Existing agent
tests remain in place, including provider schemas and attachment validation.

No live model call is made by these unit tests. Real multilingual and domain
selection acceptance, independent Code QA and Functional QA remain separate
stage gates. This change adds one bounded model call to each ordinary planning
attempt; latency and token cost must be assessed in those real-provider checks.

The selector also returns a validated BCP47 reply language derived from the current
utterance or successful direct speech. The planner receives it as a final language
instruction. This adds no model call and has no authorization meaning; GUI locale,
persona and retrieved facts do not select the conversational language.

