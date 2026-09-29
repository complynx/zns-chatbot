# Broadcast profile projection

Builder contract; independent acceptance remains required.

The user importer writes `core.admin_broadcast_profiles` in the same transaction
as the user and import receipt. `fields` contains only present source properties,
with the original JSON types and values. Missing, JSON null, empty strings and
empty collections remain distinct. `source_key` and `source_hash` identify the
verified immutable source record.

The allowlist is `user_id`, `bot_id`, `username`, `first_name`, `last_name`,
`print_name`, `language_code`, `known_names`, `informal_name`, `legal_name`,
`role`, `passport_number`, `legal_name_frozen`, `massage_specialist`, and all
present `inner_name_` properties with a nonempty suffix. No fixed locale list
discards source names. Unknown properties remain subject to the existing user
plan blockers. `_id`, workflow state and ban policy are not broadcast fields.
Selectors must explicitly reject unsupported fields; the projection does not
turn unsupported source properties into missing values.

Readers merge `fields || overrides`. They must use the trusted current Telegram
identity for `user_id` and trusted deployment scope for `bot_id`. They must not
overlay normalized user/pass column defaults onto the source projection.

Runtime writes update `overrides` in the transaction that accepts the operation:

- An accepted, monotonic Telegram sender update writes username, first name,
  last name, print name and user ID. Absent optional sender strings clear their
  previous values. Stale or invalid updates write nothing.
- Explicit language selection writes the selected locale. Initialization writes
  only when it initializes an empty stored language and has a nonempty source
  locale. It retains that source locale before presentation fallback.
- Accepted pass `set`/`submit` commands write only their selected role, legal
  name or passport field. Opening or cancelling a prompt creates no defaults.
- An accepted administrator legal-name change writes that field.

No generic public profile dump or arbitrary profile writer is added. Imported
frozen/specialist values remain source values; these hooks do not infer current
values from default columns. Informal-name generation belongs to the broadcast
domain and must preserve concurrent explicit overrides.

Replay verifies the source key, hash and complete immutable projection, while
leaving overrides intact. The existing importer also checks normalized user,
identity and pass columns. Live changes to those columns can still make the
overall import replay fail; this change does not relax that separate invariant.
Changed projection source data fails reconciliation without modifying either
source fields or overrides.

This is an additive migration. Reverting application code leaves the projection
table intact; no source data is deleted. Regenerate user plans and their attested
resolution hashes after upgrading the planner. Old plan bytes cannot silently
acquire newly projected fields because apply regenerates and compares the plan.
