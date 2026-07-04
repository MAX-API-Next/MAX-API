# New API Upstream Sync Log

This file tracks selective synchronization from upstream New API into MAX-API. It is a durable project workflow record, not a temporary investigation note.

## Current Baseline

- Upstream checkout: `new-api/new-api`
- Upstream remote: `https://github.com/QuantumNous/new-api.git`
- Last reviewed upstream commit: `917a2cff64feed0acd687298252bd400adf293e0`
- Last reviewed upstream tag: `v1.0.0-rc.16-2-g917a2cff`
- MAX-API branch reviewed: `cscitech`
- MAX-API commit reviewed: `9c407405f32818a41811712372d20fb736adcbf0`
- Last review date: `2026-07-04`

## Sync Rules

- Do not wholesale copy upstream files.
- Preserve MAX-API branding, package/import paths, billing, quota, relay retry, security, and UI customizations.
- Prioritize security fixes, billing/log correctness, relay compatibility, database migrations, and operational reliability.
- Keep scratch notes and generated diffs under `.tmp/`; keep this durable sync log in `.agents/upstream-sync/`.
- For frontend user-facing text, update all supported i18n locales when new strings are introduced.

## Status Values

- `pending`: not yet analyzed
- `accepted`: selected for porting
- `ported`: ported into MAX-API
- `skipped`: intentionally not merged
- `superseded`: MAX-API already has equivalent or stronger behavior
- `blocked`: needs a decision or prerequisite

## Review Queue

| Upstream commit/range | Category | Summary | Status | MAX action | Verification | Notes |
| --- | --- | --- | --- | --- | --- | --- |
| `0b48ad86`, `626dadb5` | security, frontend | Custom HTML/Markdown rendering now uses shared `RichContent`/`HtmlContent`; raw HTML is sanitized before rendering on Home, About, Legal, notification popover, and announcement detail content. | ported | Added MAX-API `HtmlContent` and `RichContent`, replaced unsafe `dangerouslySetInnerHTML` paths in public content pages, sandboxed public content iframes, and reused existing `getRenderableContentKind`. | `bun run typecheck`; targeted Node tests for `html-content`, `markdown`, and `renderable-content`. | Highest-priority fix from the 2026-06-29 upstream review. Preserve MAX-API public page layout and branding while porting. |
| `3a506f50`, `2d5a0416` | relay, compatibility | Harden Chat-to-Responses conversion and add Responses-to-Chat/Gemini Responses support. | pending | Evaluate after security rendering fix. | - | Likely conflicts with MAX-API empty completion retry and billing/log behavior. |
| `df44a75d` | bugfix, logs | Adapt ClickHouse log LIKE filters. | pending | Evaluate if ClickHouse log DB is enabled or supported in MAX-API deployments. | - | Current MAX-API still has generic `LIKE ? ESCAPE '!'` in `model/log.go`. |
| `d10fc762` | bugfix, tasks | Attribute async task usage log to the initiating node. | pending | Port narrowly through task private data and task billing log attribution. | - | Important for multi-node deployments. |
| `4aee5f7d` | permissions, admin | Better admin permissions/RBAC for channels and users. | pending | Needs design review before porting. | - | Large change touching backend authz, routes, models, and frontend permissions. |
| `966af88e` | frontend, playground | Improve Playground chat experience and Markdown rendering. | pending | Evaluate after preserving MAX-API one-click clear behavior from `477e4809`. | - | Large frontend refactor. |
| `25f99859`, `1d166532` | frontend, channels | Refine channel management UI and channel drawer layout. | pending | Evaluate after higher-priority fixes. | - | Large channel drawer diff; likely conflicts with MAX-API custom channel UX. |
| `43591fba` | frontend, channels | Improve advanced custom route editor density and validation affordances. | pending | Evaluate separately against MAX-API custom channel UX. | - | UI-only priority after backend/relay fixes. |
| `c8491b41`, `e514db20` | tasks, billing | Doubao Seedance 2.0 output-resolution/video-input billing plus `safety_identifier`/`priority` request fields. | superseded | MAX-API already has equivalent or stronger Doubao support and local billing tests. | Existing `relay/channel/task/doubao` tests. | Do not overwrite local task billing enhancements. |
| `52858ad1` | tasks, relay | Add Wan2.7 i2v `input.media` mapping and single-image normalization. | ported | Added Wan2.7 model entries, `input.media` normalization, `image` to `images` direct-task normalization, and regression tests while preserving MAX-API Kling/multi-shot media logic. | `go test ./relay/channel/task/ali ./relay/common`; broader focused Go test set; `go run ./tools/jsonwrapcheck`; `git diff --check`. | Uses local `[]map[string]interface{}` media shape instead of upstream typed struct to preserve local extensions. |
| `fda81778`, `1f4d8d2b` | frontend, rendering | Isolate custom HTML rendering and clone app styles into the isolated root. | pending | Evaluate against MAX-API shared `html-sanitizer` and `RichContent` behavior before porting. | - | Optional unless custom HTML style bleed or style loss is reported. |
| `95e8c5ee` | frontend, build | Optimize Rsbuild/Tailwind build pipeline. | pending | Treat as a separate build tooling change. | - | Requires lockfile/package-manager validation. |
| `f9165e7b`, `e1fd9cc2` | dev, build | Align make targets and dev-web behavior with upstream web naming. | pending | Evaluate against MAX-API local frontend/dev workflow. | - | Low priority. |
| `759ab6bb` | frontend, routing | Keep page state when switching `$section` route tabs by keying transitions on route id. | pending | Evaluate with MAX-API page transition implementation. | - | Optional UX fix. |
| `986d90ae`, `8874d192` | ops, logs | Add graceful shutdown, flush quota dashboard cache on exit, and make quota logging synchronous. | ported | Replaced `server.Run` with `http.Server` graceful shutdown, flushes `SaveQuotaDataCache()`, delayed startup success until listen goroutine starts, and logs quota data synchronously. | `go test . ./controller ./model ./relay/channel/ollama ./relay/channel/task/ali ./relay/common`; `go run ./tools/jsonwrapcheck`; `git diff --check`. | Preserves MAX-API startup, pprof, and analytics injection. |
| `0565e626` | frontend, auth | Only treat 401 as session expiry in authenticated route guard. | ported | Updated `web/default` auth guard to leave local session intact on network errors, timeouts, and 5xx. | `bun run typecheck`; `git diff --check`. | No new i18n strings. |
| `c1903607` | frontend, channels | Persist channel status filter across page navigation. | pending | Evaluate against MAX-API URL-state table behavior. | - | Optional UX fix. |
| `69c4d83d`, `1dcb389d` | dependencies, security | Bump `golang.org/x/net` and `golang.org/x/image` plus related x/* modules. | pending | Handle in a dedicated dependency PR with full Go validation. | - | Avoid mixing dependency churn into behavior port. |
| `bfddc5fe` | security, users | Omit `access_token` from user queries. | superseded | MAX-API `User.AccessToken` already uses `json:"-"`; optional DB `Omit` can be considered separately. | - | No API JSON exposure found in current MAX-API user DTO. |
| `dfc0d632` | users, billing correctness | Harden user setting/profile updates so stale user snapshots do not overwrite quota counters; add critical rate limit for self update. | ported | Added `UpdateUserSetting`, refreshed only non-quota cache fields on profile/settings update, protected `quota`/`used_quota`/`request_count` in `User.Update`, updated setting paths, and added regression tests. | `go test ./model -run "TestUserUpdateDoesNotOverwriteAccountingFields|TestUpdateUserSettingOnlyUpdatesSetting"`; broader focused Go test set; `go run ./tools/jsonwrapcheck`; `git diff --check`. | Preserves explicit quota mutation paths and token/cache invalidation logic. |
| `0977965d` | relay, compatibility | Handle Ollama non-stream tool calls and use `tool_calls` finish reason. | ported | Converted Ollama tool calls to OpenAI responses in stream and non-stream paths, preserved tool-call arguments with explicit zero values, and migrated touched JSON calls to `common.*`. | `go test ./relay/channel/ollama`; broader focused Go test set; `go run ./tools/jsonwrapcheck`; `git diff --check`. | Keeps `json.RawMessage` type references only. |
| `55858f35` | CI, release | Add manual Docker image publishing workflow. | skipped | Do not directly merge upstream workflow because it hardcodes upstream image names and branding. | - | Any MAX-API equivalent must be designed with MAX-API image paths and protected project identity. |
| `12fc0100`, `5bf34683`, `bff701b0`, `917a2cff` | deps, docs, format | Electron lockfile bumps, frontend formatting, AGENTS docs, and `tmp` dev dependency bump. | pending | Evaluate only if those surfaces are maintained in this fork. | - | Low-priority/noisy changes. |

## Incremental Sync Procedure

1. Fetch upstream in `new-api/new-api`.
2. Read `Last reviewed upstream commit` from this file.
3. Compare only `last_reviewed..origin/main`.
4. Classify each upstream commit into security, bugfix, relay, billing, database, frontend, ops, docs, or tooling.
5. Update the Review Queue with status and rationale before porting.
6. Port only `accepted` items, preserving MAX-API behavior.
7. Record verification commands and any skipped/conflicting areas.
8. Advance `Last reviewed upstream commit` only after the range has been analyzed.
