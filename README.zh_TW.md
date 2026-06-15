<div align="center">

![max-api](/web/default/public/logo.png)

# MAX API

🍥 **新一代大模型網關與AI資產管理系統**

<p align="center">
  繁體中文 |
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.md">English</a> |
  <a href="./README.fr.md">Français</a> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/MAX-API-Next/MAX-API/main/LICENSE">
    <img src="https://img.shields.io/github/license/MAX-API-Next/MAX-API?color=brightgreen" alt="license">
  </a>
  <a href="https://github.com/MAX-API-Next/MAX-API/releases/latest">
    <img src="https://img.shields.io/github/v/release/MAX-API-Next/MAX-API?color=brightgreen&include_prereleases" alt="release">
  </a>
  <a href="https://hub.docker.com/r/max-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a>
  <a href="https://goreportcard.com/report/github.com/MAX-API-Next/MAX-API">
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
  </a>
  <a href="https://www.producthunt.com/products/max-api/launches/max-api?embed=true&utm_source=badge-featured&utm_medium=badge&utm_campaign=badge-max-api" target="_blank" rel="noopener noreferrer">
    <img src="https://api.producthunt.com/widgets/embed-image/v1/featured.svg?post_id=1047693&theme=light&t=1769577875005" alt="MAX API - All-in-one AI asset management gateway. | Product Hunt" style="width: 250px; height: 54px;" width="250" height="54" />
  </a>
</p>

<p align="center">
  <a href="#-快速開始">快速開始</a> •
  <a href="#-主要特性">主要特性</a> •
  <a href="#-部署">部署</a> •
  <a href="#-文件">文件</a> •
  <a href="#-幫助支援">幫助</a>
</p>

</div>

## 📝 項目說明

> [!IMPORTANT]
> - 本專案僅面向合法授權的 AI API 閘道、組織內部鑑權、多模型管理、用量統計、成本核算和私有化部署場景。
> - 使用者必須合法取得上游 API Key、帳號、模型服務或介面權限，並遵守上游服務條款及適用法律法規。
> - 使用者應確保其使用方式符合上游服務條款及適用法律法規。
> - 面向公眾提供生成式人工智慧服務時，使用者應遵守[《生成式人工智慧服務管理暫行辦法》](http://www.cac.gov.cn/2023-07/13/c_1690898327029107.htm)等監管要求，自行完成所在司法轄區要求的備案、許可、內容安全、實名、日誌留存、稅務和上游授權等合規義務。

---

## 🤝 我們信任的合作伙伴

<p align="center">
  <em>排名不分先後</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="北京大學" height="80" />
  </a><!--
  --><a href="https://www.compshare.cn/?ytag=GPU_yy_gh_maxapi" target="_blank">
    <img src="./docs/images/ucloud.png" alt="UCloud 優刻得" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="阿里雲" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 特別鳴謝

<p align="center">
  <a href="https://www.jetbrains.com/?from=max-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong>感謝 <a href="https://www.jetbrains.com/?from=max-api">JetBrains</a> 為本項目提供免費的開源開發許可證</strong>
</p>

---

## 🚀 快速開始

### 使用 Docker Compose（推薦）

```bash
# 複製項目
git clone https://github.com/MAX-API-Next/MAX-API.git
cd max-api

# 編輯 docker-compose.yml 配置
nano docker-compose.yml

# 啟動服務
docker-compose up -d
```

<details>
<summary><strong>使用 Docker 命令</strong></summary>

```bash
# 拉取最新鏡像
docker pull max-api:latest

# 使用 SQLite（預設）
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest

# 使用 MySQL
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

> **💡 提示：** `-v ./data:/data` 會將數據保存在當前目錄的 `data` 資料夾中，你也可以改為絕對路徑如 `-v /your/custom/path:/data`

</details>

---

🎉 部署完成後，訪問 `http://localhost:3000` 即可使用！

> [!WARNING]
> 將本專案作為面向公眾的生成式 AI 服務或 API 轉售服務運營時，使用者應先完成備案、內容安全、實名、日誌留存、稅務、支付和上游授權等合規義務。

📖 更多部署方式請參考 [部署指南](https://github.com/MAX-API-Next/MAX-API)

---

## 📚 文件

<div align="center">

### 📖 [官方文件](https://github.com/MAX-API-Next/MAX-API) | [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/MAX-API-Next/MAX-API)

</div>

**快速導航：**

| 分類 | 連結 |
|------|------|
| 🚀 部署指南 | [安裝文件](https://github.com/MAX-API-Next/MAX-API) |
| ⚙️ 環境配置 | [環境變數](https://github.com/MAX-API-Next/MAX-API) |
| 📡 接口文件 | [API 文件](https://github.com/MAX-API-Next/MAX-API) |
| ❓ 常見問題 | [FAQ](https://github.com/MAX-API-Next/MAX-API) |
| 💬 社群交流 | [交流管道](https://github.com/MAX-API-Next/MAX-API) |

---

## ✨ 主要特性

> 詳細特性請參考 [特性說明](https://github.com/MAX-API-Next/MAX-API)

### 🎨 核心功能

| 特性 | 說明 |
|------|------|
| 🎨 全新 UI | 現代化的用戶界面設計 |
| 🌍 多語言 | 支援簡體中文、繁體中文、英文、法語、日語 |
| 🔄 數據兼容 | 完全兼容原版 One API 資料庫 |
| 📈 數據看板 | 視覺化控制檯與統計分析 |
| 🔒 權限管理 | 令牌分組、模型限制、用戶管理 |

### 💰 授權用量與成本管理

- ✅ 合法授權場景下的內部儲值與額度分配（易支付、Stripe）
- ✅ 組織內按次、按量或快取命中成本核算
- ✅ 支援 OpenAI、Azure、DeepSeek、Claude、Qwen 等模型的快取計費統計
- ✅ 面向內部管理或企業客戶的靈活計費策略配置

### 🔐 授權與安全

- 😈 Discord 授權登錄
- 🤖 LinuxDO 授權登錄
- 📱 Telegram 授權登錄
- 🔑 OIDC 統一認證
- 🔍 Key 查詢使用額度（配合 [max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool)）

### 🚀 高級功能

**API 格式支援：**
- ⚡ [OpenAI Responses](https://github.com/MAX-API-Next/MAX-API)
- ⚡ [OpenAI Realtime API](https://github.com/MAX-API-Next/MAX-API)（含 Azure）
- ⚡ [Claude Messages](https://github.com/MAX-API-Next/MAX-API)
- ⚡ [Google Gemini](https://github.com/MAX-API-Next/MAX-API)
- 🔄 [Rerank 模型](https://github.com/MAX-API-Next/MAX-API)（Cohere、Jina）

**智慧路由：**
- ⚖️ 管道加權隨機
- 🔄 失敗自動重試
- 🚦 用戶級別模型限流

**格式轉換：**
- 🔄 **OpenAI Compatible ⇄ Claude Messages**
- 🔄 **OpenAI Compatible → Google Gemini**
- 🔄 **Google Gemini → OpenAI Compatible** - 僅支援文本，暫不支援函數調用
- 🚧 **OpenAI Compatible ⇄ OpenAI Responses** - 開發中
- 🔄 **思考轉內容功能**

**Reasoning Effort 支援：**

<details>
<summary>查看詳細配置</summary>

**OpenAI 系列模型：**
- `o3-mini-high` - High reasoning effort
- `o3-mini-medium` - Medium reasoning effort
- `o3-mini-low` - Low reasoning effort
- `gpt-5-high` - High reasoning effort
- `gpt-5-medium` - Medium reasoning effort
- `gpt-5-low` - Low reasoning effort

**Claude 思考模型：**
- `claude-3-7-sonnet-20250219-thinking` - 啟用思考模式

**Google Gemini 系列模型：**
- `gemini-2.5-flash-thinking` - 啟用思考模式
- `gemini-2.5-flash-nothinking` - 禁用思考模式
- `gemini-2.5-pro-thinking` - 啟用思考模式
- `gemini-2.5-pro-thinking-128` - 啟用思考模式，並設置思考預算為128tokens
- 也可以直接在 Gemini 模型名稱後追加 `-low` / `-medium` / `-high` 來控制思考力道（無需再設置思考預算後綴）

</details>

---

## 🤖 模型支援

> 詳情請參考 [接口文件 - 閘道接口](https://github.com/MAX-API-Next/MAX-API)

| 模型類型 | 說明 | 文件 |
|---------|------|------|
| 🤖 OpenAI-Compatible | OpenAI 兼容模型 | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 🤖 OpenAI Responses | OpenAI Responses 格式 | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 🎨 Midjourney-Proxy | [Midjourney-Proxy(Plus)](https://github.com/novicezk/midjourney-proxy) | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 🎵 Suno-API | [Suno API](https://github.com/Suno-API/Suno-API) | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 🔄 Rerank | Cohere、Jina | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 💬 Claude | Messages 格式 | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 🌐 Gemini | Google Gemini 格式 | [文件](https://github.com/MAX-API-Next/MAX-API) |
| 🔧 Dify | ChatFlow 模式 | - |
| 🎯 自訂上游 | 支援配置合法授權的上游介面位址 | - |

### 📡 支援的接口

<details>
<summary>查看完整接口列表</summary>

- [聊天接口 (Chat Completions)](https://github.com/MAX-API-Next/MAX-API)
- [響應接口 (Responses)](https://github.com/MAX-API-Next/MAX-API)
- [圖像接口 (Image)](https://github.com/MAX-API-Next/MAX-API)
- [音訊接口 (Audio)](https://github.com/MAX-API-Next/MAX-API)
- [影片接口 (Video)](https://github.com/MAX-API-Next/MAX-API)
- [嵌入接口 (Embeddings)](https://github.com/MAX-API-Next/MAX-API)
- [重排序接口 (Rerank)](https://github.com/MAX-API-Next/MAX-API)
- [即時對話 (Realtime)](https://github.com/MAX-API-Next/MAX-API)
- [Claude 聊天](https://github.com/MAX-API-Next/MAX-API)
- [Google Gemini 聊天](https://github.com/MAX-API-Next/MAX-API)

</details>

---

## 🚢 部署

> [!TIP]
> **最新版 Docker 鏡像：** `max-api:latest`

### 📋 部署要求

| 組件 | 要求 |
|------|------|
| **本地資料庫** | SQLite（Docker 需掛載 `/data` 目錄）|
| **遠端資料庫** | MySQL ≥ 5.7.8 或 PostgreSQL ≥ 9.6 |
| **容器引擎** | Docker / Docker Compose |

### ⚙️ 環境變數配置

<details>
<summary>常用環境變數配置</summary>

| 變數名 | 說明                                                           | 預設值 |
|--------|--------------------------------------------------------------|--------|
| `SESSION_SECRET` | 會話密鑰（多機部署必須）                                                 | - |
| `CRYPTO_SECRET` | 加密密鑰（Redis 必須）                                               | - |
| `SQL_DSN` | 資料庫連接字符串                                                     | - |
| `REDIS_CONN_STRING` | Redis 連接字符串                                                  | - |
| `STREAMING_TIMEOUT` | 流式超時時間（秒）                                                    | `300` |
| `STREAM_SCANNER_MAX_BUFFER_MB` | 流式掃描器單行最大緩衝（MB），圖像生成等超大 `data:` 片段（如 4K 圖片 base64）需適當調大 | `64` |
| `MAX_REQUEST_BODY_MB` | 請求體最大大小（MB，**解壓縮後**計；防止超大請求/zip bomb 導致記憶體暴漲），超過將返回 `413` | `32` |
| `AZURE_DEFAULT_API_VERSION` | Azure API 版本                                                 | `2025-04-01-preview` |
| `ERROR_LOG_ENABLED` | 錯誤日誌開關                                                       | `false` |
| `PYROSCOPE_URL` | Pyroscope 服務位址                                            | - |
| `PYROSCOPE_APP_NAME` | Pyroscope 應用名                                        | `max-api` |
| `PYROSCOPE_BASIC_AUTH_USER` | Pyroscope Basic Auth 用戶名                        | - |
| `PYROSCOPE_BASIC_AUTH_PASSWORD` | Pyroscope Basic Auth 密碼                  | - |
| `PYROSCOPE_MUTEX_RATE` | Pyroscope mutex 採樣率                               | `5` |
| `PYROSCOPE_BLOCK_RATE` | Pyroscope block 採樣率                               | `5` |
| `HOSTNAME` | Pyroscope 標籤裡的主機名                                          | `max-api` |

📖 **完整配置：** [環境變數文件](https://github.com/MAX-API-Next/MAX-API)

</details>

### 🔧 部署方式

<details>
<summary><strong>方式 1：Docker Compose（推薦）</strong></summary>

```bash
# 複製項目
git clone https://github.com/MAX-API-Next/MAX-API.git
cd max-api

# 編輯配置
nano docker-compose.yml

# 啟動服務
docker-compose up -d
```

</details>

<details>
<summary><strong>方式 2：Docker 命令</strong></summary>

**使用 SQLite：**
```bash
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

**使用 MySQL：**
```bash
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

> **💡 路徑說明：**
> - `./data:/data` - 相對路徑，數據保存在當前目錄的 data 資料夾
> - 也可使用絕對路徑，如：`/your/custom/path:/data`

</details>

<details>
<summary><strong>方式 3：寶塔面板</strong></summary>

1. 安裝寶塔面板（≥ 9.2.0 版本）
2. 在應用商店搜尋 **MAX-API**
3. 一鍵安裝

📖 [圖文教學](./docs/BT.md)

</details>

### ⚠️ 多機部署注意事項

> [!WARNING]
> - **必須設置** `SESSION_SECRET` - 否則登錄狀態不一致
> - **公用 Redis 必須設置** `CRYPTO_SECRET` - 否則數據無法解密

### 🔄 管道重試與快取

**重試配置：** `設置 → 運營設置 → 通用設置 → 失敗重試次數`

**快取配置：**
- `REDIS_CONN_STRING`：Redis 快取（推薦）
- `MEMORY_CACHE_ENABLED`：記憶體快取

---

## 🔗 相關項目

### 上游項目

| 項目 | 說明 |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | 原版項目基礎 |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Midjourney 接口支援 |

### 配套工具

| 項目 | 說明 |
|------|------|
| [max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool) | Key 額度查詢工具 |
| [max-api-horizon](https://github.com/MAX-API-Next/MAX-API-horizon) | MAX API 高性能優化版 |

---

## 💬 幫助支援

### 📖 文件資源

| 資源 | 連結 |
|------|------|
| 📘 常見問題 | [FAQ](https://github.com/MAX-API-Next/MAX-API) |
| 💬 社群交流 | [交流管道](https://github.com/MAX-API-Next/MAX-API) |
| 🐛 回饋問題 | [問題回饋](https://github.com/MAX-API-Next/MAX-API) |
| 📚 完整文件 | [官方文件](https://github.com/MAX-API-Next/MAX-API) |

### 🤝 貢獻指南

歡迎各種形式的貢獻！

- 🐛 報告 Bug
- 💡 提出新功能
- 📝 改進文件
- 🔧 提交程式碼

---

## 📜 許可證

本項目採用 [GNU Affero 通用公共許可證 v3.0 (AGPLv3)](./LICENSE) 授權。

本項目為開源項目，在 [One API](https://github.com/songquanpeng/one-api)（MIT 許可證）的基礎上進行二次開發。

如果您所在的組織政策不允許使用 AGPLv3 許可的軟體，或您希望規避 AGPLv3 的開源義務，請發送郵件至：[GitHub Issues](https://github.com/MAX-API-Next/MAX-API/issues)

---

## 🌟 Star History

<div align="center">

[![Star History Chart](https://api.star-history.com/svg?repos=MAX-API-Next/MAX-API&type=Date)](https://star-history.com/#MAX-API-Next/MAX-API&Date)

</div>

---

<div align="center">

### 💖 感謝使用 MAX API

如果這個項目對你有幫助，歡迎給我們一個 ⭐️ Star！

**[官方文件](https://github.com/MAX-API-Next/MAX-API)** • **[問題回饋](https://github.com/MAX-API-Next/MAX-API/issues)** • **[最新發布](https://github.com/MAX-API-Next/MAX-API/releases)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
