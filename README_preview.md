<div align="center">

![MAX API](./web/default/public/logo.png)

# MAX API

**AI Models 与 Agents 的统一接入、治理和运营基础设施**

把多供应商模型、Agent 工作负载、成本、路由、日志和权限放进同一套可控边界。

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
  <a href="#-快速开始">快速开始</a> •
  <a href="#-核心能力">核心能力</a> •
  <a href="#-生产部署">生产部署</a> •
  <a href="#-常见问题">常见问题</a> •
  <a href="#-资源与社区">资源与社区</a>
</p>

</div>

---

MAX API 是由来自科研机构和高校的 AGI 爱好者组织发起、维护和长期运营的开源项目。它位于应用、Agent 与上游模型服务之间，提供统一接口、身份鉴权、渠道路由、计费结算、日志观测和组织治理能力。

它不替代 Dify、LangChain、MCP Server 或业务 Agent，也不提供上游模型账号和 API Key。MAX API 的职责，是帮助已有 AI 应用和 Agent 更稳定、更可控地进入真实运行环境。

> [!IMPORTANT]
> 面向公众提供生成式人工智能服务时，使用者应自行完成所在司法辖区要求的上游授权、备案许可、内容安全、实名、日志留存、税务、支付和用户协议等合规义务。日志审计和内容留存仅应在具备合法依据、明确告知、权限隔离和数据安全措施的场景下启用。

> [!TIP]
> 生产环境建议使用正式版。Preview 版本用于测试和灰度验证，升级前请备份数据库并准备回滚方案。

## 🚀 快速开始

本地体验默认使用 SQLite，只需 Docker：

```bash
docker pull cscitechtop/max-api:latest

docker run --name max-api -d --restart always -p 3000:3000 -e TZ=Asia/Shanghai -v ./data:/data cscitechtop/max-api:latest
```

启动后访问：<http://localhost:3000>

接下来只需完成三件事：

1. 创建或确认管理员账号。
2. 添加一个具有合法授权的上游渠道和 API Key。
3. 创建访问令牌，在应用中把 Base URL 指向 MAX API。

> [!WARNING]
> SQLite 适合本地体验、开发和小规模测试。正式环境建议使用 MySQL ≥ 5.7.8 或 PostgreSQL ≥ 9.6，并配置 Redis、HTTPS、备份和恢复方案。

## ✨ 核心能力

| 能力 | MAX API 解决的问题 |
|---|---|
| 统一模型入口 | 用统一网关接入 OpenAI Compatible、Responses、Claude Messages、Gemini、Realtime 和多模态任务接口 |
| 多供应商路由 | 管理渠道、权重、分组、模型映射、失败重试和跨供应商切换，降低单一上游风险 |
| Agent 治理 / AgentOps | 为 Agent、工作流和工具调用分配独立令牌、模型范围、额度与调用记录 |
| 成本与计费 | 支持倍率、固定价格、表达式计费、异步任务 rate-card、预扣费、结算和失败退款 |
| 可观测与审计 | 按用户、令牌、模型、渠道、分组和节点查看用量、耗时、错误、重试与管理员审计信息 |
| 私有化与扩展 | 支持 SQLite、MySQL、PostgreSQL、Redis、多节点部署和自定义上游协议配置 |

### 使用 MAX API 的优势

| 维度 | 直接连接多个供应商 | 使用 MAX API |
|---|---|---|
| 应用接入 | 每家 SDK、协议和错误格式分别维护 | 应用侧使用相对稳定的统一入口 |
| 模型切换 | 需要修改代码和配置 | 通过渠道、映射、分组和路由调整 |
| 成本核算 | 账单分散，难以归因 | 按用户、令牌、模型、渠道和节点统一统计 |
| 权限治理 | 凭据和权限散落在不同应用 | 集中管理令牌、模型范围、额度和有效期 |
| 故障排查 | 日志分散，跨供应商关联困难 | 在网关侧统一查看请求、重试、耗时和错误 |

## 🎯 适用场景

- **团队或组织内部模型网关**：统一管理用户、令牌、模型、供应商、权限和费用。
- **Agent 应用运行底座**：为 Agent、工作流和工具调用提供模型访问控制、成本归因和异常定位。
- **多供应商容灾与迁移**：在不同模型平台之间进行映射、加权路由、失败重试和灰度切换。
- **多模态任务治理**：统一管理图像、音频、视频、嵌入、重排序和实时对话接口。
- **私有化与合规运营**：自主管理密钥、数据、日志、审计、价格和部署环境。

## 🧭 工作方式

![MAX API 系统架构图](./docs/images/MAX-API架构图.png)

一次模型请求通常经过以下链路：

```text
应用 / SDK / Agent
  → 统一接口与身份鉴权
  → 模型权限、限流和安全检查
  → 渠道选择、映射与失败重试
  → 上游协议适配
  → 用量结算、日志和审计
```

MAX API 保持分层架构：`Router → Controller → Service → Model`，供应商协议转换位于 `relay/` 与 `relay/channel/`。数据层使用 GORM，兼容 SQLite、MySQL 和 PostgreSQL；Redis 与内存缓存用于限流、会话和多节点协同。

<details>
<summary><strong>开发者技术栈</strong></summary>

| 层 | 技术 |
|---|---|
| 后端 | Go 1.25.1+、Gin、GORM v2 |
| 前端 | React 19、TypeScript、Rsbuild、Base UI、Tailwind CSS |
| 前端包管理 | Bun workspace |
| 数据库 | SQLite / MySQL ≥ 5.7.8 / PostgreSQL ≥ 9.6 |
| 缓存 | Redis + 内存缓存 |
| 鉴权 | JWT、WebAuthn/Passkeys、OAuth、OIDC |

</details>

## 🤖 模型与接口

> 实际可用能力取决于你的上游授权、渠道配置、模型映射和供应商支持。MAX API 负责治理这些能力，不提供模型服务本身。

### 主要接口

| 类别 | 接口或能力 |
|---|---|
| Chat / Responses | `/v1/chat/completions`、`/v1/responses`，以及 Responses ↔ Chat Completions 兼容转换 |
| Embeddings / Rerank | `/v1/embeddings`、`/v1/rerank` |
| Images / Audio / Video | `/v1/images/*`、`/v1/audio/*`、`/v1/videos/*` |
| 原生协议 | Claude Messages、Google Gemini、OpenAI Realtime 等入口 |
| 异步任务 | 任务提交、轮询、状态映射、结果代理和参数化计费 |
| 自定义上游 | Base URL、路径覆盖、参数覆盖、Header 覆盖、状态和结果字段映射 |

### 供应商与生态

可接入 OpenAI、Azure OpenAI、Claude、Gemini、AWS Bedrock、Vertex AI、Ollama，以及 DeepSeek、通义千问 / 阿里云百炼、智谱 GLM、Kimi、豆包 / 火山引擎、腾讯混元、百度文心 / 千帆、讯飞星火、MiniMax、零一万物、硅基流动等平台。

同时支持 Codex、Dify、RAGFlow、Kling、Seedance、Midjourney、Suno 等应用、Agent 或多模态服务相关的接入治理。具体能力以渠道类型和当前版本为准。

<details>
<summary><strong>Reasoning Effort 模型命名示例</strong></summary>

- OpenAI：`o3-mini-high`、`gpt-5-medium`、`gpt-5-low`
- Claude：`claude-3-7-sonnet-20250219-thinking`
- Gemini：`gemini-2.5-flash-thinking`、`gemini-2.5-pro-thinking-128`

</details>

## 🛡️ 治理与运营

推荐按照下面的顺序配置：

1. 配置登录、安全限制和用户注册策略。
2. 添加上游渠道，确认 Base URL、模型列表、能力和合法授权凭据。
3. 按团队或业务线设置用户分组、令牌分组和模型范围。
4. 配置模型价格、任务计费、额度、失败重试和日志策略。
5. 为每个应用、Agent 或环境创建独立令牌。
6. 通过使用日志、数据看板和渠道测试持续观察运行状态。

| 管理入口 | 用途 |
|---|---|
| 渠道管理 | 维护供应商、模型映射、权重、协议路径、能力矩阵和配置校验 |
| 模型与价格 | 维护模型列表、倍率、固定价格、表达式计费和任务 rate-card |
| 令牌管理 | 为应用、Agent、工作流和用户限制模型范围、额度和有效期 |
| 使用日志 | 查看消耗、耗时、错误、重试、渠道命中和审计信息 |
| 系统设置 | 管理安全、性能、日志、运营、计费、支付和站点配置 |
| 数据看板 | 查看请求量、模型用量、消费趋势和渠道运行状态 |

<details>
<summary><strong>高级治理能力</strong></summary>

- **渠道能力矩阵与校验**：识别 `chat/completions`、`responses`、`embeddings`、`rerank`、视频任务和模型发现能力，并提示 Base URL、JSON、Vertex AI、Codex 凭据及任务路径配置风险。
- **通用视频任务协议**：可配置提交路径、查询路径、任务 ID、状态、进度、结果 URL 和错误字段映射；请求体透传使用 `Pass Through Body`，字段改写使用 `Param Override`。
- **计费 JSON 维护**：支持批量维护表达式计费和任务 rate-card；视频任务可按时长、清晰度、音频等请求参数匹配价格。
- **管理员日志审计**：普通用户日志接口会过滤管理员专用字段；启用内容留存前应确认合规依据和访问权限。
- **性能治理**：支持模型限流、流式超时、大响应缓冲、请求体限制、磁盘缓存、Pyroscope 和优雅关闭。

完整配置参考请查看 [详细中文文档](./README.zh_CN.md)。

</details>

## 🚢 生产部署

推荐使用 Docker Compose：

```bash
git clone https://github.com/MAX-API-Next/MAX-API.git
cd MAX-API

# 修改 docker-compose.yml 中的数据库、Redis 密码和密钥
docker compose up -d
```

### 部署要求

| 组件 | 建议 |
|---|---|
| 数据库 | MySQL ≥ 5.7.8 或 PostgreSQL ≥ 9.6，并配置备份与恢复 |
| 缓存 | 单机可使用内存缓存，多机部署建议使用 Redis |
| 入口 | HTTPS 反向代理、请求大小限制和可信网络策略 |
| 密钥 | 显式配置随机 `SESSION_SECRET`；Redis/多节点场景配置统一 `CRYPTO_SECRET` |
| 节点 | 多节点设置稳定且唯一的 `NODE_NAME` |
| 日志 | 根据合规和运维需要配置日志目录、LOG_DB、清理与保留策略 |

<details>
<summary><strong>常用环境变量</strong></summary>

| 变量 | 用途 | 默认值 |
|---|---|---|
| `SQL_DSN` | 主数据库连接 | SQLite |
| `LOG_SQL_DSN` | 可选独立日志数据库 | 与主数据库相同 |
| `REDIS_CONN_STRING` | Redis 连接 | - |
| `SESSION_SECRET` | 会话密钥，多机必须一致 | - |
| `CRYPTO_SECRET` | 加密密钥，Redis/多机必须一致 | - |
| `NODE_NAME` | 节点名称与日志归属 | - |
| `STREAMING_TIMEOUT` | 流式响应超时，单位秒 | `300` |
| `STREAM_SCANNER_MAX_BUFFER_MB` | 流式单行最大缓冲 | `64` |
| `MAX_REQUEST_BODY_MB` | 解压后请求体上限 | `32` |
| `ERROR_LOG_ENABLED` | 错误请求日志开关 | `false` |
| `PYROSCOPE_URL` | Pyroscope 服务地址 | - |

</details>

<details>
<summary><strong>多节点部署注意事项</strong></summary>

- 所有节点必须使用相同的 `SESSION_SECRET`。
- 使用共享 Redis 时必须使用相同的 `CRYPTO_SECRET`。
- 每个节点应设置不同且稳定的 `NODE_NAME`。
- 使用外部数据库、外部 Redis、统一 HTTPS 入口和可靠备份。

</details>

<details>
<summary><strong>从源码构建镜像</strong></summary>

```bash
git clone https://github.com/MAX-API-Next/MAX-API.git
cd MAX-API

go mod download
go mod verify
docker build --pull --no-cache -t cscitechtop/max-api:latest .
```

前端使用 Bun workspace。构建上下文需要保留 `web/package.json`、`web/bun.lock` 和 `web/default/package.json`，否则 `catalog:` 依赖可能无法解析。

</details>

## 🗺️ 路线图

以下方向会根据维护节奏和真实场景调整，不构成发布日期承诺：

- 深化模型目录、权限、能力标签、价格和供应商变化治理。
- 完善 Agent 调用追踪、成本归因、异常定位、Evidence 与 AgentOps。
- 以受控插件形式提供 Detector、OpsAgent、评估器和行业治理能力。
- 增强多模态任务、协议转换、国产模型和供应商模板。
- 完善组织级权限、审计、私有化、多节点和运营体验。

欢迎通过 [GitHub Issues](https://github.com/MAX-API-Next/MAX-API/issues) 提交需求和改进建议。

## ❓ 常见问题

<details>
<summary><strong>MAX API 会提供模型服务或 API Key 吗？</strong></summary>

不会。MAX API 是模型与 Agent 工作负载的网关治理层。你需要自行获得合法授权的上游模型服务和凭据。

</details>

<details>
<summary><strong>MAX API 和 Agent 框架是什么关系？</strong></summary>

MAX API 不替代 Dify、LangChain、MCP Server、工作流引擎或业务 Agent。它负责这些应用与上游模型之间的访问、路由、成本、日志和治理。

</details>

<details>
<summary><strong>支持哪些数据库？</strong></summary>

支持 SQLite、MySQL ≥ 5.7.8 和 PostgreSQL ≥ 9.6。SQLite 适合本地体验；生产建议使用 MySQL 或 PostgreSQL。

</details>

<details>
<summary><strong>能否从 One API / New API 迁移？</strong></summary>

项目兼容 One API 与 New API 的主要数据结构，但迁移前仍应备份数据库，并在测试环境验证渠道、倍率、用户、令牌、任务和日志数据。

</details>

<details>
<summary><strong>图像、流式响应或大响应被截断怎么办？</strong></summary>

可以调整 `STREAM_SCANNER_MAX_BUFFER_MB`。请求体返回 `413` 时检查 `MAX_REQUEST_BODY_MB`。提高限制前请同时评估内存与并发风险。

</details>

<details>
<summary><strong>多机部署登录状态不一致怎么办？</strong></summary>

确认所有节点使用相同的 `SESSION_SECRET`。使用共享 Redis 时，还必须统一 `CRYPTO_SECRET`，并为节点设置稳定且不同的 `NODE_NAME`。

</details>

<details>
<summary><strong>普通用户能看到管理员日志审计内容吗？</strong></summary>

普通用户日志接口会过滤管理员专用字段。数据库管理员、系统管理员或拥有管理员日志权限的人仍可能访问相关数据，因此必须严格控制权限和留存策略。

</details>

## 🔗 资源与社区

| 资源 | 链接 |
|---|---|
| 项目仓库 | [MAX-API-Next/MAX-API](https://github.com/MAX-API-Next/MAX-API) |
| 问题反馈 | [GitHub Issues](https://github.com/MAX-API-Next/MAX-API/issues) |
| 最新发布 | [GitHub Releases](https://github.com/MAX-API-Next/MAX-API/releases) |
| Docker 镜像 | [cscitechtop/max-api](https://hub.docker.com/r/cscitechtop/max-api) |
| DeepWiki | [Ask DeepWiki](https://deepwiki.com/MAX-API-Next/MAX-API) |
| 社群交流 | QQ 群 `950126533`；微信群搜索 `MAX-API` |

配套工具：

- [max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool)：Key 额度查询工具。
- [max-api-horizon](https://github.com/MAX-API-Next/MAX-API-horizon)：MAX API 高性能优化版。

## 🤝 项目来源与二次开发

MAX API 基于以下开源项目持续增强 AI API 网关与治理能力、扩展功能并修复问题：

| 项目 | 许可证 |
|---|---|
| [One API](https://github.com/songquanpeng/one-api) | MIT |
| [New API](https://github.com/QuantumNous/new-api) | AGPLv3 |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Apache-2.0 |
| [Suno API](https://github.com/Suno-API/Suno-API) | MIT |

如果你基于本项目进行二次开发并仅供自用，欢迎在项目主页、页脚或“关于”页面等明显位置，任选一种方式保留项目来源或社区鸣谢：

- 添加项目地址：[MAX-API-Next/MAX-API](https://github.com/MAX-API-Next/MAX-API)
- 鸣谢社区：[MAX-API-Next](https://github.com/MAX-API-Next)

<details>
<summary><strong>React / Tailwind CSS 鸣谢代码示例</strong></summary>

```tsx
<p className='text-sm text-muted-foreground'>
  基于{' '}
  <a
    href='https://github.com/MAX-API-Next/MAX-API'
    target='_blank'
    rel='noopener noreferrer'
    className='font-medium underline underline-offset-4'
  >
    MAX-API-Next/MAX-API
  </a>{' '}
  二次开发 · 感谢{' '}
  <a
    href='https://github.com/MAX-API-Next'
    target='_blank'
    rel='noopener noreferrer'
    className='font-medium underline underline-offset-4'
  >
    MAX-API-Next 社区
  </a>
</p>
```

</details>

满足上述任一展示要求并保持链接清晰可见，即自动获得本项目的临时商用授权，无需另行申请或等待确认。该授权为非永久授权，仅在持续满足展示要求期间有效；有效期及后续调整以项目方在本 README 或官方社区发布的最新说明为准。

商业使用本项目时，还须分别遵守 One API 的 MIT 许可证与 New API 的 AGPLv3 许可证，具体以各上游项目的 `LICENSE` 文件为准。本项目提供的临时商用授权不替代、也不免除相关上游项目的开源许可义务。

如不再满足展示要求，或临时授权到期、被公告调整或终止，仍需按 AGPLv3 或项目方另行书面授权使用本项目。如需长期商用授权，请联系：maxapi@max-api.ai。

## 📜 许可证

本项目采用 [GNU Affero 通用公共许可证 v3.0（AGPLv3）](./LICENSE) 授权。

除默认的 AGPLv3 授权外，符合上文“项目来源与二次开发”条件的自用二开项目，可按该说明自动获得非永久的临时商用授权。该临时授权仅覆盖 MAX API 项目方有权授权的新增与修改部分，并不包含或代替 One API、New API 等上游项目的许可授权。

如果你修改并通过网络向用户提供本项目服务，请理解并遵守 AGPLv3 对应源码提供等义务。商业合作、机构合作或其他授权问题，请联系：maxapi@max-api.ai。

---

<div align="center">

### 💖 感谢使用 MAX API

如果这个项目对你有帮助，欢迎给我们一个 ⭐ Star。

**[官方文档](https://github.com/MAX-API-Next/MAX-API)** • **[问题反馈](https://github.com/MAX-API-Next/MAX-API/issues)** • **[最新发布](https://github.com/MAX-API-Next/MAX-API/releases)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
