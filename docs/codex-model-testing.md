# Local real-model testing through Codex

The local Codex CLI can supply real synthetic model decisions without an API key
in the sandbox. This is optional local testing, not a production provider or a
replacement for deterministic CI scenarios.

Verified on 2026-09-25 with installed `codex-cli 0.155.0-alpha.16.3` and exact
model `gpt-6-luna`: one synthetic order/shuttle input returned a schema-conforming
JSON proposal, explicitly saying it had not executed. No tool calls appeared in
the JSONL events. Evidence: `qa.local/codex-model-probe/result.json` and the local
probe script. This does not prove a full bot/API integration or model parity.

The probe used an empty temporary workdir, `--ignore-user-config`, `--ephemeral`,
read-only sandbox, approval policy `never`, structured output schema, disabled
web search and disabled shell/apps/browser/plugins/hooks/memory/multi-agent/image
features. Authentication stayed in Codex; no token was read or copied into the
repository. The CLI emitted a nonfatal code-mode-host-disabled event and still
completed the structured answer; a future adapter must distinguish startup
diagnostics from a failed inference without accepting arbitrary error states.

Next integration boundary: a local-only model adapter accepts the same bounded
`agent.Input` and returns a validated `agent.Plan`. It must not expose Codex tools
to the bot agent or execute proposed actions itself. The existing bot executor
and Core API retain identity, version, permissions and business constraints.
Keep timeouts, output limits, process cleanup and explicit opt-in; do not send
production profiles, passports or receipts. Real-model tests need their own
evidence and must not silently replace `gpt-6-luna` with a different model.

Official references: [non-interactive structured output and saved authentication](https://learn.chatgpt.com/docs/non-interactive-mode),
[configuration controls](https://learn.chatgpt.com/docs/config-file/config-reference).
