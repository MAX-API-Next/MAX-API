# Billing Expression System (billingexpr)

## Design Philosophy

**One expression, one truth.** A single expression string completely defines a model's billing logic — pricing, tier conditions, cache/image/audio differentiation, time-based discounts, request-aware multipliers — all in one line. No scattered configuration, no implicit rules, no magic numbers.

The expression is the billing contract between the administrator and the system. What you write is what gets executed. The system's job is to evaluate it faithfully, not to interpret it.

### Core Principles

1. **Expression is self-contained** — The expression string alone determines billing. No external ratio tables, no implicit completion multipliers, no hidden conversion factors. Given the same token counts and request context, the same expression always produces the same cost.

2. **Variables are opt-in** — `p` (prompt) and `c` (completion) are the base. Cache (`cr`, `cc`, `cc1h`), image (`img`), and audio (`ai`, `ao`) variables are optional. If omitted, those tokens are included in `p`/`c` and priced at their rate. The system automatically detects which variables the expression uses (via AST introspection) and adjusts token normalization accordingly.

3. **Prices are real prices** — Expression coefficients are actual $/1M tokens prices as published by providers. No ratio conversion, no `/2` convention. `p * 2.5` means $2.50 per 1M prompt tokens.

4. **Upstream-agnostic** — The expression doesn't need to know whether the upstream API is OpenAI-format (prompt_tokens includes cache) or Claude-format (input_tokens excludes cache). The system normalizes token counts before evaluation based on the upstream response format.

5. **Version-aware** — Expressions carry a version tag (`v1:`, default when omitted). The version controls the compile environment, token normalization, and quota conversion formula, enabling future evolution without breaking existing expressions.

---

## Expression Language

Powered by [expr-lang/expr](https://github.com/expr-lang/expr). Expressions are compiled, cached, and evaluated against a runtime environment.

### Token Variables

**输入侧变量：**

| 变量 | 含义 |
|------|------|
| `p` | 输入 token 数（**计价用**）。**自动排除**表达式中单独计价的子类别（见下方说明） |
| `len` | 输入上下文总长度（**条件判断用**）。不受自动排除影响，始终反映完整输入长度。非 Claude：等于原始 `prompt_tokens`；Claude：等于文本输入 + 缓存读取 + 缓存创建 |
| `cr` | 缓存命中（读取）token 数 |
| `cc` | 缓存创建 token 数（Claude 5分钟 TTL / 通用） |
| `cc1h` | 缓存创建 token 数 — 1小时 TTL（Claude 专用） |
| `img` | 图片输入 token 数 |
| `ai` | 音频输入 token 数 |

**输出侧变量：**

| 变量 | 含义 |
|------|------|
| `c` | 输出 token 数。**自动排除**表达式中单独计价的子类别（见下方说明） |
| `img_o` | 图片输出 token 数 |
| `ao` | 音频输出 token 数 |

#### `p` 和 `c` 的自动排除机制

`p` 和 `c` 是"兜底变量"——它们代表**所有没有被表达式单独定价的 token**。系统会根据表达式实际使用了哪些变量，自动从 `p` / `c` 中减去对应的子类别 token，避免重复计费。

**规则：如果表达式使用了某个子类别变量，对应的 token 就从 `p` 或 `c` 中扣除；如果没使用，那些 token 就留在 `p` 或 `c` 里按基础价格计费。**

> **重要：`len` 不受自动排除影响。** `len` 始终代表完整的输入上下文长度，不管表达式是否单独对缓存/图片/音频定价。因此**阶梯条件应使用 `len` 而非 `p`**，以避免缓存命中导致 `p` 降低而误判档位。

举例说明（假设上游返回的原始数据：prompt_tokens=1000，其中包含 200 cache read、100 image）：

| 表达式 | `p` 的值 | 说明 |
|--------|---------|------|
| `p * 3 + c * 15` | 1000 | 没用 `cr`/`img`，所以缓存和图片都包含在 `p` 里，全按 $3 计费 |
| `p * 3 + c * 15 + cr * 0.3` | 800 | 用了 `cr`，缓存 200 从 `p` 中扣除，按 $0.3 单独计费；图片仍在 `p` 里按 $3 计费 |
| `p * 3 + c * 15 + cr * 0.3 + img * 2` | 700 | 用了 `cr` 和 `img`，都从 `p` 中扣除，各自按自己的价格计费 |

输出侧同理（假设 completion_tokens=500，其中包含 100 audio output）：

| 表达式 | `c` 的值 | 说明 |
|--------|---------|------|
| `p * 3 + c * 15` | 500 | 没用 `ao`，音频输出包含在 `c` 里按 $15 计费 |
| `p * 3 + c * 15 + ao * 50` | 400 | 用了 `ao`，音频 100 从 `c` 中扣除按 $50 计费 |

> **注意：** 这个自动排除仅针对 GPT/OpenAI 格式的 API（prompt_tokens 包含所有子类别）。Claude 格式的 API（input_tokens 本身就只包含纯文本）不做任何减法。系统根据上游返回格式自动判断，表达式作者无需关心。

### Built-in Functions

| Function | Signature | Purpose |
|----------|-----------|---------|
| `tier` | `tier(name, value) → float64` | Records which pricing tier matched; must wrap the cost expression |
| `param` | `param(path) → any` | Reads an approved pricing-metadata path from the request body |
| `header` | `header(key) → string` | Reads an approved provider feature header |
| `has` | `has(source, substr) → bool` | Substring check |
| `hour` | `hour(tz) → int` | Current hour in timezone (0-23) |
| `minute` | `minute(tz) → int` | Current minute (0-59) |
| `weekday` | `weekday(tz) → int` | Day of week (0=Sunday, 6=Saturday) |
| `month` | `month(tz) → int` | Month (1-12) |
| `day` | `day(tz) → int` | Day of month (1-31) |
| `max` | `max(a, b) → float64` | Math max |
| `min` | `min(a, b) → float64` | Math min |
| `abs` | `abs(x) → float64` | Absolute value |
| `ceil` | `ceil(x) → float64` | Ceiling |
| `floor` | `floor(x) → float64` | Floor |

For request-aware pricing, `header()` and `param()` use a positive allowlist.
`header()` currently exposes only `anthropic-beta` and `openai-beta`.
`param()` supports the established pricing metadata paths `service_tier`,
`stream`, `fast`, `stream_options.fast_mode`, `messages.#`, `input.#`,
`tools.#`, `modalities.#`, `response_format.type`, `reasoning.effort`,
`thinking.type`, `thinking.budget_tokens`, and indexed `role` / `type` fields
under `messages`, `input`, or `tools`.

All other headers and body paths return an empty string or `nil`. GJSON
modifiers, pipelines, queries, wildcards, whole-document selectors, prompt or
message content, tool definitions, credentials, user metadata, image, audio,
and other raw payload fields are not available to pricing expressions.

### Expression Examples

```
# Simple flat pricing
tier("base", p * 2.5 + c * 15 + cr * 0.25)

# Multi-tier (Claude Sonnet style) — use len for tier conditions
len <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)

# Image model (no separate cache/audio pricing — those tokens stay in p/c)
tier("base", p * 2 + c * 8 + img * 2.5)

# Multimodal with audio
tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)
```

### Request Rules (appended after `|||`)

Request-conditional multipliers are appended to the expression after a `|||` separator:

```
tier("base", p * 5 + c * 25)|||when(header("anthropic-beta") has "fast-mode") * 6
```

These are parsed and applied separately by the request rule system.

---

## Task expressions: explicit `new-api-task@1` profile

The token path above remains v1: `exprOutput / 1,000,000 * QuotaPerUnit`.
Task pricing uses the same compiler/runtime through `QuoteTaskExpression`, but
requires the explicit `new-api-task@1` profile and interprets the output as a
**total currency amount**. A `v1:` syntax prefix alone never switches semantics.
Task programs have a separate cache namespace. The token compiler still rejects
`u()`; no legacy channel/task is activated or migrated by this addition.

```text
u("tokens") / 1000000 * (u("video_input") == "video" ? 2 : 3)
u("units") * 0.1
```

`u("key")` reads host-validated scalar facts (normalized float64, boolean or
enum string). It has no default value: a missing evaluated key is an error,
while explicit zero and false are retained. `TaskUsageKeys` inspects the parsed
tree **before optimization**, including unreachable branches. The host checks
every referenced key against the selected model/source schema using
`taskpricing.ValidateExpressionContract`. Dynamic keys, indirect/aliased `u`,
`$env`, `fixed()` and implicit token variables such as `p`/`c` are rejected in
task expressions. This is deliberately stricter than upstream's omission of
dynamic usage references. An optional declared fact can be absent if not
evaluated; a final-required fact must always be supplied under its contract.

`TaskQuoteInput` requires an explicit `PricingAt`. All task time helpers use
that instant (empty/invalid timezone falls back to UTC); they do not read the
wall clock. Request probes retain the existing positive allowlist. The host
must retain this time and the relevant request metadata with future task plans.
The current token clock/allowlist behavior is unchanged.

Task helpers reject the special Go timezone `Local`, including surrounding
whitespace. Unlike an explicit IANA zone, `Local` depends on each node's system
configuration and cannot be reproduced from PricingAt alone. Direct literal
arguments are checked before optimization (even in hidden branches); computed
arguments are checked at runtime and return a quote error, not a zero price.
Empty/invalid zone fallback and the legacy token path retain their existing
behavior. Explicit IANA zones still require consistent timezone-rule data on
deployment nodes; the isolated tests do not certify that environment condition.

The task profile also rejects Expr's `now()`, `date()` and `timezone()` builtins
before optimization, including hidden branches. These time-object factories
can read the wall clock or expose methods such as `.Local()` that bypass the
frozen scalar helpers. This restriction applies only to task prices; existing
token expressions and deterministic scalar builtins keep their current behavior.

```text
amount = evaluateTaskExpression(validatedFacts, frozenRequest, PricingAt)
quotaBeforeGroup = amount * QuotaPerUnit
quotaAfterGroup = roundHalfAwayFromZero(quotaBeforeGroup * GroupRatio)
reservedPreview = max(reservationFloor, completeEstimate.quotaAfterGroup)
finalTargetPreview = finalQuote.quotaAfterGroup
finalMinusReserved = finalTargetPreview - reservedPreview
```

Task prices are not divided by a million again and do not apply legacy
`OtherRatios`. Conversion requires finite positive QuotaPerUnit and finite
non-negative GroupRatio; zero GroupRatio is explicit free pricing. Invalid
facts, negative/non-finite results and int32 quota overflow return an error.
Overflow is checked before the shared rounding helper so rejected previews do
not silently clamp or emit saturation logs. The reservation floor applies once
after the complete estimate, never to the final target.

`pkg/taskpricing.Preview` composes source selection, contract/guard comparison,
submit facts and final facts with this core. Nil final usage means **unknown**,
not zero. Only frozen request-phase facts are inherited; final-required actual
facts cannot reuse estimates. A positive finalMinusReserved only reports a
funding gap; it does not authorize a debit or prove a reservation upper bound.

These functions are pure calculations. They do not read prices from a database,
check real cache freshness, invoke a provider, persist a TaskBillingPlan or
reserve/settle funds. Current/expected guard equality alone is not a DB CAS.
The model API/transaction/editor caller and paid task admission remain
separate implementation stages. H3/Kling/Seedance fixtures now invoke this core;
their expected amounts remain independently derived integer/rational constants.

`taskpricing.Freeze` now produces a serializable expression branch of the existing
`TaskBillingPlan`. It freezes the selected expression/hash, original/mapped model,
actual producer/package/channel/request/action identity, price/contract/binding
versions, full semantic fact contract, submit facts, pricing instant, currency,
quota conversion, group ratio, floor and rounding version. It is pure and does
not prove authoritative reads or a provider exposure bound. Its current caller
is an isolated task harness; the production submit gate rejects the new profile.

Frozen plans reject `param` and `header` dependencies before optimization,
including dead branches: they must be represented as declared request-phase
facts for this slice. Read-only configuration previews retain their existing
request-probe behavior. No raw request body or credentials are persisted in the
new snapshot. Completion uses the frozen contract and a host-owned typed
`TaskPricingUsageEnvelope` matching producer package version/digest, channel,
request/action and contract; it never turns fractional credits into tokens or
reuses missing actual usage from the estimate. Missing/incompatible evidence or
a final target above actual reservation stays pollable under manual settlement.
Existing durable task intents/funding/effects remain the sole accounting path;
an explicit zero target and a zero delta still complete that lifecycle. Old H3
plans without a profile retain their existing validator and component pricing.

## Architecture

### Data Flow

```
Frontend Editor → Storage → Pre-consume → Settlement → Log Display
```

### 1. Frontend Editor

**File**: `web/src/pages/Setting/Ratio/components/TieredPricingEditor.jsx`

Two editing modes:
- **Visual mode**: Fill in prices per variable, conditions per tier. Generates expression via `generateExprFromVisualConfig()`.
- **Raw mode**: Edit the expression string directly. Includes preset templates for common models.

The editor outputs a billing expression string and an optional request rule expression string. These are combined via `combineBillingExpr(billingExpr, requestRuleExpr)` before storage.

### 2. Storage

**File**: `setting/billing_setting/tiered_billing.go`

Two option maps stored in the `options` DB table:
- `ModelBillingMode`: `{ "model-name": "tiered_expr" }` — activates tiered billing for a model
- `ModelBillingExpr`: `{ "model-name": "tier(\"base\", p * 2.5 + c * 15)" }` — the expression

On save, the expression is validated:
1. Compiled via `billingexpr.CompileFromCache()` — syntax check
2. Smoke-tested with sample token vectors — ensures non-negative results

### 3. Pre-consume (Quota Estimation)

**File**: `relay/helper/price.go` → `modelPriceHelperTiered()`

When a request arrives and the model uses `tiered_expr` billing:
1. Loads expression from `billing_setting.GetBillingExpr()`
2. Builds `RequestInput` (headers + body) for `param()` / `header()` functions
3. Runs expression with estimated tokens: `RunExprWithRequest(expr, {P, C}, requestInput)`
4. Converts output to quota: `rawCost / 1,000,000 * QuotaPerUnit`
5. Creates `BillingSnapshot` (frozen state for settlement) and stores on `RelayInfo`

### 4. Settlement (Actual Billing)

**Files**: `service/tiered_settle.go`, `pkg/billingexpr/settle.go`

After the upstream response returns with actual token usage:

1. `BuildTieredTokenParams(usage, isClaudeUsageSemantic, usedVars)`:
   - Reads actual token counts from `dto.Usage`
   - For GPT-format APIs (prompt_tokens includes everything): subtracts sub-categories from P/C **only when** the expression uses their variables (detected via AST introspection of the compiled expression)
   - For Claude-format APIs (input_tokens is text-only): no adjustment needed

2. `TryTieredSettle(relayInfo, params)`:
   - Uses the frozen `BillingSnapshot` from pre-consume
   - Re-runs the expression with actual token counts
   - Converts via `quotaConversion()` (version-dispatched)
   - Returns actual quota

### 5. Log Display

**Files**: `service/log_info_generate.go`, `web/src/helpers/render.jsx`

Backend: `InjectTieredBillingInfo()` adds `billing_mode`, `expr_b64` (base64 expression), and `matched_tier` to the log's `other` JSON.

Frontend: Detects `billing_mode === "tiered_expr"`, decodes `expr_b64`, parses tiers via shared `parseTiersFromExpr()`, and renders pricing breakdown.

---

## Key Design Decisions

### Token Normalization via AST Introspection

Different upstream APIs report `prompt_tokens` differently:
- **OpenAI/GPT**: `prompt_tokens` = total (text + cache + image + audio)
- **Claude**: `input_tokens` = text only (cache reported separately)

The system normalizes `p` to mean "tokens not separately priced" by subtracting sub-categories **only when the expression references them**. This is determined by walking the compiled AST to find `IdentifierNode` references — zero runtime cost after first compilation (cached).

Example: `p * 2.5 + c * 15 + cr * 0.25`
- Expression uses `cr` → cache read tokens subtracted from `p`
- Expression doesn't use `img` → image tokens stay in `p`, priced at $2.50

### `len` — Context Length Variable

`len` represents the total input context length, designed for **tier condition evaluation** (e.g. `len <= 200000 ? ...`). Unlike `p`, `len` is never reduced by sub-category exclusion.

**Computation rules:**
- **Non-Claude (GPT/OpenAI format)**: `len = prompt_tokens` (the raw total from the upstream response)
- **Claude format**: `len = input_tokens + cache_read_tokens + cache_creation_tokens` (since Claude's `input_tokens` is text-only, cache must be added back to reflect full context length)

This ensures that heavy cache usage doesn't cause the tier condition to incorrectly evaluate to a lower tier. For example, if a request has 300K total context but 250K is cached, `p` with cache subtracted would be only 50K (standard tier), while `len` correctly reports 300K (long-context tier).

### Quota Conversion

Expression coefficients are $/1M tokens. Conversion to internal quota:

```
quota = exprOutput / 1,000,000 * QuotaPerUnit * groupRatio
```

This matches the per-call billing pattern: `quota = modelPrice * QuotaPerUnit * groupRatio`.

### Expression Versioning

Expressions can carry a version prefix: `v1:tier(...)`. No prefix = v1.

Version controls:
- Compile environment (available variables and functions)
- Token normalization logic
- Quota conversion formula

This enables future evolution without breaking existing expressions.

---

## Model pricing management API

Root-only routes under `/api/option/model_pricing` provide GET, PATCH,
POST `/preview`, and POST `/quote`. Prices remain in the existing Option maps;
`billing_setting.plugin_billing_expr` stores `pluginKey::model` overrides.
These routes do not install an executor or activate a task pricing profile.
The first `::` separates the plugin key from the complete model name; plugin
keys cannot contain `::`, while model names can.

A task-only `u()` default cannot replace a shared model default while a channel
advertises that model without a known Go task contract, including disabled or
unverified channels. Saving and previewing such a draft fail before publication;
retain its legacy default and use a plugin override or a distinct task alias.
This is a configuration-time compatibility check, not a task executor or a
guarantee against later incompatible channel/catalog edits. Those edits require
revalidation; diagnosed drift is not permission for paid admission.

PATCH accepts `changes[]` with a model name, `expected_version`,
`expected_contract_digest`, `expected_binding_version`, and `pricing`.
Each pricing object replaces that model's complete configuration; submit only
dirty models but retain their unchanged fields. An omitted field removes the
override; a numeric `0` or expression `"0"` is explicit zero. `reset: true`
restores the compiled model defaults. All changes validate and commit together.
Equality-token conflicts return 409. Tokens include inherited alias/mapped price
dependencies and host schema/binding metadata, and cannot be sorted as revisions.

Preview returns the authoritative base `version` separately from `draft_version`;
it never saves its candidate. Quote selects a server-issued `context_id` and
checks all three base guards. The outer `contract_digest` covers model contexts;
`preview.contract_digest` identifies the single selected fact contract. Group
ratio/floor come from database configuration and quota conversion from the node's
server context; clients cannot supply arbitrary multipliers. The quote exposes
those terms, its evaluation time, selected expression and resulting amounts.

Quotes are configuration samples with `admission_ready: false`, not request
admission, provider validation, reservation-bound certification or actual charges.
They exclude user-specific group overrides. `pricing_at` only controls this
read-only sample; real task submission must freeze host-issued terms independently.
Known Go source contracts are intentionally finite. A plugin schema supports
schema-checked editing but does not establish phase semantics: without a vetted
host phase contract, plugin quoting fails even if its key resembles a known provider.

Writes use an internal coordination row in the existing options table; it is
not another price/version store. Only changed price maps are persisted. Successful
responses distinguish `committed` from `cache_published`; a failed local cache
publication does not roll back the committed database state. After a lost response,
reread authoritative configuration instead of blindly overwriting it. Neither
local cache publication nor a successful save proves immediate multi-node cutover.

Legacy Option/tiered-editor writes share the transaction. Unversioned writes that
would create or change protected task expressions/plugin prices are rejected; unrelated
legacy prices remain editable. Deploy this protection only with the compatible
model-price editor. Read-only preview/quote retain RootAuth and do not create
management/consumption logs or business writes.

The model-price settings page uses **Model and source pricing** for guarded,
per-model editing. It keeps a complete `configured` draft, including unshown
plugin overrides, rather than saving the `effective` projection. Empty numeric
inputs remove overrides; `0` stays explicit. A plugin expression left empty
inherits the default, while `"0"` is a configured zero expression. Stored
effective values describe the saved entry; configuration preview describes a
candidate and never installs an executor. The full-model JSON editor replaces
only that model's draft and requires explicit save.

Copy/reset of selected models uses one atomic PATCH with each model's observed
guards. Dirty target drafts must be resolved before batch replacement. After an
ambiguous write, the editor rereads each model independently without replaying
the PATCH. A lost reset receipt cannot establish its compiled target from an
ordinary read, so it remains unresolved until an explicit operator decision.
Adopting a server snapshot discards local work; it cannot prove an older request
failed or will not finish later. Cache publication failure is shown separately
from an acknowledged database commit. Successful writes invalidate management
and public price queries; this does not certify cache cutover on every node.
Returning from legacy tools rereads the active model without rebasing a dirty
draft. Legacy price writes invalidate guarded price queries as well; settings
refresh preserves dirty fields in the existing legacy forms.

**Legacy price tools** retain the existing token/fixed-price editors and Option
semantics; they do not gain guarded task writes. Protected task expressions and
plugin overrides must use the new per-model entry. Legacy task rate cards,
including structured MiniMax rules, remain separately configured for their
existing execution profiles. They are not automatically converted, added to,
or selected together with the new task-expression profile by this UI.

## File Map

| Layer | Files |
|-------|-------|
| Expression engine | `pkg/billingexpr/compile.go`, `run.go`, `settle.go`, `round.go`, `types.go` |
| Storage | `setting/billing_setting/tiered_billing.go` |
| Pre-consume | `relay/helper/price.go`, `relay/helper/billing_expr_request.go` |
| Settlement | `service/tiered_settle.go`, `service/quota.go` |
| Log injection | `service/log_info_generate.go` |
| Frontend editor | `web/src/pages/Setting/Ratio/components/TieredPricingEditor.jsx` |
| Frontend display | `web/src/helpers/render.jsx`, `web/src/helpers/utils.jsx` |
| Model detail | `web/src/components/table/model-pricing/modal/components/DynamicPricingBreakdown.jsx` |
| Log display | `web/src/hooks/usage-logs/useUsageLogsData.jsx`, `web/src/components/table/usage-logs/UsageLogsColumnDefs.jsx` |
