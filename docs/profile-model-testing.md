# Local real-model profile intent probe

On 2026-09-25, six synthetic requests ran through authenticated local Codex with
the exact model `gpt-6-luna`. The probe reads the actual `instructions` and
`planSchema` constants from `platform/internal/agent/openai.go`. It uses the
same fixed isolation options as the Codex adapter: empty temporary directory
plus schema, stdin prompt, ignored user config, ephemeral run, read-only sandbox,
never approvals, and disabled tools/plugins/hooks/memory/code-mode features.
Credentials were neither read nor copied. No bot actions were executed.

| Input situation | Observed typed decision |
| --- | --- |
| Pending legal name; unrelated arithmetic question | Answered “Two plus two is four”; no action |
| Explicit own synthetic name, English, no pending form | Proposed `set legal_name` with exact supplied name |
| Explicit own synthetic name, Russian, no pending form | Proposed `set legal_name` with exact supplied name |
| Bare plausible name with pending legal-name form | Asked whether to save it; no action |
| Third-party name, Russian | Explained that it was a friend's name; no action |
| Quoted self-introduction inside a meaning question | Explained the quote; no action |

All six exited successfully, returned schema-valid plans, and matched the
expected typed decisions. There were no tool execution events. Each event stream
contained the known startup code-mode-host-disabled diagnostic followed by one
completed model turn. The first process also emitted stderr; raw diagnostic
output was not retained. Its structured event stream and exit code succeeded.

**The baseline exposed a wording defect:** the English own-name response told the user to use
an application confirmation button. The profile proposal contract does not
establish such a button. The shared prompt now explicitly forbids invented
profile buttons and extra confirmation steps and explains that the application
executes the proposal and reports the authoritative outcome. One rerun of the
affected English case returned the same valid typed proposal and the text
“I propose saving your legal name as Avery Example.” No fictitious button or
completed write was claimed. The baseline evidence is preserved. This observed
correction does not establish full-feature acceptance or model reliability.

Evidence is local and synthetic:

- `qa.local/profile-model-probe/probe.mjs`: reproducible bounded probe.
- `qa.local/profile-model-probe/results.json`: inputs, expected names, sanitized
  event types, plans, validation results, timestamps and prompt/schema hashes.
- `qa.local/profile-model-probe/review.json`: manual wording review and limits.
- `qa.local/profile-model-probe/recheck.json`: affected-case real inference after
  the prompt correction, with its updated prompt hash.

Each scenario ran once. The probe exercises the CLI with shared source constants,
not the Go adapter or the bot. It does not establish GUI behavior, permissions,
version conflicts, persistence, history, retry safety or production PII handling.
No deterministic fixture substituted for real inference.

## Follow-up: name after an interruption

The initial prompt was too restrictive about context: it discouraged interpreting
a bare supplied name even when a legal-name task was pending. The revised prompt
distinguishes the current name value from intent inferred using conversation/task
context. No introductory phrase is required; unrelated questions remain questions.

Three more real `gpt-6-luna` calls used a pending name task followed by an unrelated
question in history. The current input `Иван Примеров Петрович` and `Avery Example`
each produced the exact typed name update. A new arithmetic question produced an
answer and no action. All three completed without tools; evidence is
`qa.local/profile-model-probe/pending.json`. These are observed cases, not a claim
of statistical reliability. The baseline and earlier recheck are preserved.

## Follow-up: replacing an existing name

The prompt now explicitly permits replacing an editable existing name through a
clear own-name introduction. The `has_legal_name` flag does not reveal the saved
value and cannot justify saying that the supplied name is already saved.

Three real calls after this refinement proposed the exact supplied replacement
for English and Russian introductions. The ambiguous single word `Александр`
instead produced no action and asked whether the user wanted to change their full
name and to supply it fully. Evidence: `replacement-fixed.json` in the same local
probe directory. `replacement-baseline.json` preserves the preceding observations.
Each case ran once; end-to-end independent acceptance is a separate gate.
