# MAX API

<div align="center">

![MAX API](./web/default/public/logo.png)

**AI Models and Agents governance, intelligent operations, and open collaboration infrastructure for the AGI application era**

**MAX API 2.0: entering the intelligent operations era · evolving from a unified model gateway to AGI-native governance and operations**

[View the MAX API 2.0 Preview release notes](https://github.com/MAX-API-Next/MAX-API/releases/tag/v2.0.0-smartops.pre1)

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <strong>English</strong> |
  <a href="./README.fr.md">Français</a> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/MAX-API-Next/MAX-API/main/LICENSE">
    <img src="https://img.shields.io/github/license/MAX-API-Next/MAX-API?color=brightgreen" alt="license">
  </a><!--
  --><a href="https://github.com/MAX-API-Next/MAX-API/releases/latest">
    <img src="https://img.shields.io/github/v/release/MAX-API-Next/MAX-API?color=brightgreen&include_prereleases" alt="release">
  </a><!--
  --><a href="https://hub.docker.com/r/cscitechtop/max-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a><!--
  --><a href="https://goreportcard.com/report/github.com/MAX-API-Next/MAX-API">
    <img src="https://goreportcard.com/badge/github.com/MAX-API-Next/MAX-API" alt="GoReportCard">
  </a>
</p>

<p align="center">
  <a href="https://github.com/MAX-API-Next/MAX-API"><strong>⭐ Star the project</strong></a> •
  <a href="#-join-the-max-api-next-community"><strong>💬 Join the community</strong></a> •
  <a href="https://docs.max-api.ai"><strong>📚 Documentation</strong></a> •
  <a href="https://github.com/MAX-API-Next/MAX-API/releases"><strong>🚀 Releases</strong></a>
</p>

<p align="center">
  <a href="#-join-the-max-api-next-community">Community</a> •
  <a href="#-quick-start">Quick start</a> •
  <a href="#-why-max-api">Why MAX API</a> •
  <a href="#-technical-direction-for-agi">AGI direction</a> •
  <a href="#-current-capabilities">Capabilities</a> •
  <a href="#-intelligent-operations-center">SmartOps</a> •
  <a href="#-preview-deployment">Preview deployment</a>
</p>

</div>

---

MAX API sits between applications, Agents, and upstream model services as a unified model gateway, governance control plane, and operations entry point. The MAX-API-Next community builds the project around practical AGI engineering and welcomes developers, researchers, enterprise engineering teams, university technology enthusiasts, and open-source contributors. Our goal is to provide the model access, permissions, cost controls, Evidence, intelligent operations, and safety foundations that real AGI applications require.

In the AGI field, we will **continuously deliver advanced and verifiable technology, turn real production problems into open engineering capabilities, and build a community for shared research and contribution.**

## 🌐 Join the MAX-API-Next community

MAX API is more than a code repository. It is a long-term open collaboration around AI Models, Agents, AgentOps, and AGI engineering. Whether you operate a model gateway, build Agents, adapt new models, research evaluation and governance, or improve documentation and localization, you are welcome to join the community and contribute.

**Join the community to follow releases, model and protocol changes, deployment practices, troubleshooting knowledge, and opportunities to build together.**

<p align="center">
  <strong>QQ group: 950126533</strong> •
  <strong>WeChat group: search for MAX-API</strong>
</p>

| Community entry | Purpose |
|---|---|
| [MAX-API-Next on GitHub](https://github.com/MAX-API-Next) | Follow community projects, technical direction, and open collaboration |
| [MAX API Issues](https://github.com/MAX-API-Next/MAX-API/issues) | Report reproducible problems, propose improvements, and share compatibility changes |
| [MAX API Releases](https://github.com/MAX-API-Next/MAX-API/releases) | Follow stable and Preview releases |
| [Ask DeepWiki](https://deepwiki.com/MAX-API-Next/MAX-API) | Search and understand the codebase quickly |
| Technical and ecosystem cooperation | Contact `maxapi@max-api.ai` |

### Contributors we are looking for

- **Model and protocol contributors**: adapt new models, providers, Reasoning behavior, tool calling, and multimodal task protocols.
- **Agent and application developers**: share integration and governance practices for Dify, RAGFlow, Codex, MCP, workflows, and research Agents.
- **Reliability and security engineers**: strengthen cross-database tests, asynchronous task recovery, settlement safety, cache consistency, and permission boundaries.
- **Documentation and community contributors**: improve deployment guides, FAQs, examples, translations, reproducible experiments, and contributor onboarding.
- **Researchers and evaluation developers**: help build Evidence, Evaluators, Detectors, Runbooks, and controlled-autonomy designs.

<details>
<summary><strong>View collaboration areas, principles, and ways to participate</strong></summary>

| Collaboration area | Example contributions |
|---|---|
| Model and provider ecosystem | Model/protocol adapters, compatibility tests, model-list and deprecation changes, channel templates |
| AgentOps and AGI application governance | Agent integration examples, token and permission boundaries, cost governance, operational practices |
| SmartOps and Evidence | Sanitized incident samples, metric definitions, alert rules, data-quality notes, diagnosis and evaluation methods |
| Reliability, billing, and security | Idempotent settlement, asynchronous recovery, cross-database regression tests, cache consistency, high-risk operation verification |
| Documentation, tutorials, and localization | Deployment tutorials, architecture docs, FAQs, case studies, translations, contributor guides |

Please provide a minimal reproduction, version, environment, sanitized logs, or test evidence whenever possible. Protocol, database, and user-contract changes should document compatibility, migration, and rollback paths. Do not submit real API keys, customer data, raw payment records, private logs, or unauthorized data. Contributions involving money, security, credentials, production data, or automated execution require stricter testing, independent review, and a clearly accountable owner.

You can start by:

1. Reporting a reproducible problem, protocol difference, or model compatibility change.
2. Adding a failing test, cross-database case, frontend regression, or sanitized Evidence to an existing issue.
3. Improving model/channel configuration, deployment instructions, FAQs, architecture documentation, or translations.
4. Sharing sanitized Agent integration, cost governance, SmartOps, or private deployment practices.
5. Proposing an Evaluator, Detector, Runbook, or controlled-autonomy design with explicit capability and risk boundaries.

</details>

Companion documentation: [MAX-API-Docs](https://github.com/MAX-API-Next/MAX-API-Docs) (deployment, configuration, and usage guides).

Model providers, Agent/workflow projects, open-source communities, researchers, and enterprise engineering teams are welcome to collaborate. Formal partnerships, joint announcements, or use of an institution's name require explicit authorization from both parties.

> [!IMPORTANT]
> When offering generative AI services to the public, operators are responsible for upstream authorization, registration or licensing, content safety, identity verification, log retention, taxation, payments, user agreements, and any other obligations required in their jurisdiction. Enable log auditing and content retention only with a lawful basis, clear notice, permission isolation, and appropriate data-security controls.

<img width="1902" height="1031" alt="MAX API administration console" src="https://github.com/user-attachments/assets/fa481602-1e75-4326-9275-3c8271d01f5b" />

## 🧠 Technical direction for AGI

AGI applications will not depend forever on one model, one protocol, or one-shot requests. They require cross-model reasoning, tool use, multimodal tasks, long-running execution, cost constraints, production Evidence, and recoverable failure handling. MAX API starts from these verifiable engineering problems to build an AI Models and Agents governance foundation.

| Technical direction | Current foundation | Value for AGI applications |
|---|---|---|
| Multi-model and multi-protocol access | OpenAI-Compatible APIs, Responses, Claude Messages, Gemini, Realtime, multimodal and asynchronous task protocols | Gives applications and Agents a relatively stable entry point while the model ecosystem continues to change |
| Reasoning and tool-context compatibility | Reasoning Effort, tool definitions, Tool Calls, tool-response association, and multi-turn context conversion | Reduces loss of reasoning information and tool semantics when switching providers |
| Governance control plane | Users, tokens, model scopes, groups, routing, rate limits, quotas, pricing, and administrator permissions | Creates separate identity, access, and budget boundaries for different Agents, environments, and tasks |
| Recoverable billing and task lifecycle | Pre-charge, final settlement, idempotent records, failed-task refunds, asynchronous polling, and manual reconciliation states | Prevents duplicate charges, incorrect refunds, and untraceable results during long tasks, retries, and failure windows |
| Evidence and intelligent operations | Logs, errors, retries, performance buckets, active alerts, channel/model performance, and settlement evidence | Grounds diagnosis, evaluation, and future Agent recommendations in traceable facts rather than prompt-only inference |
| Security and organization governance | Passkeys, 2FA, scoped re-verification, session revocation, audit, and rate limits for sensitive operations | Preserves explicit accountability around high-risk configuration, credentials, and operations |
| Controlled autonomy and engineering evolution | **Long-term blueprint**: Policy, Budget, Approval, Shadow, Canary, Rollback, and isolated Coding Workspaces | Requires automation to be evaluated, constrained, and audited before it can enter low-risk production actions |

### Technical principles

- **Evidence before Action**: establish verifiable facts before diagnosis, recommendations, or automated actions.
- **Governance before Autonomy**: identity, permissions, budgets, approval, audit, and rollback must precede autonomous capabilities.
- **One Billing Truth**: production billing, quota, and settlement keep a single source of truth; Agents and plugins do not create a second accounting system.
- **Compatibility by Design**: continue to support SQLite, MySQL, PostgreSQL, multiple provider protocols, and portable application contracts.
- **Open Collaboration, Safe Boundaries**: open protocol adapters, tests, documentation, evaluations, and governance designs while keeping high-risk production, financial, credential, and release actions under explicit approval.

### MAX API 2.0 technical highlights

- **Evidence-driven SmartOps**: brings resource alerts, channel/model performance, data-quality states, and billing-settlement evidence into one operations entry point while preserving the boundary between human review and real financial state.
- **Continuous support for advanced model protocols**: improves Reasoning, cache keys, penalty parameters, tool definitions, and multi-turn Tool Context compatibility across Responses, Claude Messages, Gemini, and Ollama.
- **Recoverable billing and asynchronous task semantics**: uses idempotent settlement, durable effects, task IDs, and explicit pending/manual states to prevent retries from causing duplicate charges, incorrect refunds, or lost tasks.
- **Scoped re-verification for high-risk operations**: Passkeys, 2FA, Telegram, API Tokens, and session revocation use scope-bound step-up verification, while `session_generation` invalidates old sessions promptly.
- **Cross-database support and continuous verification**: core data paths support SQLite, MySQL, and PostgreSQL; Go tests, frontend Bun tests, TypeScript type checks, JSON-wrapper rules, and synchronized test mirrors form the release gate.

## 💡 Why MAX API

| Dimension | Connecting directly to multiple providers | Using MAX API |
|---|---|---|
| Application integration | Maintain separate SDKs, protocols, authentication, and error formats | Use a relatively stable application-side entry point |
| Model switching | Change code, credentials, and deployment configuration | Adjust channels, model mappings, groups, and routing |
| Availability | Each application implements its own retry and upstream-failure handling | Configure weights, priorities, retries, and failover centrally |
| Permissions and credentials | Credentials are scattered across applications and environment variables | Manage tokens, model scopes, quotas, and expiration centrally |
| Cost accounting | Provider bills are fragmented and difficult to attribute | Track usage by user, token, model, channel, and group |
| Troubleshooting | Logs are fragmented across providers | Observe requests, errors, retries, and latency at the gateway |

In one sentence: **providers supply the models, Agent frameworks orchestrate the application, and MAX API provides unified access while enforcing governance boundaries.**

## 🚀 Quick start

The local experience uses SQLite by default and requires only Docker:

```bash
MAX_API_IMAGE=cscitechtop/max-api:latest@sha256:006d5d86887a261baab4d71ec3797d429e3771a4836e5899734aee0e7f66f2ab

docker pull "$MAX_API_IMAGE"

docker run --name max-api -d --restart always -p 127.0.0.1:3000:3000 -e TZ=Asia/Shanghai -v ./data:/data "$MAX_API_IMAGE"
```

Open <http://localhost:3000>, then:

1. Create or confirm the administrator account.
2. Add an upstream channel and API key that you are legally authorized to use.
3. Create an access token and point your application's Base URL to MAX API.

> [!TIP]
> Use stable releases in production. Preview releases are intended for testing and staged validation. Back up the database and prepare a rollback plan before upgrading.
>
> [!WARNING]
> SQLite is suitable for local evaluation, development, and small-scale tests. For production, use MySQL or PostgreSQL versions that remain within the vendor's security-support lifecycle (MySQL 8.4 LTS and PostgreSQL 14+ are recommended), together with Redis, HTTPS, backups, and recovery procedures. Compatibility minimums remain MySQL ≥ 5.7.8 and PostgreSQL ≥ 9.6, but those versions are not recommended for production.

## ✨ Current capabilities

The following capabilities are available in the current system:

| Capability | Primary use |
|---|---|
| Unified model entry point | Connect OpenAI-Compatible APIs, Responses, Claude Messages, Gemini, Realtime, and multimodal task interfaces |
| Multi-provider routing | Manage channels, weights, priorities, groups, model mappings, retries, and cross-provider failover |
| Identity and access control | Manage users, tokens, model scopes, groups, quotas, expiration, rate limits, and administrator permissions |
| Cost and billing | Support multipliers, fixed pricing, expression billing, asynchronous task rate-cards, pre-charge, settlement, and failed-task refunds |
| Logs and audit | Inspect usage, errors, retries, and administrative operations by user, token, model, channel, group, and node |
| Intelligent Operations Center | Review active alerts, channel performance, model performance, system information, and billing-settlement reconciliation evidence |
| Private deployment | Support SQLite, MySQL, PostgreSQL, Redis, multiple nodes, and a separate log database |
| Upstream extensibility | Support protocol adapters, path overrides, parameter/Header overrides, model discovery, and task-status mappings |

### Use cases

- **Internal model gateway for a team or organization**: manage users, tokens, models, providers, permissions, and costs centrally.
- **Runtime foundation for AI applications and Agents**: provide model access control, cost attribution, and failure diagnosis for applications, Agents, and workflows.
- **Multi-provider resilience and migration**: reduce dependence on a single upstream through model mapping, weighted routing, retries, and staged switching.
- **Multimodal task governance**: manage image, audio, video, embedding, reranking, and realtime conversation interfaces.
- **Private and compliant operations**: retain control of credentials, data, logs, audit, pricing, and deployment environments.

## 🩺 Intelligent Operations Center

**The Intelligent Operations Center is a major MAX API 2.0 update and a key step from a unified model gateway toward AGI-native governance and operations infrastructure.**

It consolidates production observation, resource alerts, model and channel performance, system information, and billing-settlement reconciliation in one administrator entry point. The current capability focuses on seeing problems, preserving evidence, notifying administrators, and enabling controlled review. It is not an autonomous Agent that automatically changes channels, routing, balances, or hosts.

| Module | What it currently provides |
|---|---|
| Active alerts | Deduplicated alerts when CPU, memory, or disk on the current node remains above a threshold, plus recovery notifications; reuses existing administrator Email, Webhook, Bark, or Gotify configuration |
| Channel performance | Requests and errors, consumed quota, estimated success rate, log latency, retries, probe latency, and latest observation time; detail view shows the channel's model and group performance over the last 24 hours |
| Model performance | Aggregated channel count, requests and errors, consumed quota, estimated success rate, log latency, throughput, and retries; detail view includes group performance, latency trends, and availability trends |
| Billing-settlement reconciliation | Shows `pending` / `manual` positive final settlements, unsettled funds, retry state, and error evidence; root administrators can configure the default user-blocking policy, and administrators can atomically review batches by `id + revision` and close alerts |
| System information | Shows nodes, running instances, system tasks, and related information; this module continues to require super-administrator permission |

The Active Alerts page reads current alert state every five seconds but does not trigger new detection or remediation. Channel and model lists query the latest hour by default and accept a custom `1–168` hour window. They do not continuously rescan large log databases: queries run only when the administrator selects “Apply filters” or “Refresh,” and detail data loads on demand.

Billing-settlement reconciliation strictly separates financial recovery state from operational alert state. “Review and close” records the administrator review and closes the current alert; it does not mark a settlement as `applied` and does not change balances, applied deltas, or effect state. Batch review is bound to the current financial revision. If a record changes after refresh, the old selection becomes invalid, preventing action based on stale evidence.

> [!NOTE]
> Current production-performance views primarily aggregate existing Consume/Error logs and `perf_metrics`. Estimated success rate is not a complete Relay Attempt success rate, while throughput and trends are performance-bucket approximations. When logging is disabled, historical data is missing, collection is disabled, a window has no samples, or a query fails, the UI displays the corresponding data-quality state.
>
> Active alerts depend on performance monitoring and resource-threshold configuration. A threshold of `0` disables that resource alert, and two consecutive valid samples are required before an alert fires. Alert state and the notification queue exist only in the current process memory: they do not survive a restart, and multiple nodes do not merge them into a cross-node Incident. Channel, model, and system observation remains read-only. Settlement review only updates review metadata and the user-blocking policy; it does not perform financial settlement. The Intelligent Operations Center does not automatically test, disable, reweight, switch channels, or repair hosts.

The value of this stage is to complete the loop of seeing problems, notifying administrators, presenting evidence, and enabling controlled review—creating a foundation for unified Evidence, diagnostic Agents, Evaluators, and controlled automation.

## 🔌 Models, interfaces, and extensibility

> Actual availability depends on your upstream authorization, channel configuration, model mapping, and provider support. MAX API governs these capabilities; it does not provide model services itself.

| Category | Interface or capability |
|---|---|
| General model interfaces | Chat Completions, Responses, Embeddings, Rerank, Images, Audio, and Video |
| Native and realtime protocols | Claude Messages, Google Gemini, OpenAI Realtime, and related entry points |
| Reasoning and tool calling | Reasoning Effort, function tools, Tool Call IDs, tool names, and multi-turn tool-response association, with protocol conversion based on upstream capability |
| Asynchronous tasks | Task submission, polling, status mapping, result proxying, and parameterized billing |
| Custom upstreams | Base URL, path, parameter, Header, status-field, and result-field mappings |

Coverage includes OpenAI, Claude, Gemini, Azure, AWS Bedrock, Vertex AI, Ollama, and multiple domestic model platforms. MAX API can also govern Codex, Dify, RAGFlow, and multimodal task services. Exact support depends on the current release and channel type.

### System flow and technology stack

![MAX API system architecture](./docs/images/MAX-API架构图.png)

```text
Application / SDK / Agent
  → Unified interface and identity authentication
  → Model permissions, rate limits, budgets, and security checks
  → Channel selection, mapping, and failure retry
  → Upstream protocol adaptation
  → Recoverable settlement, Evidence, logging, and audit
  → Intelligent Operations Center and administrator governance
```

The backend uses Go, Gin, and GORM. The frontend uses React 19, TypeScript, Base UI, and Tailwind CSS. The data layer supports SQLite, MySQL, and PostgreSQL, with optional Redis and a separate log database. Provider protocol adapters live in a dedicated Relay/Channel layer, billing and settlement remain inside a unified service boundary, and SmartOps presents read-only observation and constrained governance entry points.

## 🛡️ Governance and operations

For production, configure the system in this order:

1. Configure login, security limits, and user-registration policy.
2. Add legally authorized upstream channels and confirm model, capability, and protocol configuration.
3. Configure groups, tokens, model scopes, quotas, and prices by team, business, or environment.
4. Use a separate token for each application, Agent, or environment to avoid shared credentials and ambiguous cost attribution.
5. Configure retries, logs, and alerts, then continuously observe the system through dashboards and the Intelligent Operations Center.

For channel capability validation, expression billing, generic task protocols, administrator audit, and performance settings, see the [documentation](https://docs.max-api.ai).

## 🧭 Evolution roadmap

MAX API will continue to make **AI Models and Agents governance** its core. Starting from the unified gateway, intelligent operations, and recoverable settlement, it will gradually build Evidence, evaluation, policy, and controlled-execution capabilities for AGI applications. The long-term direction is not to let an unbounded Agent control production, but to create an engineering loop that is verifiable, approvable, stoppable, and reversible.

| Stage | Status | Focus |
|---|---|---|
| Unified gateway and intelligent operations | **Available now** | Access, authentication, routing, billing, logging, resource alerts, channel/model performance, system information, and billing-settlement reconciliation |
| Evidence fact layer | **Near-term development** | Unify model requests, system logs, metrics, Tasks, routing, policies, settlement, and audit events behind sanitized, permission-limited, read-only Agent interfaces |
| Open evaluation and governance templates | **Planned** | Build model/protocol compatibility tests, anonymized incident samples, evaluation sets, Runbooks, Detectors, and industry governance templates with the community |
| Controlled autonomous operations | **Long-term blueprint** | Evaluate low-risk automated actions under Policy, Budget, Approval, Shadow, Canary, and Rollback constraints |
| Controlled capability evolution | **Long-term blueprint** | Generate, test, and review candidate improvements inside isolated Coding Workspaces without directly modifying production |
| AGI engineering loop | **Long-term direction** | Connect Evidence, evaluation, governance policies, human approval, and reversible execution into a verifiable loop |

MAX API is not a foundation model and does not claim to have already achieved AGI or autonomous operations. Long-term capabilities will be validated gradually only after Evidence, permission, budget, approval, and rollback boundaries are in place.

## 🚢 Preview deployment

The following steps are for Preview testing and staged validation. For production, use a stable release and keep the same verification and rollback process.

Docker Compose is recommended:

```bash
MAX_API_VERSION=v2.0.0-smartops.pre1
MAX_API_COMMIT=b7096156549edc930ca244891afb1ba5632dbe8f
git clone --branch "$MAX_API_VERSION" --depth 1 https://github.com/MAX-API-Next/MAX-API.git
cd MAX-API

actual_commit="$(git rev-parse HEAD)"
if [ "$actual_commit" != "$MAX_API_COMMIT" ]; then
  echo "Unexpected commit: $actual_commit" >&2
  exit 1
fi

# Update database and Redis passwords and configure secrets in docker-compose.yml
docker compose up -d
```

### Deployment checklist

| Component | Recommendation |
|---|---|
| Database | Use a MySQL or PostgreSQL release still covered by vendor security support (MySQL 8.4 LTS and PostgreSQL 14+ recommended), with backup and recovery configured |
| Cache | In-memory cache is suitable for one node; Redis is recommended for multiple nodes |
| Entry point | Configure an HTTPS reverse proxy, request-size limits, and trusted-network policy |
| Secrets | Set a random `SESSION_SECRET` explicitly; `CRYPTO_SECRET` is an optional override that falls back to `SESSION_SECRET` when unset, and must be shared across Redis/multi-node deployments when set |
| Nodes | Give every node a stable and unique `NODE_NAME` |
| Logging | Configure `LOG_SQL_DSN`, cleanup, and retention based on compliance and operational requirements |

Multi-node deployments must share `SESSION_SECRET` while using a different `NODE_NAME` for each node. `CRYPTO_SECRET` is an optional override that falls back to `SESSION_SECRET` when unset; if explicitly set, use the same value on every node. Use `LOG_SQL_DSN` for a separate log database and enable `ERROR_LOG_ENABLED` when error-performance statistics are needed. See the [documentation](https://docs.max-api.ai) for complete environment variables and source-build instructions.

## 🤝 Legal notes and derivative use

If you create or distribute a derivative version, read [NOTICE](./NOTICE) and [LICENSE](./LICENSE) in full and preserve the legal notices, attributions, original-project link, and change marking required there.

For a self-use derivative, meeting the display and attribution requirements in NOTICE may qualify you for the applicable non-perpetual temporary commercial license announced by the project, without a separate application or approval. That permission covers only material the MAX API project maintainers have the right to license and does not replace or waive any applicable upstream obligations.

## 📜 License

This project is licensed under the [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE).

The temporary commercial license covers only additions and modifications that the MAX API project maintainers have the right to license. It does not replace or waive any applicable upstream licensing obligations.

If you modify this project and provide it to users over a network, please understand and comply with AGPLv3 source-availability obligations. For a long-term commercial license, institutional cooperation, or other licensing questions, contact maxapi@max-api.ai.

---

<div align="center">

### 💖 Thank you for using MAX API

If this project helps you, please consider giving it a ⭐ Star, following Releases, reporting a reproducible Issue, or joining the MAX-API-Next community.

**[Project repository](https://github.com/MAX-API-Next/MAX-API)** • **[Contribute](https://github.com/MAX-API-Next/MAX-API/issues)** • **[Latest releases](https://github.com/MAX-API-Next/MAX-API/releases)** • **[MAX-API-Next community](https://github.com/MAX-API-Next)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
