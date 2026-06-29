# New API Upstream Sync Log

This file tracks selective synchronization from upstream New API into MAX-API. It is a durable project workflow record, not a temporary investigation note.

## Current Baseline

- Upstream checkout: `new-api/new-api`
- Upstream remote: `https://github.com/QuantumNous/new-api.git`
- Last reviewed upstream commit: `25f998595d2da4ac9c749f3eae8fffcf9047bc3e`
- Last reviewed upstream tag: `v1.0.0-rc.15`
- MAX-API branch reviewed: `cscitech`
- MAX-API commit reviewed: `477e4809bac3da3584dfec17be97c991fec78ba3`
- Last review date: `2026-06-29`

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

## Incremental Sync Procedure

1. Fetch upstream in `new-api/new-api`.
2. Read `Last reviewed upstream commit` from this file.
3. Compare only `last_reviewed..origin/main`.
4. Classify each upstream commit into security, bugfix, relay, billing, database, frontend, ops, docs, or tooling.
5. Update the Review Queue with status and rationale before porting.
6. Port only `accepted` items, preserving MAX-API behavior.
7. Record verification commands and any skipped/conflicting areas.
8. Advance `Last reviewed upstream commit` only after the range has been analyzed.
