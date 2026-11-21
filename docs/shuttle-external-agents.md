# External agents in Zinusya

Open `/mcp` in a private chat with Zinusya. Enable read access, copy the
server URL into your external MCP client and sign in there with the same
ZITADEL account. History, personal memory and the organization's skill
library are separate settings. External clients can disclose the data they
read to their own model. The history setting includes past results from
the default chat.

When Loom advertises the `zinusya` token service, external agents can use
Zinusya's account, profile, people, registration, knowledge, orders, meals,
massage, pair, payment, communication, and other read tools. Event staff
and administrators get the additional reads their current roles allow.
The optional history setting governs Loom chat history. Zinusya's
owner-scoped legacy history follows the Zinusya tool permissions.

Enable **Write and management tools** in `/mcp` to use the write catalog.
The external client uses Loom's `write_catalog` to inspect the current
methods and sends a stable request ID with each `write`. Profile, event,
registration, booking, order, knowledge, payment and administration
actions use the same role and ownership checks as the main bot.

When a service action requires approval, Zinusya sends the prepared terms
to the user's private Telegram chat. Orders and payment reviews show the
full terms from the saved preparation before the buttons appear. The buttons approve
the exact request once or reject it. Eligible methods offer two more
choices. Remember approval for the same method, or allow all eligible
writes for this MCP connection. Mandatory approvals still appear
for each operation. Closing the connection or withdrawing write access
ends its remembered rules. The external client calls `write_continue`
with the original operation ID to resume delivery if the prompt did not
arrive or to execute the operation after approval.
Only the last message can confirm a multipart prompt. If Telegram loses the
delivery receipt for that message, continuation can send a new final part.

File transfers into Loom runs, run-bound one-step avatar generation and
control of MCP access stay in Telegram. External agents can prepare and
finalize avatar drafts for Telegram delivery. Users can enter a passport
number in the private form or explicitly provide it to an agent for
registration. Both write paths return presence and version only. Loom encrypts
the passport write arguments using its storage key.

**Turn off access and close connections** disables the profile and revokes
all open MCP sessions for that user in the organization. To end just one
session, ask Zinusya in the normal chat to list and revoke MCP connections
using Loom's `mcp_access` tools.

## Deployment

1. Upgrade Loom to a version that supports the external-agent manifest
   permission. Configure its `external_agents` settings, a separate MCP
   audience and introspection credentials, external disclosure, and the
   public routes for the MCP endpoint and its OAuth resource metadata.
   Register compatible MCP clients at the issuer.
2. Add `external_agent.profile.write` to the existing Loom role mapping
   for Zinusya's service caller, for example `zinusya-caller`.
3. Set `loom.external_agent_enabled: true` in Zinusya, or set
   `ZNS_CHATBOT_LOOM_EXTERNAL_AGENT_ENABLED=true`. This publishes the caller
   permission. Zinusya checks the authenticated Loom capability when the
   user opens `/mcp` or clicks a settings button.
4. To enable Zinusya tools, configure `loom.external_agent_tokens` with
   the separate `audience` and an MCP introspection `client_id` and
   `client_secret`. Set the shared `secret` to the same value as Loom's
   `external_agents.token_services.zinusya`. Environment overrides are
   `ZNS_CHATBOT_LOOM_MCP_AUDIENCE`, `ZNS_CHATBOT_LOOM_MCP_CLIENT_ID`,
   `ZNS_CHATBOT_LOOM_MCP_CLIENT_SECRET`, and `ZNS_CHATBOT_LOOM_MCP_TOKEN_SECRET`.
   Keep the token endpoint on Zinusya's internal listener.
   After upgrading Loom to a version that protects private write arguments, set
   `loom.external_agent_tokens.writes: true` or `ZNS_CHATBOT_LOOM_MCP_WRITES=true`
   to publish write tools and the internal Telegram approval endpoint.
5. Configure the issuer so the regular Zinusya exchange client can issue
   a delegated JWT for the Zinusya API without the Loom or MCP audiences.
   Its API introspection must return current API-project roles. Loom must
   also see the `zinusya.admin` permission in the MCP identity to discover
   the global administration module. Map the corresponding role in Loom
   and make that role available to MCP introspection without adding the
   Zinusya API audience to MCP tokens.
6. In a private Telegram chat, open `/mcp`, enable access, and connect a
   real MCP client. From `script`, call
   `return await modules.zinusya_account.me({})` and check admin discovery
   with accounts with and without the role. If Loom advertises multiple
   token services, use `token_service: "zinusya"` in the script arguments.
   Turn access off in Telegram and verify the same MCP session is rejected.
   When write access is enabled, try a pending request with one test account.
   Verify the Telegram decision, the original operation ID on continuation,
   and the absence of a main agent run.

Zinusya confirms the user's profile over Loom's delegated `/v1` API.
The external client handles OAuth and talks directly to Loom. Loom uses a
separate credential for the active local user when calling Zinusya. Zinusya
limits that credential to published tool routes. Write and prepare routes
and write-only status routes also require Loom's private service key.
The registration details operation route is additionally published as an
owner read and returns only the redacted receipt. Every handler continues
to check ownership and role permissions.

For rollback, disable the MCP endpoint in Loom to stop external access,
then turn off the Zinusya flag before rolling back to a Loom version with
the older manifest schema.
