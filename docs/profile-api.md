# Profile API contract

All routes use the authenticated subject as owner. A request cannot choose another
owner. Responses use `Cache-Control: no-store`. Sandbox credentials and HTTP helpers
are documented in [the FQA kit](../platform/scripts/fqa/README.md).

## Read

`GET /v1/me/pass-profile` returns HTTP 200 with:

```json
{
  "owner": "alice",
  "version": 0,
  "role": "",
  "legal_name": "",
  "passport": "",
  "frozen": false,
  "pending": "",
  "passport_after": false
}
```

An existing pending task also has `expires_at` (RFC3339). Reading does not create
a task or remove an expired one. An unknown subject receives 403 `forbidden`.

`GET /v1/me/pass-profile/history` returns up to 30 recent changes, oldest first.
Each has `version`, `action`, `field`, `origin`, and `at`. It contains no name or
passport values. A replay does not create an extra change.

## Change

`POST /v1/me/pass-profile/actions` accepts:

```json
{
  "name": "set",
  "field": "legal_name",
  "value": "Avery Synthetic Example",
  "version": 0,
  "key": "unique-synthetic-operation",
  "origin": "manual"
}
```

- `version`: latest observed nonnegative version.
- `key`: unique per operation, nonempty UTF-8, at most 200 bytes. Retry the exact
  same body/key. Success returns the current profile, including after a newer edit.
- `origin`: `manual` or `agent`; neither grants extra permission.
- `begin`: `field` is `legal_name`, `passport`, or `role`; omit `value`.
  Starts a 15-minute hint. Optional `passport_after: true` is legal only for name.
- `submit`: needs a matching, unexpired hint and a nonempty `value`.
- `set`: accepts an explicit value without a hint. Completes a matching task;
  an unrelated task remains. An active name task with `passport_after` advances
  to passport entry.
- `cancel`: omit `field`, `value`, and `passport_after`; clears the task.
- Values are trimmed and limited to 300 Unicode code points. Role accepts only
  `leader` or `follower`. Telegram agent name proposals have a narrower 200-point
  bound. All example values must remain synthetic in a QA stand.

Success is HTTP 200 with the current profile. An accepted new operation advances
the version. Writes require `can_book`. Frozen profiles reject name/passport
changes, including starting a hint; role changes and cancellation remain allowed.

| HTTP | `code` | Meaning |
| --- | --- | --- |
| 400 | `invalid_json` | Malformed body or unknown fields |
| 400 | `pass_profile_invalid` | Invalid command, field, key, origin, or value |
| 403 | `forbidden` | Unknown subject or writes disabled |
| 409 | `pass_profile_stale` | Version changed |
| 409 | `idempotency_conflict` | Same key with a different command |
| 409 | `pass_profile_frozen` | Identity edits locked |
| 409 | `pass_profile_no_pending` | Submit does not match a task |
| 409 | `pass_profile_expired` | Submit task expired |

## Optional native stand fixtures

The [Codex stand](codex-model-provider.md) has a separate database and ports.
For frozen/rights scenarios, record and restore the prior fixture values using
its explicit Compose project `zns-codex-sandbox` and `compose.codex.yaml`:

```sh
docker --context desktop-linux compose --project-name zns-codex-sandbox -f compose.codex.yaml exec -T postgres psql -U postgres -d zns -c "SELECT owner,frozen FROM core.pass_profiles WHERE owner='bob';"
```

Fixture controls are `core.pass_profiles.frozen` by `owner` and
`core.users.can_book` by `id`. Use SQL only to arrange/restore these controls;
assert behavior through the authenticated API and rendered Telegram UI.
