# MAX API

<div align="center">

![MAX API](./web/default/public/logo.png)

**面向 AGI 应用时代的 AI Models 与 Agents 治理、智能运维和开放协作基础设施**

**MAX API 2.0：开启智能运维时代 · 从统一模型网关走向 AGI 原生治理与运营**

[查看 MAX API 2.0 Preview 发布说明](https://github.com/MAX-API-Next/MAX-API/releases/tag/v2.0.0-preview.1)

<p align="center">
  <strong>简体中文</strong> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.en.md">English</a> |
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
  <a href="https://github.com/MAX-API-Next/MAX-API"><strong>⭐ Star 项目</strong></a> •
  <a href="#-加入-max-api-next-社区"><strong>💬 加入社群</strong></a> •
  <a href="https://docs.max-api.ai"><strong>📚 查看文档</strong></a> •
  <a href="https://github.com/MAX-API-Next/MAX-API/releases"><strong>🚀 获取版本</strong></a>
</p>

<p align="center">
  <a href="#-加入-max-api-next-社区">加入社区</a> •
  <a href="#-快速开始">快速开始</a> •
  <a href="#-为什么需要-max-api">为什么需要</a> •
  <a href="#-面向-agi-的技术方向">AGI 技术方向</a> •
  <a href="#-当前能力">当前能力</a> •
  <a href="#-智能运维中心">智能运维</a> •
  <a href="#-生产部署">生产部署</a>
</p>

</div>

---

MAX API 位于应用、Agent 与上游模型服务之间，是统一的模型网关、治理控制平面和运营入口。MAX-API-Next 社区围绕 AGI 工程化持续建设本项目，并面向开发者、研究者、企业工程团队、高校技术爱好者和开源贡献者开放协作。我们的目标是建设 AGI 应用真正需要的模型接入、权限、成本、Evidence、智能运维和安全控制基础。

我们将在 AGI 领域：**持续交付先进而可验证的技术、将真实生产问题沉淀为开放工程能力、建立能够共同研究和贡献的社区。**

## 🌐 加入 MAX-API-Next 社区

MAX API 不只是一个代码仓库，也是一项面向 AI Models、Agents、AgentOps 与 AGI 工程化的长期开放协作。无论你正在部署模型网关、开发 Agent、适配新模型、研究评测与治理，还是愿意改进文档和国际化，都欢迎加入社群交流并参与共建。

**加入社群，你可以更快获取版本动态、模型与协议变化、部署经验、问题排查线索和社区共建机会。**

<p align="center">
  <strong>QQ 群：950126533</strong> •
  <strong>微信群：搜索 MAX-API</strong>
</p>

| 社区入口 | 用途 |
|---|---|
| [MAX-API-Next GitHub](https://github.com/MAX-API-Next) | 关注社区项目、技术方向与开放协作 |
| [MAX API Issues](https://github.com/MAX-API-Next/MAX-API/issues) | 提交可复现问题、需求建议和兼容性变化 |
| [MAX API Releases](https://github.com/MAX-API-Next/MAX-API/releases) | 获取正式版与 Preview 版本动态 |
| [Ask DeepWiki](https://deepwiki.com/MAX-API-Next/MAX-API) | 快速检索和理解项目代码 |
| 技术与生态合作 | 联系 `maxapi@max-api.ai` |

### 我们正在寻找这些共建者

- **模型与协议贡献者**：适配新模型、新供应商、Reasoning、工具调用和多模态任务协议。
- **Agent 与应用开发者**：分享 Dify、RAGFlow、Codex、MCP、工作流和研究 Agent 的接入与治理实践。
- **可靠性与安全工程师**：完善跨数据库测试、异步任务恢复、结算安全、缓存一致性和权限边界。
- **文档与社区贡献者**：改进部署教程、FAQ、案例、翻译、可复现实验和新贡献者指引。
- **研究者与评测开发者**：共建 Evidence、Evaluator、Detector、Runbook 和受控自治方案。

<details>
<summary><strong>查看共建方向、协作原则与参与方式</strong></summary>

| 共建方向 | 适合的贡献 |
|---|---|
| 模型与供应商生态 | 模型/协议适配、兼容测试、模型列表与弃用变化、渠道配置模板 |
| AgentOps 与 AGI 应用治理 | Agent 接入示例、令牌与权限边界、成本治理和运行实践 |
| SmartOps 与 Evidence | 脱敏故障样本、指标口径、告警规则、数据质量说明、诊断与评测方法 |
| 可靠性、计费与安全 | 幂等结算、异步任务恢复、跨数据库回归、缓存一致性和高风险操作验证 |
| 文档、教程与国际化 | 部署教程、架构说明、FAQ、案例、翻译和新贡献者指引 |

协作时请尽量提供最小复现、版本、环境、脱敏日志或测试证据；协议、数据库和用户契约变更需要说明兼容性、迁移与回滚路径。请勿提交真实 API Key、客户数据、支付原始记录、私有日志或未经授权的数据。涉及资金、安全、密钥、生产数据或自动执行的贡献，需要更严格的测试、独立审查和明确责任人。

你可以从以下方式开始：

1. 提交一个可复现的问题、协议差异或模型兼容性变化。
2. 为已有问题补充失败测试、跨数据库用例、前端回归或脱敏 Evidence。
3. 完善模型/渠道配置、部署说明、FAQ、架构文档或多语言翻译。
4. 分享经过脱敏的 Agent 接入、成本治理、SmartOps 和私有化部署实践。
5. 提交 Evaluator、Detector、Runbook 或受控自治设计提案，并明确能力边界与风险。

</details>

配套文档：[MAX-API-Docs](https://github.com/MAX-API-Next/MAX-API-Docs)（部署、配置与使用说明）。

欢迎模型厂商、Agent/工作流项目、开源社区、研究者和企业工程团队开展技术共建。正式合作、联合发布或机构名义使用应经过双方明确授权。

> [!IMPORTANT]
> 面向公众提供生成式人工智能服务时，使用者应自行完成所在司法辖区要求的上游授权、备案许可、内容安全、实名、日志留存、税务、支付和用户协议等合规义务。日志审计和内容留存仅应在具备合法依据、明确告知、权限隔离和数据安全措施的场景下启用。

<img width="1902" height="1031" alt="image" src="https://github.com/user-attachments/assets/fa481602-1e75-4326-9275-3c8271d01f5b" />

## 🧠 面向 AGI 的技术方向

AGI 应用不会长期依赖单一模型、单一协议或一次性请求。它们需要跨模型推理、工具调用、多模态任务、长期运行、成本约束、生产证据和可恢复的失败处理。MAX API 选择从这些可验证的工程问题出发，建设 AI Models and Agents governance 基础。

| 技术方向 | 当前基础 | 对 AGI 应用的价值 |
|---|---|---|
| 多模型与多协议接入 | OpenAI Compatible、Responses、Claude Messages、Gemini、Realtime、多模态与异步任务协议 | 让应用和 Agent 面对相对稳定的入口，并能随模型生态变化持续迁移 |
| 推理与工具上下文兼容 | Reasoning Effort、工具定义、Tool Call、工具响应关联和多轮上下文转换 | 减少跨供应商切换时推理信息与工具调用语义丢失 |
| 治理控制平面 | 用户、令牌、模型范围、分组、路由、限流、额度、价格和管理员权限 | 为不同 Agent、环境和任务建立独立身份、访问边界与预算边界 |
| 可恢复计费与任务生命周期 | 预扣、最终结算、幂等记录、失败退款、异步任务轮询和人工对账状态 | 避免长任务、重试和异常窗口造成重复扣费、错误退款或不可追溯结果 |
| Evidence 与智能运维 | 日志、错误、重试、性能桶、活动告警、渠道/模型性能和结算证据 | 让诊断、评测和未来 Agent 建议建立在可追溯事实上，而不是仅依赖提示词推断 |
| 安全与组织治理 | Passkey、2FA、分作用域重新验证、会话撤销、审计和敏感操作限流 | 为高风险配置、凭据和运营操作保留明确的责任边界 |
| 受控自治与工程进化 | **长期蓝图**：Policy、Budget、Approval、Shadow、Canary、Rollback、隔离 Coding Workspace | 让自动化能力先被评测、约束和审计，再逐步进入低风险生产动作 |

### 技术原则

- **Evidence before Action**：先建立可验证事实，再进行诊断、建议或自动动作。
- **Governance before Autonomy**：身份、权限、预算、审批、审计和回滚必须先于自治能力。
- **One Billing Truth**：生产计费、额度与结算保持单一事实来源，不为 Agent 或插件建立第二套账务逻辑。
- **Compatibility by Design**：持续支持 SQLite、MySQL、PostgreSQL、多供应商协议和可迁移的应用侧契约。
- **Open Collaboration, Safe Boundaries**：开放协议适配、测试、文档、评测和治理方案，高风险生产、资金、密钥与发布动作继续由明确责任人审批。

### MAX API 2.0 技术亮点

- **Evidence-driven SmartOps**：把资源告警、渠道/模型性能、数据质量状态和计费结算证据汇聚到统一运维入口，并保留人工审阅与真实财务状态之间的边界。
- **面向先进模型协议持续演进**：增强 Responses、Claude Messages、Gemini 与 Ollama 的 Reasoning、缓存键、惩罚参数、工具定义和多轮 Tool Context 兼容。
- **可恢复的计费与异步任务语义**：通过幂等 settlement、持久化 effect、任务 ID 和明确的 pending/manual 状态，避免异常重试导致重复扣费、错误退款或任务丢失。
- **高风险操作分作用域重新验证**：Passkey、2FA、Telegram、API Token 与会话撤销采用 scope-bound step-up verification，并通过 `session_generation` 及时撤销旧会话。
- **跨数据库与持续验证**：核心数据路径兼容 SQLite、MySQL 和 PostgreSQL；Go 测试、前端 Bun 测试、TypeScript 类型检查、JSON 包装规则和测试镜像同步共同构成发布门禁。

## 💡 为什么需要 MAX API

| 维度 | 直接连接多个供应商 | 使用 MAX API |
|---|---|---|
| 应用接入 | 分别维护 SDK、协议、鉴权和错误格式 | 应用侧使用相对稳定的统一入口 |
| 模型切换 | 修改代码、密钥和部署配置 | 通过渠道、模型映射、分组和路由调整 |
| 可用性 | 每个应用自行处理重试和上游故障 | 集中配置权重、优先级、失败重试和切换 |
| 权限与密钥 | 凭据散落在应用和环境变量中 | 集中管理令牌、模型范围、额度和有效期 |
| 成本核算 | 多家账单分散，难以归因 | 按用户、令牌、模型、渠道和分组统计 |
| 故障排查 | 日志分散，跨供应商定位困难 | 在网关侧统一观察请求、错误、重试和耗时 |

一句话概括：**供应商负责提供模型，Agent 框架负责编排业务，MAX API 负责统一接入并守住治理边界。**

## 🚀 快速开始

本地体验默认使用 SQLite，只需 Docker：

```bash
docker pull cscitechtop/max-api:latest

docker run --name max-api -d --restart always -p 3000:3000 -e TZ=Asia/Shanghai -v ./data:/data cscitechtop/max-api:latest
```

启动后访问：<http://localhost:3000>

接下来完成三件事：

1. 创建或确认管理员账号。
2. 添加一个具有合法授权的上游渠道和 API Key。
3. 创建访问令牌，把应用中的 Base URL 指向 MAX API。

> [!TIP]
> 生产环境建议使用正式版。Preview 版本用于测试和灰度验证，升级前请备份数据库并准备回滚方案。
>
> [!WARNING]
> SQLite 适合本地体验、开发和小规模测试。正式环境建议使用仍在供应商安全支持周期内的 MySQL（建议 8.4 LTS）或 PostgreSQL（建议 14+），并配置 Redis、HTTPS、备份和恢复方案。项目兼容性下限仍为 MySQL ≥ 5.7.8、PostgreSQL ≥ 9.6，但不建议将其用于生产。

## ✨ 当前能力

以下能力已经在当前系统中提供：

| 能力 | 主要用途 |
|---|---|
| 统一模型入口 | 接入 OpenAI Compatible、Responses、Claude Messages、Gemini、Realtime 和多模态任务接口 |
| 多供应商路由 | 管理渠道、权重、优先级、分组、模型映射、失败重试和跨供应商切换 |
| 身份与访问控制 | 管理用户、令牌、模型范围、分组、额度、有效期、限流和管理员权限 |
| 成本与计费 | 支持倍率、固定价格、表达式计费、异步任务 rate-card、预扣费、结算和失败退款 |
| 日志与审计 | 按用户、令牌、模型、渠道、分组和节点查看使用、错误、重试和管理操作 |
| 智能运维中心 | 集中查看活动告警、渠道性能、模型性能、系统信息和计费结算对账证据，辅助管理员发现、定位和审阅生产问题 |
| 私有化部署 | 支持 SQLite、MySQL、PostgreSQL、Redis、多节点和独立日志库 |
| 上游扩展 | 支持协议适配、路径覆盖、参数/Header 覆盖、模型发现和任务状态映射 |

### 适用场景

- **团队或组织内部模型网关**：统一管理用户、令牌、模型、供应商、权限和费用。
- **AI 应用与 Agent 运行底座**：为应用、Agent 和工作流提供模型访问控制、成本归因与异常定位。
- **多供应商容灾与迁移**：通过模型映射、加权路由、失败重试和灰度切换降低单一上游风险。
- **多模态任务治理**：统一管理图像、音频、视频、嵌入、重排序和实时对话接口。
- **私有化与合规运营**：自主管理密钥、数据、日志、审计、价格和部署环境。

## 🩺 智能运维中心

**智能运维中心是 MAX API 2.0 的重大更新，也是项目从统一模型网关走向 AGI 原生治理与运营基础设施的关键一步。**

它将生产观测、资源告警、模型与渠道性能、系统信息和计费结算对账集中到统一管理员入口。当前能力强调“看见问题、保留证据、通知管理员、受控审阅”，并不是会自动修改渠道、路由、余额或主机的自治 Agent。

| 模块 | 当前提供的内容 |
|---|---|
| 活动告警 | 对当前节点的 CPU、内存和磁盘持续超阈值进行去重告警，在恢复时发送恢复通知；复用管理员已有的 Email、Webhook、Bark 或 Gotify 配置 |
| 渠道性能 | 查看请求与错误、消耗额度、估算成功率、日志延迟、重试、探测延迟和最近观测时间；详情可查看该渠道最近 24 小时的模型与分组表现 |
| 模型性能 | 汇总所有模型的渠道数、请求与错误、消耗额度、估算成功率、日志延迟、吞吐量和重试；详情提供各分组性能、延迟趋势和可用率趋势 |
| 计费结算对账 | 展示 `pending` / `manual` 正向最终结算、未结资金、重试和错误证据；根管理员可配置默认用户阻断策略，管理员可按 `id + revision` 原子批量审阅并关闭告警 |
| 系统信息 | 查看节点、运行实例和系统任务等信息；该模块继续要求超级管理员权限 |

活动告警页面每 5 秒读取一次当前告警状态，但不会触发新的检测或修复动作。渠道与模型列表默认查询最近 1 小时，管理员可以输入 `1–168` 小时的自定义窗口；它们不会自动反复统计大日志库，只有点击“应用筛选”或“刷新”时才执行查询，详情数据在打开时按需加载。

计费结算对账把财务恢复状态与运维告警状态严格分离：“审阅并关闭”只记录管理员审阅并关闭当前告警，不会把结算标记为 `applied`，不会修改余额、已应用差额或 effect 状态。批量审阅绑定当前财务 revision；刷新后记录发生变化时，旧选择会自动失效，避免使用过期证据作出操作。

> [!NOTE]
> 当前生产性能主要聚合既有 Consume/Error 日志和 `perf_metrics`：估算成功率并非完整的 Relay Attempt 成功率，吞吐量与趋势属于性能桶级近似值。日志关闭、历史数据缺失、采集关闭、窗口无样本或查询失败时，页面会显示相应的数据质量状态。
>
> 活动告警依赖性能监控和资源阈值配置；阈值设为 `0` 时表示关闭该资源告警，需连续两个有效样本才会触发。状态与通知队列只保存在当前进程内存中：进程重启后不会保留，多节点也不会自动合并为跨节点 Incident。渠道、模型和系统观测保持只读；结算审阅仅更新审阅元数据与用户阻断策略，不执行资金结算。智能运维中心不会自动测试、禁用、调权、切换渠道或修复主机。

这一阶段的价值，是先完成“看见问题、通知管理员、提供证据、受控审阅”的闭环，为后续统一 Evidence、诊断 Agent、Evaluator 和受控自动化建立基础。

## 🔌 模型、接口与扩展

> 实际可用能力取决于你的上游授权、渠道配置、模型映射和供应商支持。MAX API 负责治理这些能力，不提供模型服务本身。

| 类别 | 接口或能力 |
|---|---|
| 通用模型接口 | Chat Completions、Responses、Embeddings、Rerank、Images、Audio、Video |
| 原生与实时协议 | Claude Messages、Google Gemini、OpenAI Realtime 等入口 |
| 推理与工具调用 | 支持 Reasoning Effort、函数工具、Tool Call ID、工具名称和多轮工具响应关联，并按不同上游能力进行协议转换 |
| 异步任务 | 任务提交、轮询、状态映射、结果代理和参数化计费 |
| 自定义上游 | Base URL、路径、参数、Header、状态字段和结果字段映射 |

覆盖 OpenAI、Claude、Gemini、Azure、AWS Bedrock、Vertex AI、Ollama 及多种国内模型平台，也可治理 Codex、Dify、RAGFlow 和多模态任务服务。具体支持范围以当前版本和渠道类型为准。

<details>
<summary><strong>系统工作方式与技术栈</strong></summary>

![MAX API 系统架构图](./docs/images/MAX-API架构图.png)

```text
应用 / SDK / Agent
  → 统一接口与身份鉴权
  → 模型权限、限流、预算和安全检查
  → 渠道选择、映射与失败重试
  → 上游协议适配
  → 可恢复结算、Evidence、日志和审计
  → 智能运维中心与管理员治理
```

后端使用 Go、Gin 和 GORM，前端使用 React 19、TypeScript、Base UI 与 Tailwind CSS，数据层兼容 SQLite、MySQL 和 PostgreSQL，并可使用 Redis 与独立日志库。供应商协议适配位于独立 Relay/Channel 层，计费与结算集中在统一服务边界中，管理端通过 SmartOps 展示只读观测和受限治理入口。

</details>

## 🛡️ 治理与运营

生产环境建议按以下顺序配置：

1. 配置登录、安全限制和用户注册策略。
2. 添加合法授权的上游渠道，确认模型、能力和协议配置。
3. 按团队、业务或环境设置分组、令牌、模型范围、额度和价格。
4. 为每个应用、Agent 或环境使用独立令牌，避免共享凭据和成本归属。
5. 配置重试、日志与告警，通过数据看板和智能运维中心持续观察。

渠道能力校验、表达式计费、通用任务协议、管理员审计和性能参数等高级配置，请查看 [详细文档](https://docs.max-api.ai)。

## 🧭 演进路线

MAX API 将继续以 **AI Models and Agents governance** 为核心，从统一网关、智能运维和可恢复结算出发，逐步建设面向 AGI 应用的 Evidence、评测、策略与受控执行能力。长期方向不是让一个无边界 Agent 接管生产系统，而是建立可验证、可审批、可停止、可回滚的工程闭环。

| 阶段 | 状态 | 重点 |
|---|---|---|
| 统一网关与智能运维 | **现已提供** | 接入、鉴权、路由、计费、日志、资源告警、渠道/模型性能、系统信息和计费结算对账 |
| Evidence 事实层 | **近期建设** | 统一模型请求、系统日志、指标、Task、路由、策略、结算和审计事件，向 Agent 提供脱敏、限权、只读接口 |
| 开放评测与治理模板 | **规划中** | 与社区沉淀模型/协议兼容测试、匿名故障样本、评测集、Runbook、Detector 和行业治理模板 |
| 受控自治运维 | **长期蓝图** | 在 Policy、Budget、Approval、Shadow、Canary 和 Rollback 约束下评估低风险自动动作 |
| 受控能力进化 | **长期蓝图** | 在隔离 Coding Workspace 中生成、测试和审阅候选改进，不直接修改生产系统 |
| AGI 工程闭环 | **长期方向** | 将 Evidence、评测、治理策略、人工审批与可回滚执行连接为可验证闭环 |

MAX API 不是基础模型，也不宣称当前已经实现 AGI 或自治运维。路线图中的长期能力只有在证据、权限、预算、审批和回滚边界完善后才会逐步验证。

## 🚢 生产部署

推荐使用 Docker Compose：

```bash
MAX_API_VERSION=v2.0.0-smartops.pre1
MAX_API_COMMIT=b7096156549edc930ca244891afb1ba5632dbe8f
git clone --branch "$MAX_API_VERSION" --depth 1 https://github.com/MAX-API-Next/MAX-API.git
cd MAX-API

test "$(git rev-parse HEAD)" = "$MAX_API_COMMIT"

# 修改 docker-compose.yml 中的数据库、Redis 密码和密钥
docker compose up -d
```

### 部署检查

| 组件 | 建议 |
|---|---|
| 数据库 | 使用仍在供应商安全支持周期内的 MySQL（建议 8.4 LTS）或 PostgreSQL（建议 14+），并配置备份与恢复 |
| 缓存 | 单机可使用内存缓存，多节点部署建议使用 Redis |
| 入口 | 配置 HTTPS 反向代理、请求大小限制和可信网络策略 |
| 密钥 | 显式配置随机 `SESSION_SECRET`；Redis/多节点场景统一 `CRYPTO_SECRET` |
| 节点 | 每个节点设置稳定且唯一的 `NODE_NAME` |
| 日志 | 根据合规和运维需要配置 `LOG_SQL_DSN`、清理与保留策略 |

多节点必须统一 `SESSION_SECRET` 和 `CRYPTO_SECRET`，并为每个节点使用不同的 `NODE_NAME`。独立日志库使用 `LOG_SQL_DSN`；错误性能统计需要按需启用 `ERROR_LOG_ENABLED`。完整环境变量和源码构建说明见 [详细文档](https://docs.max-api.ai)。

## 🤝 项目来源、致谢与二次开发

MAX API 基于 [One API](https://github.com/songquanpeng/one-api) 和 [New API](https://github.com/QuantumNous/new-api) 的开源成果持续开发。感谢上游项目、贡献者，以及所有参与协议适配、测试、文档、翻译与问题反馈的社区成员。

如果你基于本项目进行二次开发并仅供自用，欢迎在项目主页、页脚或“关于”页面等明显位置，任选一种方式保留项目来源或社区鸣谢：

- 添加项目地址：[MAX-API-Next/MAX-API](https://github.com/MAX-API-Next/MAX-API)
- 鸣谢社区：[MAX-API-Next](https://github.com/MAX-API-Next)

满足上述任一展示要求并保持链接清晰可见，即自动获得本项目的非永久临时商用授权，无需另行申请或等待确认；该授权仅在持续满足展示要求期间有效，最新条件以正式 README 与社区公告为准。

## 📜 许可证

本项目采用 [GNU Affero 通用公共许可证 v3.0（AGPLv3）](./LICENSE) 授权。

临时商用授权仅覆盖 MAX API 项目方有权授权的新增与修改部分，不包含、替代或免除 One API、New API 等上游项目的许可义务。商业使用时，请同时遵守 One API 的 MIT 许可证、New API 的 AGPLv3 许可证及各上游项目的最新许可要求。

如果你修改并通过网络向用户提供本项目服务，请理解并遵守 AGPLv3 对应的源码提供等义务。长期商用授权、机构合作或其他授权问题，请联系：maxapi@max-api.ai。

---

<div align="center">

### 💖 感谢使用 MAX API

如果这个项目对你有帮助，欢迎给我们一个 ⭐ Star、关注 Releases、提交可复现 Issue，或参与 MAX-API-Next 社区共建。

**[项目仓库](https://github.com/MAX-API-Next/MAX-API)** • **[参与共建](https://github.com/MAX-API-Next/MAX-API/issues)** • **[最新发布](https://github.com/MAX-API-Next/MAX-API/releases)** • **[MAX-API-Next 社区](https://github.com/MAX-API-Next)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
