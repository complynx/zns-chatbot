# Browser authentication snapshot manifest

This manifest describes only the browser authentication slice. Other concurrent
legacy-order, scripting and pass-tier changes must not be included implicitly.

## New files

- `platform/internal/browserauth/{service,http,telegram,legacy}.go`
- `platform/internal/identity/browser.go`, `browser_test.go`
- `platform/internal/api/browser_auth.go`
- `platform/internal/bot/browser_auth.go`
- `platform/internal/config/browser_auth.go`, `browser_auth_test.go`
- `platform/internal/i18n/catalog_browser_auth.go`
- `platform/internal/miniapp/browser-auth.js`
- `platform/internal/store/migrations/054_browser_auth.sql`
- `platform/cmd/zns/browser_auth.go`
- `platform/integration/browser_auth_test.go`
- `platform/integration/browser_auth_role_test.go`
- `platform/integration/browser_auth_fake_test.go`
- `platform/integration/browser_auth_stand_test.go`
- `platform/tests/browser-auth.mjs`
- `docs/browser-auth.md`, this manifest

## Existing files: exact hooks

- `internal/identity/sandbox.go`: existing `token` delegates to new private
  `tokenUntil(subject, audience, expires)`; HMAC implementation remains shared.
- `internal/api/api.go`: add `browserAuthRoutes(mux, s, signer, logger)` after
  `telegramMetadataRoutes`. No other concurrent route hooks belong to this slice.
- `internal/bot/bot.go`: browserauth import, `Bot.BrowserAuth` field, and the first
  `dispatchUpdate` branch calling `isBrowserAuth` / `handleBrowserAuth`.
- `internal/config/types.go`: `Auth.LegacyBrowserOrigins` string with
  `legacy_browser_origins` YAML/JSON tags.
- `internal/config/auth.go`: `identity.BrowserOrigins` validation at start of
  `Auth.validate`.
- `internal/config/validate.go`: replace the existing Web App URL condition with
  `c.validateWebAppURLs()`. The new helper retains that condition and adds the
  HTTPS requirement for explicitly configured legacy origins.
- `internal/i18n/catalog_en.go`, `catalog_ru.go`: the six `BrowserAuth...` entries
  at the top of each text map, corresponding to the new catalog IDs.
- `cmd/zns/app.go`: `configureBrowserAuth(b, cfg, base)` after maintenance setup;
  `BrowserAuth: b.BrowserAuth` in the Mini App gateway; optional `/auth` route.
  Do not include concurrent `LegacyOrderBotID` setup or Core service fields.
- `cmd/zns/main.go`: `configureBrowserAuth(b, cfg, "")` at the start of `runBot`,
  then `BrowserAuth: b.BrowserAuth` in its gateway.

## Existing files owned exclusively by this slice

- `internal/miniapp/handler.go`: browserauth import/field, shared JS embed and
  route, `/miniapp/auth/` and `/auth` adapters, `authenticate` helper retaining
  signed initData precedence and adding origin-protected cookie fallback.
- `internal/miniapp/editor.js`: import/await shared login; omit empty tma header.
- `internal/miniapp/timetable.js`: same login import/await, cookie fallback,
  remove unconditional rejection when no initData exists.
- `internal/miniapp/timetable.html`: load timetable JS as a module.
- `internal/sandbox/fake.go`: optional username in `labInput`, length validation,
  and sender metadata. Omitted/empty input still clears the username.
- `internal/sandbox/app.js`: include optional username in the lab input payload;
  clear its field when switching synthetic users.
- `internal/sandbox/index.html`: optional username field above the chat input.

All paths in the last two sections are relative to `platform/`. No migration 055,
pass import/tier changes, order callback changes or scripting changes are required.
