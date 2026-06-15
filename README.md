<div align="center">

![max-api](/web/default/public/logo.png)

# MAX API

🍥 **AI API Gateway, AgentOps, and AGI Application Infrastructure**

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
  --><a href="https://hub.docker.com/r/max-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a><!--
  --><a href="https://goreportcard.com/report/github.com/MAX-API-Next/MAX-API">
    <img src="https://goreportcard.com/badge/github.com/MAX-API-Next/MAX-API" alt="GoReportCard">
  </a>
</p>

<p align="center">
  <a href="https://trendshift.io/repositories/20180" target="_blank">
    <img src="https://trendshift.io/api/badge/repositories/20180" alt="MAX-API-Next%2FMAX-API | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/>
  </a>
  <br>
  <a href="https://hellogithub.com/repository/MAX-API-Next/MAX-API" target="_blank">
    <img src="https://api.hellogithub.com/v1/widgets/recommend.svg?rid=539ac4217e69431684ad4a0bab768811&claim_uid=tbFPfKIDHpc4TzR" alt="Featured｜HelloGitHub" style="width: 250px; height: 54px;" width="250" height="54" />
  </a><!--
  --><a href="https://www.producthunt.com/products/max-api/launches/max-api?embed=true&utm_source=badge-featured&utm_medium=badge&utm_campaign=badge-max-api" target="_blank" rel="noopener noreferrer">
    <img src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=1047693&theme=light&t=1769577875005" alt="MAX API - All-in-one AI asset management gateway. | Product Hunt" style="width: 250px; height: 54px;" width="250" height="54" />
  </a>
</p>

<p align="center">
  <a href="#-quick-start">Quick Start</a> •
  <a href="#-key-features">Key Features</a> •
  <a href="#-deployment">Deployment</a> •
  <a href="#-documentation">Documentation</a> •
  <a href="#-help-support">Help</a>
</p>

</div>

## 📝 Project Description

MAX API is a public-interest project initiated, maintained, and operated by an organization of AGI enthusiasts from research institutions. It aims to provide stable and reusable AI application infrastructure for developers, researchers, and organizations.

> [!IMPORTANT]
> - This project is intended solely for lawful and authorized AI API gateway, organization-level authentication, multi-model management, usage analytics, cost accounting, and private deployment scenarios.
> - Users must lawfully obtain upstream API keys, accounts, model services, and interface permissions, and must comply with upstream terms of service and applicable laws and regulations.
> - Users should ensure their use complies with upstream terms of service and applicable laws and regulations.
> - When providing generative AI services to the public, users should comply with applicable regulatory requirements and fulfill all filing, licensing, content safety, real-name verification, log retention, tax, and upstream authorization obligations required by their jurisdiction.

---

## Project Positioning

In the AGI application era, MAX API focuses on being an open AI API Gateway, AgentOps console, and application-service foundation, building the service, governance, and operations layer that helps developers and organizations run AI applications reliably:

- **Unified AI API Gateway**: provide one access layer for LLMs, image generation, video generation, audio, rerank, realtime, and other model APIs.
- **Protocol and provider adaptation**: normalize official APIs and provider-specific APIs into stable application-facing interfaces.
- **Cost, quota, and reliability control**: support routing, retries, rate limits, quota accounting, usage analytics, billing, and operational dashboards.
- **AgentOps and application operations**: provide authentication, permissions, model access control, cost tracking, observability, and a foundation for future tool, workflow, and MCP-style service orchestration.
- **Community service templates**: encourage reusable channel templates, task-protocol templates, pricing configurations, and deployment practices for the broader AI application ecosystem.

---

## 🤝 Trusted Partners

<p align="center">
  <em>No particular order</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="Peking University" height="80" />
  </a><!--
  --><a href="https://www.compshare.cn/?ytag=GPU_yy_gh_maxapi" target="_blank">
    <img src="./docs/images/ucloud.png" alt="UCloud" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="Alibaba Cloud" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 Special Thanks

<p align="center">
  <a href="https://www.jetbrains.com/?from=max-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong>Thanks to <a href="https://www.jetbrains.com/?from=max-api">JetBrains</a> for providing free open-source development license for this project</strong>
</p>

---

## 🚀 Quick Start

### Using Docker Compose (Recommended)

```bash
# Clone the project
git clone https://github.com/MAX-API-Next/MAX-API.git
cd max-api

# Edit docker-compose.yml configuration
nano docker-compose.yml

# Start the service
docker-compose up -d
```

<details>
<summary><strong>Using Docker Commands</strong></summary>

```bash
# Pull the latest image
docker pull max-api:latest

# Using SQLite (default)
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest

# Using MySQL
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

> **💡 Tip:** `-v ./data:/data` will save data in the `data` folder of the current directory, you can also change it to an absolute path like `-v /your/custom/path:/data`

</details>

---

🎉 After deployment is complete, visit `http://localhost:3000` to start using!

> [!WARNING]
> When operating this project as a public generative AI service or API resale service, users should first complete all required filing, licensing, content safety, real-name verification, log retention, tax, payment, and upstream authorization obligations.

📖 For more deployment methods, please refer to [Deployment Guide](https://github.com/MAX-API-Next/MAX-API)

---

## 📚 Documentation

<div align="center">

### 📖 [Official Documentation](https://github.com/MAX-API-Next/MAX-API) | [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/MAX-API-Next/MAX-API)

</div>

**Quick Navigation:**

| Category | Link |
|------|------|
| 🚀 Deployment Guide | [Installation Documentation](https://github.com/MAX-API-Next/MAX-API) |
| ⚙️ Environment Configuration | [Environment Variables](https://github.com/MAX-API-Next/MAX-API) |
| 📡 API Documentation | [API Documentation](https://github.com/MAX-API-Next/MAX-API) |
| ❓ FAQ | [FAQ](https://github.com/MAX-API-Next/MAX-API) |
| 💬 Community Interaction | [Communication Channels](https://github.com/MAX-API-Next/MAX-API) |

---

## ✨ Key Features

> For detailed features, please refer to [Features Introduction](https://github.com/MAX-API-Next/MAX-API)

### 🎨 Core Functions

| Feature | Description |
|------|------|
| 🎨 New UI | Modern user interface design |
| 🌍 Multi-language | Supports Simplified Chinese, Traditional Chinese, English, French, Japanese |
| 🔄 Data Compatibility | Fully compatible with the original One API database |
| 📈 Data Dashboard | Visual console and statistical analysis |
| 🔒 Permission Management | Token grouping, model restrictions, user management |

### 💰 Authorized Usage Accounting and Billing

- ✅ Internal top-up and quota allocation for lawful authorized scenarios (EPay, Stripe)
- ✅ Organization-level per-request, usage-based, and cache-hit cost accounting
- ✅ Cache billing statistics for OpenAI, Azure, DeepSeek, Claude, Qwen, and supported models
- ✅ Flexible billing policies for internal management or authorized enterprise customers

### 🔐 Authorization and Security

- 😈 Discord authorization login
- 🤖 LinuxDO authorization login
- 📱 Telegram authorization login
- 🔑 OIDC unified authentication
- 🔍 Key quota query usage (with [max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool))

### 🚀 Advanced Features

**API Format Support:**
- ⚡ [OpenAI Responses](https://github.com/MAX-API-Next/MAX-API)
- ⚡ [OpenAI Realtime API](https://github.com/MAX-API-Next/MAX-API) (including Azure)
- ⚡ [Claude Messages](https://github.com/MAX-API-Next/MAX-API)
- ⚡ [Google Gemini](https://github.com/MAX-API-Next/MAX-API)
- 🔄 [Rerank Models](https://github.com/MAX-API-Next/MAX-API) (Cohere, Jina)

**Intelligent Routing:**
- ⚖️ Channel weighted random
- 🔄 Automatic retry on failure
- 🚦 User-level model rate limiting

**Format Conversion:**
- 🔄 **OpenAI Compatible ⇄ Claude Messages**
- 🔄 **OpenAI Compatible → Google Gemini**
- 🔄 **Google Gemini → OpenAI Compatible** - Text only, function calling not supported yet
- 🚧 **OpenAI Compatible ⇄ OpenAI Responses** - In development
- 🔄 **Thinking-to-content functionality**

**Configurable Video Task Protocol:**

MAX API can route video task requests through third-party gateways whose HTTP paths and task response fields differ from the official provider. On any video task channel, set `task_protocol` to `generic_video_task` and configure `task_protocol_config` in the channel `settings` field.

The path template supports `{task_id}` for query requests and `{model}` / `{action}` for submit or query requests. The query path must include `{task_id}`.

```json
{
  "task_protocol": "generic_video_task",
  "task_protocol_config": {
    "submit_path": "/v1/videos/create",
    "query_path": "/v1/videos/{task_id}",
    "task_id_path": "task_id",
    "status_path": "status",
    "progress_path": "progress",
    "result_url_paths": ["result.url", "result.video_url", "url"],
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

This protocol controls the upstream submit path, query path, upstream task ID path, task status path, progress path, result URL paths, error message path, and upstream-to-internal status mapping. It applies to video task channels such as Sora, Kling, Gemini/Veo, Vertex/Veo, Ali video, Hailuo, Jimeng, Vidu, and Doubao video.

For example, a third-party video gateway can be configured with:

- Channel type: any supported video task channel
- Base URL: `https://www.example.com`
- Upstream submit path: `POST /v1/videos/create`
- Upstream query path: `GET /v1/videos/{task_id}`

If only `task_protocol_config.submit_path` and `task_protocol_config.query_path` are set without `task_protocol`, MAX API only overrides the upstream HTTP paths and keeps the channel's official response parser. Existing `seedance_official_media` settings are still accepted for backward compatibility with Doubao Seedance-style request conversion.

Client requests keep the selected channel's normal video request format. For example:

```json
{
  "model": "video-model-name",
  "prompt": "A cinematic neon street video",
  "aspect_ratio": "16:9",
  "duration_seconds": 5,
  "input_mode": "text",
  "control_mode": "none",
  "resolution": "720p",
  "with_audio": true
}
```

**Reasoning Effort Support:**

<details>
<summary>View detailed configuration</summary>

**OpenAI series models:**
- `o3-mini-high` - High reasoning effort
- `o3-mini-medium` - Medium reasoning effort
- `o3-mini-low` - Low reasoning effort
- `gpt-5-high` - High reasoning effort
- `gpt-5-medium` - Medium reasoning effort
- `gpt-5-low` - Low reasoning effort

**Claude thinking models:**
- `claude-3-7-sonnet-20250219-thinking` - Enable thinking mode

**Google Gemini series models:**
- `gemini-2.5-flash-thinking` - Enable thinking mode
- `gemini-2.5-flash-nothinking` - Disable thinking mode
- `gemini-2.5-pro-thinking` - Enable thinking mode
- `gemini-2.5-pro-thinking-128` - Enable thinking mode with thinking budget of 128 tokens
- You can also append `-low`, `-medium`, or `-high` to any Gemini model name to request the corresponding reasoning effort (no extra thinking-budget suffix needed).

</details>

---

## 🤖 Model Support

> For details, please refer to [API Documentation - Gateway Interface](https://github.com/MAX-API-Next/MAX-API)

| Model Type | Description | Documentation |
|---------|------|------|
| 🤖 OpenAI-Compatible | OpenAI compatible models | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 🤖 OpenAI Responses | OpenAI Responses format | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 🎨 Midjourney-Proxy | [Midjourney-Proxy(Plus)](https://github.com/novicezk/midjourney-proxy) | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 🎵 Suno-API | [Suno API](https://github.com/Suno-API/Suno-API) | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 🔄 Rerank | Cohere, Jina | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 💬 Claude | Messages format | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 🌐 Gemini | Google Gemini format | [Documentation](https://github.com/MAX-API-Next/MAX-API) |
| 🔧 Dify | ChatFlow mode | - |
| 🎯 Custom upstream | Supports configuring legally authorized upstream endpoints | - |

### 📡 Supported Interfaces

<details>
<summary>View complete interface list</summary>

- [Chat Interface (Chat Completions)](https://github.com/MAX-API-Next/MAX-API)
- [Response Interface (Responses)](https://github.com/MAX-API-Next/MAX-API)
- [Image Interface (Image)](https://github.com/MAX-API-Next/MAX-API)
- [Audio Interface (Audio)](https://github.com/MAX-API-Next/MAX-API)
- [Video Interface (Video)](https://github.com/MAX-API-Next/MAX-API)
- [Embedding Interface (Embeddings)](https://github.com/MAX-API-Next/MAX-API)
- [Rerank Interface (Rerank)](https://github.com/MAX-API-Next/MAX-API)
- [Realtime Conversation (Realtime)](https://github.com/MAX-API-Next/MAX-API)
- [Claude Chat](https://github.com/MAX-API-Next/MAX-API)
- [Google Gemini Chat](https://github.com/MAX-API-Next/MAX-API)

</details>

---

## 🚢 Deployment

> [!TIP]
> **Latest Docker image:** `max-api:latest`

### 📋 Deployment Requirements

| Component | Requirement |
|------|------|
| **Local database** | SQLite (Docker must mount `/data` directory)|
| **Remote database** | MySQL ≥ 5.7.8 or PostgreSQL ≥ 9.6 |
| **Container engine** | Docker / Docker Compose |

### ⚙️ Environment Variable Configuration

<details>
<summary>Common environment variable configuration</summary>

| Variable Name | Description | Default Value |
|--------|------|--------|
| `SESSION_SECRET` | Session secret (required for multi-machine deployment) | - |
| `CRYPTO_SECRET` | Encryption secret (required for Redis) | - |
| `SQL_DSN` | Database connection string | - |
| `REDIS_CONN_STRING` | Redis connection string | - |
| `STREAMING_TIMEOUT` | Streaming timeout (seconds) | `300` |
| `STREAM_SCANNER_MAX_BUFFER_MB` | Max per-line buffer (MB) for the stream scanner; increase when upstream sends huge image/base64 payloads | `64` |
| `MAX_REQUEST_BODY_MB` | Max request body size (MB, counted **after decompression**; prevents huge requests/zip bombs from exhausting memory). Exceeding it returns `413` | `32` |
| `AZURE_DEFAULT_API_VERSION` | Azure API version | `2025-04-01-preview` |
| `ERROR_LOG_ENABLED` | Error log switch | `false` |
| `PYROSCOPE_URL` | Pyroscope server address | - |
| `PYROSCOPE_APP_NAME` | Pyroscope application name | `max-api` |
| `PYROSCOPE_BASIC_AUTH_USER` | Pyroscope basic auth user | - |
| `PYROSCOPE_BASIC_AUTH_PASSWORD` | Pyroscope basic auth password | - |
| `PYROSCOPE_MUTEX_RATE` | Pyroscope mutex sampling rate | `5` |
| `PYROSCOPE_BLOCK_RATE` | Pyroscope block sampling rate | `5` |
| `HOSTNAME` | Hostname tag for Pyroscope | `max-api` |

📖 **Complete configuration:** [Environment Variables Documentation](https://github.com/MAX-API-Next/MAX-API)

</details>

### 🔧 Deployment Methods

<details>
<summary><strong>Method 1: Docker Compose (Recommended)</strong></summary>

```bash
# Clone the project
git clone https://github.com/MAX-API-Next/MAX-API.git
cd max-api

# Edit configuration
nano docker-compose.yml

# Start service
docker-compose up -d
```

</details>

<details>
<summary><strong>Method 2: Docker Commands</strong></summary>

**Using SQLite:**
```bash
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

**Using MySQL:**
```bash
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

> **💡 Path explanation:**
> - `./data:/data` - Relative path, data saved in the data folder of the current directory
> - You can also use absolute path, e.g.: `/your/custom/path:/data`

</details>

<details>
<summary><strong>Method 3: BaoTa Panel</strong></summary>

1. Install BaoTa Panel (≥ 9.2.0 version)
2. Search for **MAX-API** in the application store
3. One-click installation

📖 [Tutorial with images](./docs/BT.md)

</details>

### ⚠️ Multi-machine Deployment Considerations

> [!WARNING]
> - **Must set** `SESSION_SECRET` - Otherwise login status inconsistent
> - **Shared Redis must set** `CRYPTO_SECRET` - Otherwise data cannot be decrypted

### 🔄 Channel Retry and Cache

**Retry configuration:** `Settings → Operation Settings → General Settings → Failure Retry Count`

**Cache configuration:**
- `REDIS_CONN_STRING`: Redis cache (recommended)
- `MEMORY_CACHE_ENABLED`: Memory cache

---

## 🔗 Related Projects

### Upstream Projects

| Project | Description |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | Original project base |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Midjourney interface support |

### Supporting Tools

| Project | Description |
|------|------|
| [max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool) | Key quota query tool |
| [max-api-horizon](https://github.com/MAX-API-Next/MAX-API-horizon) | MAX API high-performance optimized version |

---

## 💬 Help Support

### 📖 Documentation Resources

| Resource | Link |
|------|------|
| 📘 FAQ | [FAQ](https://github.com/MAX-API-Next/MAX-API) |
| 💬 Community Interaction | [Communication Channels](https://github.com/MAX-API-Next/MAX-API) |
| 🐛 Issue Feedback | [Issue Feedback](https://github.com/MAX-API-Next/MAX-API) |
| 📚 Complete Documentation | [Official Documentation](https://github.com/MAX-API-Next/MAX-API) |

### 🤝 Contribution Guide

Welcome all forms of contribution!

- 🐛 Report Bugs
- 💡 Propose New Features
- 📝 Improve Documentation
- 🔧 Submit Code

---

## 📜 License

This project is licensed under the [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE).

Additional terms under AGPLv3 Section 7 apply. Modified versions must preserve
the author attribution notice `Frontend design and development by MAX API
contributors.` in the appropriate legal notices and in any prominent about,
legal, footer, or attribution location presented by the user interface.

Modified versions that present a user interface must also preserve a visible
link to the original project: <https://github.com/MAX-API-Next/MAX-API>.

This is an open-source project developed based on [One API](https://github.com/songquanpeng/one-api) (MIT License).

If your organization's policies do not permit the use of AGPLv3-licensed software, or if you wish to avoid the open-source obligations of AGPLv3, please contact us at: [GitHub Issues](https://github.com/MAX-API-Next/MAX-API/issues)

---

## 🌟 Star History

<div align="center">

[![Star History Chart](https://api.star-history.com/svg?repos=MAX-API-Next/MAX-API&type=Date)](https://star-history.com/#MAX-API-Next/MAX-API&Date)

</div>

---

<div align="center">

### 💖 Thank you for using MAX API

If this project is helpful to you, welcome to give us a ⭐️ Star！

**[Official Documentation](https://github.com/MAX-API-Next/MAX-API)** • **[Issue Feedback](https://github.com/MAX-API-Next/MAX-API/issues)** • **[Latest Release](https://github.com/MAX-API-Next/MAX-API/releases)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
