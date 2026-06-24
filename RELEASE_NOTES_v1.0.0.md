# MAX API v1.0.0 发布说明

本说明面向正在评估或准备从 New API 迁移的用户。基于当前 MAX API 代码与本地 `new-api-1.0.0-rc.13` 代码、README 和配置文档对比，MAX API v1.0.0 延续 New API/One API 的多模型网关、用户与令牌、计费、日志、Docker 部署和三数据库兼容基础，并进一步聚焦 AI 模型治理、AgentOps、渠道配置治理、异步任务治理和成本核算。

一句话概括：MAX API 不只是把多个模型接口统一到一个入口，而是把模型、渠道、令牌、计费、日志、异步任务和运维配置纳入一个可配置、可观测、可核算的治理层。

## 相比 New API 的主要不同与改进

| 领域 | New API 已有基础 | MAX API v1.0.0 的改进 |
| --- | --- | --- |
| 项目定位 | 多模型 API 网关与 AI 资产管理 | 明确扩展为 AI 模型治理、AgentOps 与应用服务基础设施，强调模型、渠道、Agent 调用链路、成本和审计的持续运营 |
| 渠道治理 | 支持多供应商渠道、模型映射、权重、分组和失败重试 | 增加渠道能力矩阵与配置校验，帮助管理员在保存前识别 Base URL、JSON 配置、模型发现、Codex 凭证、Vertex AI 区域、视频任务占位符等风险 |
| 自定义上游 | 支持 Advanced Custom 渠道与协议转换 | 强化按请求路径选择渠道的逻辑，使高级自定义渠道与普通渠道在缓存和数据库选择路径下保持一致；无效高级自定义配置会被跳过并记录错误，避免影响同模型下的健康渠道 |
| 多模态任务 | 已具备视频任务入口和部分供应商任务适配 | 新增通用视频任务协议，可配置提交路径、查询路径、任务 ID、状态、进度、结果 URL、错误字段和状态映射，减少为兼容型视频上游重复写适配器的成本 |
| 任务结果代理 | 支持任务状态查询 | 强化 `/v1/videos/{task_id}/content` 内容代理场景，便于在需要隐藏上游资源地址时通过网关返回任务结果 |
| 成本与计费 | 支持倍率、固定价格、表达式计费等基础 | 增加分阶段计费 JSON 统一维护入口，并新增 `task_billing_setting.rate_cards` 任务 rate-card 计费配置，适合按供应商、模型、时长、质量、音频、视频输入等参数核算异步任务成本 |
| 运维与文档 | 提供部署、环境变量、FAQ 等通用说明 | README 和安装文档改为围绕治理配置、能力矩阵、视频任务协议、计费 JSON、Docker 镜像和多机部署注意事项展开，更适合生产部署前核对 |
| 发布与镜像 | 使用 New API 镜像和仓库命名 | MAX API 使用独立镜像 `cscitechtop/max-api:v1.0.0` 与 `cscitechtop/max-api:latest`，服务名、部署示例、文档入口和支持入口已切换到 MAX API 体系 |

## 重点更新

### 1. 模型治理与 AgentOps 定位

MAX API v1.0.0 将网关能力从“统一转发”进一步扩展为治理平面：

- 统一管理模型入口、供应商渠道、模型映射、分组、权限和价格规则。
- 为应用、Agent、工作流和工具调用提供独立令牌、模型范围、额度和过期时间控制。
- 通过请求日志、用量统计、渠道命中、错误信息和成本归因支持 Agent 调用链路排查。
- 面向私有化和组织运营场景，强调密钥自主管理、日志留存、审计边界和合规配置。

### 2. 渠道能力矩阵与配置校验

渠道新建和编辑流程新增更清晰的治理辅助能力：

- 能力矩阵展示 `chat/completions`、`responses`、`Claude Messages`、`Gemini native`、`embeddings`、`images`、`audio`、`rerank`、`video tasks`、`model discovery` 等能力状态。
- 保存前校验 API Key、模型列表、Base URL、额外配置 JSON、Vertex AI 区域、Codex 凭证、模型发现设置和视频任务路径占位符。
- 对需要额外 JSON 配置的渠道，提前暴露配置风险，减少上线后才发现路径、协议或字段不匹配的问题。

### 3. 高级自定义渠道选择更稳健

v1.0.0 强化了 Advanced Custom 渠道在多路径、多优先级、多权重场景下的选择行为：

- 渠道选择会先按请求路径过滤高级自定义渠道，再进入优先级和权重选择。
- 内存缓存路径和数据库路径使用一致的高级自定义配置解析与校验逻辑。
- 配置损坏或校验失败的高级自定义渠道会被排除并记录错误，不会阻断同模型、同分组下的正常渠道。
- 优先级重试会做边界处理，避免重试索引越界导致异常。

### 4. 通用视频任务协议

MAX API 将视频任务接入从固定供应商适配扩展为可配置任务协议。渠道可通过 `task_protocol = "generic_video_task"` 与 `task_protocol_config` 描述上游任务接口：

```json
{
  "task_protocol": "generic_video_task",
  "task_protocol_config": {
    "submit_path": "/v1/videos/create",
    "query_path": "/v1/videos/{task_id}",
    "task_id_path": "task_id",
    "status_path": "status",
    "progress_path": "progress",
    "result_url_paths": [
      "result.primary_url",
      "result.urls.0",
      "data.result.primary_url",
      "url",
      "video_url",
      "download_url"
    ],
    "error_message_path": "error_message",
    "status_map": {
      "queued": "QUEUED",
      "running": "IN_PROGRESS",
      "succeeded": "SUCCESS",
      "failed": "FAILURE"
    }
  }
}
```

该能力适合路径、任务 ID、状态字段、进度字段、错误字段和结果 URL 字段可通过 JSON 路径描述的视频上游。查询路径支持 `{task_id}`、`{operation_name}` 和 `{upstream_task_id}`，其中 `{operation_name}` 可保留多段路径，适配 Gemini / Vertex 风格的 operation 查询。

如果上游需要特殊签名、非标准认证、复杂请求体重写、回调机制或完全不同的任务生命周期，仍建议新增或扩展专用适配器，不应仅依赖配置项。

### 5. 任务 rate-card 与计费 JSON 维护

MAX API v1.0.0 对计费配置做了更适合生产维护的整理：

- `Tiered billing JSON` 用于统一维护多个模型的分阶段计费规则，保存时同步更新 `billing_mode` 与 `billing_expr`。
- `task_billing_setting.rate_cards` 用于维护异步任务计费规则，可按 `vendor` 区分 Sora、Veo、Seedance、Kling 等供应商分区。
- 任务 rate-card 支持按请求参数匹配价格，例如时长、清晰度、是否带音频、是否有视频输入等字段。
- 用量日志中保留计费模式和命中明细，便于定位复杂模型或视频任务的成本来源。

示例：

```json
{
  "vendor/model-name": {
    "vendor": "kling",
    "unit": "second",
    "quantity_field": "duration",
    "default_quantity": 5,
    "strict": true,
    "defaults": {
      "quality": "std",
      "has_audio": "false"
    },
    "rows": [
      {
        "id": "std_no_audio",
        "match": {
          "quality": "std",
          "has_audio": "false"
        },
        "unit_price": 0.6
      }
    ]
  }
}
```

### 6. 部署与镜像

v1.0.0 推荐直接使用 MAX API 镜像。固定生产版本可使用 `v1.0.0` tag，跟随最新构建可使用 `latest` tag：

```bash
docker pull cscitechtop/max-api:v1.0.0
docker pull cscitechtop/max-api:latest

docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  cscitechtop/max-api:v1.0.0
```

生产环境建议使用 Docker Compose，并显式配置数据库、Redis、`SESSION_SECRET`、`CRYPTO_SECRET`、日志目录、HTTPS 反向代理和备份策略。

## 兼容与迁移说明

- MAX API 继续兼容 New API 与 One API 的主要数据结构，通常可以复用既有数据库数据。
- 迁移前必须备份数据库，并在测试环境验证用户、令牌、渠道、倍率、模型映射、任务记录和日志数据。
- 使用多机部署时，所有节点应配置相同的 `SESSION_SECRET`；启用 Redis 或多机共享加密数据时，应配置相同的 `CRYPTO_SECRET`。
- 从 New API 镜像迁移时，请将部署镜像替换为 `cscitechtop/max-api:v1.0.0` 或 `cscitechtop/max-api:latest`，并核对容器名、服务名、Pyroscope 应用名、仓库地址和文档入口。
- SMTP 发送现在默认校验 TLS 证书；如果既有企业邮箱使用自签名证书、IP 证书或主机名不匹配证书，请在后台 SMTP 邮件设置中开启“跳过 SMTP TLS 证书验证”，或通过环境变量 `SMTP_INSECURE_SKIP_VERIFY=true` 显式兼容旧部署。
- 对已有 Advanced Custom 渠道，建议迁移后重点检查 `advanced_custom` 配置是否能通过当前校验规则，以及每条路由的 `incoming_path` 是否覆盖真实请求路径。
- 对视频任务渠道，建议先用测试模型验证提交、轮询、状态映射、结果 URL 提取和内容代理，再开放给生产令牌。

## 仍需注意的边界

- MAX API 是网关治理层，不提供上游模型账号、API Key、基础模型训练能力或模型服务本身。
- MAX API 不替代 Dify、LangChain、MCP Server、工作流引擎或业务 Agent 应用；它负责这些应用和上游模型服务之间的接入、鉴权、路由、计费、日志和治理。
- 通用视频任务协议适合结构可配置的 submit/query 类接口；对强签名、复杂认证、回调驱动或非任务式上游，仍需要适配器开发。
- 面向公众提供生成式 AI 服务时，运营方仍需自行完成上游授权、备案、内容安全、实名、日志留存、支付、税务和用户协议等合规义务。

## 致谢

MAX API v1.0.0 延续并兼容 New API 与 One API 的开源基础，在此基础上面向模型治理、AgentOps、异步任务和成本核算场景做进一步工程化增强。感谢相关开源项目和社区生态的长期贡献。
