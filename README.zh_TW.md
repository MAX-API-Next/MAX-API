# MAX API

<div align="center">

![MAX API](./web/default/public/logo.png)

**面向 AGI 應用時代的 AI Models and Agents governance、智慧運維與開放協作基礎設施**

**MAX API 2.0：開啟智慧運維時代 · 從統一模型閘道走向 AGI 原生治理與營運**

[查看 MAX API 2.0 Preview 發布說明](https://github.com/MAX-API-Next/MAX-API/releases/tag/v2.0.0-smartops.pre1)

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <strong>繁體中文</strong> |
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
  <a href="https://github.com/MAX-API-Next/MAX-API"><strong>⭐ Star 專案</strong></a> •
  <a href="#-加入-max-api-next-社群"><strong>💬 加入社群</strong></a> •
  <a href="https://docs.max-api.ai"><strong>📚 查看文件</strong></a> •
  <a href="https://github.com/MAX-API-Next/MAX-API/releases"><strong>🚀 取得版本</strong></a>
</p>

<p align="center">
  <a href="#-加入-max-api-next-社群">加入社群</a> •
  <a href="#-快速開始">快速開始</a> •
  <a href="#-為什麼需要-max-api">為什麼需要</a> •
  <a href="#-面向-agi-的技術方向">AGI 技術方向</a> •
  <a href="#-目前能力">目前能力</a> •
  <a href="#-智慧運維中心">智慧運維</a> •
  <a href="#-preview-部署">Preview 部署</a>
</p>

</div>

---

MAX API 位於應用程式、Agent 與上游模型服務之間，是統一的模型閘道、治理控制平面和營運入口。MAX-API-Next 社群圍繞 AGI 工程化持續建設本專案，並向開發者、研究者、企業工程團隊、大學技術愛好者與開源貢獻者開放協作。我們的目標是建設 AGI 應用真正需要的模型接入、權限、成本、Evidence、智慧運維和安全控制基礎。

我們將在 AGI 領域：**持續交付先進且可驗證的技術、將真實生產問題沉澱為開放工程能力、建立能夠共同研究與貢獻的社群。**

## 🌐 加入 MAX-API-Next 社群

MAX API 不只是一個程式碼倉庫，也是一項面向 AI Models、Agents、AgentOps 與 AGI 工程化的長期開放協作。無論你正在部署模型閘道、開發 Agent、適配新模型、研究評測與治理，或願意改善文件和國際化，都歡迎加入社群交流並參與共建。

**加入社群，你可以更快取得版本動態、模型與協定變化、部署經驗、問題排查線索和社群共建機會。**

<p align="center">
  <strong>QQ 群：950126533</strong> •
  <strong>微信群：搜尋 MAX-API</strong>
</p>

| 社群入口 | 用途 |
|---|---|
| [MAX-API-Next GitHub](https://github.com/MAX-API-Next) | 關注社群專案、技術方向與開放協作 |
| [MAX API Issues](https://github.com/MAX-API-Next/MAX-API/issues) | 提交可重現問題、需求建議與相容性變化 |
| [MAX API Releases](https://github.com/MAX-API-Next/MAX-API/releases) | 取得正式版與 Preview 版本動態 |
| [Ask DeepWiki](https://deepwiki.com/MAX-API-Next/MAX-API) | 快速檢索與理解專案程式碼 |
| 技術與生態合作 | 聯絡 `maxapi@max-api.ai` |

### 我們正在尋找這些共建者

- **模型與協定貢獻者**：適配新模型、新供應商、Reasoning、工具呼叫和多模態任務協定。
- **Agent 與應用開發者**：分享 Dify、RAGFlow、Codex、MCP、工作流程和研究 Agent 的接入與治理實踐。
- **可靠性與安全工程師**：完善跨資料庫測試、非同步任務復原、結算安全、快取一致性和權限邊界。
- **文件與社群貢獻者**：改善部署教學、FAQ、案例、翻譯、可重現實驗和新貢獻者指引。
- **研究者與評測開發者**：共建 Evidence、Evaluator、Detector、Runbook 和受控自治方案。

<details>
<summary><strong>查看共建方向、協作原則與參與方式</strong></summary>

| 共建方向 | 適合的貢獻 |
|---|---|
| 模型與供應商生態 | 模型/協定適配、相容性測試、模型清單與棄用變化、渠道設定範本 |
| AgentOps 與 AGI 應用治理 | Agent 接入範例、權杖與權限邊界、成本治理和運行實踐 |
| SmartOps 與 Evidence | 脫敏故障樣本、指標口徑、告警規則、資料品質說明、診斷與評測方法 |
| 可靠性、計費與安全 | 冪等結算、非同步任務復原、跨資料庫迴歸、快取一致性和高風險操作驗證 |
| 文件、教學與國際化 | 部署教學、架構說明、FAQ、案例、翻譯和新貢獻者指引 |

協作時請盡量提供最小重現、版本、環境、脫敏日誌或測試證據；協定、資料庫和使用者契約變更需要說明相容性、遷移與回復路徑。請勿提交真實 API Key、客戶資料、支付原始記錄、私人日誌或未經授權的資料。涉及資金、安全、金鑰、生產資料或自動執行的貢獻，需要更嚴格的測試、獨立審查和明確責任人。

你可以從以下方式開始：

1. 提交一個可重現的問題、協定差異或模型相容性變化。
2. 為既有問題補充失敗測試、跨資料庫案例、前端迴歸或脫敏 Evidence。
3. 完善模型/渠道設定、部署說明、FAQ、架構文件或多語言翻譯。
4. 分享經過脫敏的 Agent 接入、成本治理、SmartOps 和私有化部署實踐。
5. 提交 Evaluator、Detector、Runbook 或受控自治設計提案，並明確能力邊界與風險。

</details>

配套文件：[MAX-API-Docs](https://github.com/MAX-API-Next/MAX-API-Docs)（部署、設定與使用說明）。

歡迎模型廠商、Agent/工作流程專案、開源社群、研究者和企業工程團隊展開技術共建。正式合作、聯合發布或以機構名義使用，應取得雙方明確授權。

> [!IMPORTANT]
> 面向公眾提供生成式人工智慧服務時，使用者應自行完成所在司法管轄區要求的上游授權、備案許可、內容安全、實名、日誌留存、稅務、支付和使用者協議等合規義務。日誌稽核和內容留存僅應在具備合法依據、明確告知、權限隔離和資料安全措施的場景中啟用。

<img width="1902" height="1031" alt="MAX API 管理控制台" src="https://github.com/user-attachments/assets/fa481602-1e75-4326-9275-3c8271d01f5b" />

## 🧠 面向 AGI 的技術方向

AGI 應用不會長期依賴單一模型、單一協定或一次性請求。它們需要跨模型推理、工具呼叫、多模態任務、長時間運行、成本約束、生產 Evidence 和可復原的失敗處理。MAX API 選擇從這些可驗證的工程問題出發，建設 AI Models and Agents governance 基礎。

| 技術方向 | 目前基礎 | 對 AGI 應用的價值 |
|---|---|---|
| 多模型與多協定接入 | OpenAI Compatible、Responses、Claude Messages、Gemini、Realtime、多模態與非同步任務協定 | 讓應用和 Agent 面對相對穩定的入口，並能隨模型生態變化持續遷移 |
| 推理與工具上下文相容 | Reasoning Effort、工具定義、Tool Call、工具回應關聯和多輪上下文轉換 | 減少跨供應商切換時推理資訊與工具呼叫語意遺失 |
| 治理控制平面 | 使用者、權杖、模型範圍、分組、路由、限流、額度、價格和管理員權限 | 為不同 Agent、環境和任務建立獨立身分、存取邊界與預算邊界 |
| 可復原計費與任務生命週期 | 預扣、最終結算、冪等記錄、失敗退款、非同步任務輪詢和人工對帳狀態 | 避免長任務、重試和異常時間窗造成重複扣費、錯誤退款或不可追溯結果 |
| Evidence 與智慧運維 | 日誌、錯誤、重試、效能桶、活動告警、渠道/模型效能和結算證據 | 讓診斷、評測和未來 Agent 建議建立在可追溯事實上，而不是只依賴提示詞推斷 |
| 安全與組織治理 | Passkey、2FA、分作用域重新驗證、工作階段撤銷、稽核和敏感操作限流 | 為高風險設定、憑證和營運操作保留明確責任邊界 |
| 受控自治與工程進化 | **長期藍圖**：Policy、Budget、Approval、Shadow、Canary、Rollback、隔離 Coding Workspace | 讓自動化能力先被評測、約束和稽核，再逐步進入低風險生產動作 |

### 技術原則

- **Evidence before Action**：先建立可驗證事實，再進行診斷、建議或自動動作。
- **Governance before Autonomy**：身分、權限、預算、審批、稽核和回復必須先於自治能力。
- **One Billing Truth**：生產計費、額度與結算保持單一事實來源，不為 Agent 或外掛建立第二套帳務邏輯。
- **Compatibility by Design**：持續支援 SQLite、MySQL、PostgreSQL、多供應商協定和可遷移的應用端契約。
- **Open Collaboration, Safe Boundaries**：開放協定適配、測試、文件、評測和治理方案，高風險生產、資金、金鑰與發布動作仍由明確責任人審批。

### MAX API 2.0 技術亮點

- **Evidence-driven SmartOps**：將資源告警、渠道/模型效能、資料品質狀態和計費結算證據匯聚到統一運維入口，並保留人工審閱與真實財務狀態之間的邊界。
- **面向先進模型協定持續演進**：增強 Responses、Claude Messages、Gemini 與 Ollama 的 Reasoning、快取鍵、懲罰參數、工具定義和多輪 Tool Context 相容。
- **可復原的計費與非同步任務語意**：透過冪等 settlement、持久化 effect、任務 ID 和明確的 pending/manual 狀態，避免異常重試導致重複扣費、錯誤退款或任務遺失。
- **高風險操作分作用域重新驗證**：Passkey、2FA、Telegram、API Token 與工作階段撤銷採用 scope-bound step-up verification，並透過 `session_generation` 及時撤銷舊工作階段。
- **跨資料庫與持續驗證**：核心資料路徑相容 SQLite、MySQL 和 PostgreSQL；Go 測試、前端 Bun 測試、TypeScript 型別檢查、JSON 包裝規則和測試鏡像同步共同構成發布門檻。

## 💡 為什麼需要 MAX API

| 維度 | 直接連接多個供應商 | 使用 MAX API |
|---|---|---|
| 應用接入 | 分別維護 SDK、協定、鑑權和錯誤格式 | 應用端使用相對穩定的統一入口 |
| 模型切換 | 修改程式碼、金鑰和部署設定 | 透過渠道、模型映射、分組和路由調整 |
| 可用性 | 每個應用自行處理重試和上游故障 | 集中設定權重、優先級、失敗重試和切換 |
| 權限與金鑰 | 憑證散落在應用和環境變數中 | 集中管理權杖、模型範圍、額度和有效期 |
| 成本核算 | 多家帳單分散，難以歸因 | 按使用者、權杖、模型、渠道和分組統計 |
| 故障排查 | 日誌分散，跨供應商定位困難 | 在閘道端統一觀察請求、錯誤、重試和耗時 |

一句話概括：**供應商負責提供模型，Agent 框架負責編排業務，MAX API 負責統一接入並守住治理邊界。**

## 🚀 快速開始

本機體驗預設使用 SQLite，只需 Docker：

```bash
MAX_API_IMAGE=cscitechtop/max-api:latest@sha256:006d5d86887a261baab4d71ec3797d429e3771a4836e5899734aee0e7f66f2ab

docker pull "$MAX_API_IMAGE"

docker run --name max-api -d --restart always -p 127.0.0.1:3000:3000 -e TZ=Asia/Shanghai -v ./data:/data "$MAX_API_IMAGE"
```

啟動後存取：<http://localhost:3000>

接著完成三件事：

1. 建立或確認管理員帳號。
2. 新增一個具有合法授權的上游渠道和 API Key。
3. 建立存取權杖，將應用中的 Base URL 指向 MAX API。

> [!TIP]
> 生產環境建議使用正式版。Preview 版本用於測試和灰度驗證，升級前請備份資料庫並準備回復方案。
>
> [!WARNING]
> SQLite 適合本機體驗、開發和小規模測試。正式環境建議使用仍在供應商安全支援週期內的 MySQL（建議 8.4 LTS）或 PostgreSQL（建議 14+），並設定 Redis、HTTPS、備份和復原方案。專案相容性下限仍為 MySQL ≥ 5.7.8、PostgreSQL ≥ 9.6，但不建議將其用於生產環境。

## ✨ 目前能力

以下能力已在目前系統中提供：

| 能力 | 主要用途 |
|---|---|
| 統一模型入口 | 接入 OpenAI Compatible、Responses、Claude Messages、Gemini、Realtime 和多模態任務介面 |
| 多供應商路由 | 管理渠道、權重、優先級、分組、模型映射、失敗重試和跨供應商切換 |
| 身分與存取控制 | 管理使用者、權杖、模型範圍、分組、額度、有效期、限流和管理員權限 |
| 成本與計費 | 支援倍率、固定價格、表達式計費、非同步任務 rate-card、預扣費、結算和失敗退款 |
| 日誌與稽核 | 按使用者、權杖、模型、渠道、分組和節點查看使用、錯誤、重試和管理操作 |
| 智慧運維中心 | 集中查看活動告警、渠道效能、模型效能、系統資訊和計費結算對帳證據，協助管理員發現、定位和審閱生產問題 |
| 私有化部署 | 支援 SQLite、MySQL、PostgreSQL、Redis、多節點和獨立日誌庫 |
| 上游擴充 | 支援協定適配、路徑覆寫、參數/Header 覆寫、模型發現和任務狀態映射 |

### 適用場景

- **團隊或組織內部模型閘道**：統一管理使用者、權杖、模型、供應商、權限和費用。
- **AI 應用與 Agent 運行底座**：為應用、Agent 和工作流程提供模型存取控制、成本歸因與異常定位。
- **多供應商容錯與遷移**：透過模型映射、加權路由、失敗重試和灰度切換降低單一上游風險。
- **多模態任務治理**：統一管理圖像、音訊、影片、嵌入、重排序和即時對話介面。
- **私有化與合規營運**：自主管理金鑰、資料、日誌、稽核、價格和部署環境。

## 🩺 智慧運維中心

**智慧運維中心是 MAX API 2.0 的重大更新，也是專案從統一模型閘道走向 AGI 原生治理與營運基礎設施的關鍵一步。**

它將生產觀測、資源告警、模型與渠道效能、系統資訊和計費結算對帳集中到統一管理員入口。目前能力強調「看見問題、保留證據、通知管理員、受控審閱」，並不是會自動修改渠道、路由、餘額或主機的自治 Agent。

| 模組 | 目前提供的內容 |
|---|---|
| 活動告警 | 對目前節點的 CPU、記憶體和磁碟持續超閾值進行去重告警，在復原時傳送復原通知；沿用管理員既有的 Email、Webhook、Bark 或 Gotify 設定 |
| 渠道效能 | 查看請求與錯誤、消耗額度、估算成功率、日誌延遲、重試、探測延遲和最近觀測時間；詳細資訊可查看該渠道最近 24 小時的模型與分組表現 |
| 模型效能 | 彙總所有模型的渠道數、請求與錯誤、消耗額度、估算成功率、日誌延遲、吞吐量和重試；詳細資訊提供各分組效能、延遲趨勢和可用率趨勢 |
| 計費結算對帳 | 顯示 `pending` / `manual` 正向最終結算、未結資金、重試和錯誤證據；根管理員可設定預設使用者阻斷策略，管理員可按 `id + revision` 原子批次審閱並關閉告警 |
| 系統資訊 | 查看節點、執行個體和系統任務等資訊；此模組仍要求超級管理員權限 |

活動告警頁面每 5 秒讀取一次目前告警狀態，但不會觸發新的檢測或修復動作。渠道與模型清單預設查詢最近 1 小時，管理員可以輸入 `1–168` 小時的自訂時間窗；它們不會自動反覆統計大型日誌庫，只有按一下「套用篩選」或「重新整理」時才執行查詢，詳細資料在開啟時按需載入。

計費結算對帳將財務復原狀態與運維告警狀態嚴格分離：「審閱並關閉」只會記錄管理員審閱並關閉目前告警，不會將結算標記為 `applied`，不會修改餘額、已套用差額或 effect 狀態。批次審閱綁定目前財務 revision；重新整理後記錄發生變化時，舊選擇會自動失效，避免使用過期證據進行操作。

> [!NOTE]
> 目前生產效能主要彙總既有 Consume/Error 日誌和 `perf_metrics`：估算成功率並非完整的 Relay Attempt 成功率，吞吐量與趨勢屬於效能桶級近似值。日誌關閉、歷史資料缺失、採集關閉、時間窗無樣本或查詢失敗時，頁面會顯示相應的資料品質狀態。
>
> 活動告警依賴效能監控和資源閾值設定；閾值設為 `0` 時代表關閉該資源告警，需要連續兩個有效樣本才會觸發。狀態與通知佇列只保存在目前處理程序記憶體中：處理程序重新啟動後不會保留，多節點也不會自動合併為跨節點 Incident。渠道、模型和系統觀測保持唯讀；結算審閱只更新審閱中繼資料與使用者阻斷策略，不執行資金結算。智慧運維中心不會自動測試、停用、調權、切換渠道或修復主機。

此階段的價值，是先完成「看見問題、通知管理員、提供證據、受控審閱」的閉環，為後續統一 Evidence、診斷 Agent、Evaluator 和受控自動化建立基礎。

## 🔌 模型、介面與擴充

> 實際可用能力取決於你的上游授權、渠道設定、模型映射和供應商支援。MAX API 負責治理這些能力，不提供模型服務本身。

| 類別 | 介面或能力 |
|---|---|
| 通用模型介面 | Chat Completions、Responses、Embeddings、Rerank、Images、Audio、Video |
| 原生與即時協定 | Claude Messages、Google Gemini、OpenAI Realtime 等入口 |
| 推理與工具呼叫 | 支援 Reasoning Effort、函式工具、Tool Call ID、工具名稱和多輪工具回應關聯，並依不同上游能力進行協定轉換 |
| 非同步任務 | 任務提交、輪詢、狀態映射、結果代理和參數化計費 |
| 自訂上游 | Base URL、路徑、參數、Header、狀態欄位和結果欄位映射 |

涵蓋 OpenAI、Claude、Gemini、Azure、AWS Bedrock、Vertex AI、Ollama 及多種中國模型平台，也可治理 Codex、Dify、RAGFlow 和多模態任務服務。具體支援範圍以目前版本和渠道類型為準。

### 系統運作方式與技術堆疊

![MAX API 系統架構圖](./docs/images/MAX-API架构图.png)

```text
應用 / SDK / Agent
  → 統一介面與身分鑑權
  → 模型權限、限流、預算和安全檢查
  → 渠道選擇、映射與失敗重試
  → 上游協定適配
  → 可復原結算、Evidence、日誌和稽核
  → 智慧運維中心與管理員治理
```

後端使用 Go、Gin 和 GORM，前端使用 React 19、TypeScript、Base UI 與 Tailwind CSS，資料層相容 SQLite、MySQL 和 PostgreSQL，並可使用 Redis 與獨立日誌庫。供應商協定適配位於獨立 Relay/Channel 層，計費與結算集中在統一服務邊界中，管理端透過 SmartOps 顯示唯讀觀測和受限治理入口。

## 🛡️ 治理與營運

生產環境建議依以下順序設定：

1. 設定登入、安全限制和使用者註冊策略。
2. 新增合法授權的上游渠道，確認模型、能力和協定設定。
3. 按團隊、業務或環境設定分組、權杖、模型範圍、額度和價格。
4. 為每個應用、Agent 或環境使用獨立權杖，避免共用憑證和成本歸屬。
5. 設定重試、日誌與告警，透過資料看板和智慧運維中心持續觀察。

渠道能力驗證、表達式計費、通用任務協定、管理員稽核和效能參數等進階設定，請查看[詳細文件](https://docs.max-api.ai)。

## 🧭 演進路線

MAX API 將繼續以 **AI Models and Agents governance** 為核心，從統一閘道、智慧運維和可復原結算出發，逐步建設面向 AGI 應用的 Evidence、評測、策略與受控執行能力。長期方向不是讓一個無邊界 Agent 接管生產系統，而是建立可驗證、可審批、可停止、可回復的工程閉環。

| 階段 | 狀態 | 重點 |
|---|---|---|
| 統一閘道與智慧運維 | **目前已提供** | 接入、鑑權、路由、計費、日誌、資源告警、渠道/模型效能、系統資訊和計費結算對帳 |
| Evidence 事實層 | **近期建設** | 統一模型請求、系統日誌、指標、Task、路由、策略、結算和稽核事件，向 Agent 提供脫敏、限權、唯讀介面 |
| 開放評測與治理範本 | **規劃中** | 與社群沉澱模型/協定相容測試、匿名故障樣本、評測集、Runbook、Detector 和產業治理範本 |
| 受控自治運維 | **長期藍圖** | 在 Policy、Budget、Approval、Shadow、Canary 和 Rollback 約束下評估低風險自動動作 |
| 受控能力進化 | **長期藍圖** | 在隔離 Coding Workspace 中生成、測試和審閱候選改進，不直接修改生產系統 |
| AGI 工程閉環 | **長期方向** | 將 Evidence、評測、治理策略、人工審批與可回復執行連接為可驗證閉環 |

MAX API 不是基礎模型，也不宣稱目前已經實現 AGI 或自治運維。路線圖中的長期能力只有在證據、權限、預算、審批和回復邊界完善後才會逐步驗證。

## 🚢 Preview 部署

以下步驟用於 Preview 版本的測試和灰度驗證。生產環境請改用正式版，並沿用相同的驗證和回復流程。

建議使用 Docker Compose：

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

# 修改 docker-compose.yml 中的資料庫、Redis 密碼和金鑰
docker compose up -d
```

### 部署檢查

| 元件 | 建議 |
|---|---|
| 資料庫 | 使用仍在供應商安全支援週期內的 MySQL（建議 8.4 LTS）或 PostgreSQL（建議 14+），並設定備份與復原 |
| 快取 | 單機可使用記憶體快取，多節點部署建議使用 Redis |
| 入口 | 設定 HTTPS 反向代理、請求大小限制和可信任網路策略 |
| 金鑰 | 明確設定隨機 `SESSION_SECRET`；`CRYPTO_SECRET` 為可選覆寫項，未設定時回退到 `SESSION_SECRET`，如單獨設定則需在 Redis/多節點場景統一 |
| 節點 | 每個節點設定穩定且唯一的 `NODE_NAME` |
| 日誌 | 依合規和運維需要設定 `LOG_SQL_DSN`、清理與保留策略 |

多節點必須統一 `SESSION_SECRET`，並為每個節點使用不同的 `NODE_NAME`。`CRYPTO_SECRET` 為可選覆寫項，未設定時使用 `SESSION_SECRET`；如明確設定，所有節點必須使用相同值。獨立日誌庫使用 `LOG_SQL_DSN`；錯誤效能統計需要按需啟用 `ERROR_LOG_ENABLED`。完整環境變數和原始碼建置說明請見[詳細文件](https://docs.max-api.ai)。

## 🤝 法律說明與二次開發

如果你基於本專案進行二次開發或散布，請先完整閱讀 [NOTICE](./NOTICE) 和 [LICENSE](./LICENSE)，並依其中要求保留法律聲明、歸屬資訊、原專案連結和修改標記。

如果你基於本專案進行二次開發且僅供自用，滿足 NOTICE 中的展示與歸屬要求後，可依專案方最新公告取得適用的非永久臨時商用授權，無需另外申請或等待確認。該授權僅涵蓋 MAX API 專案方有權授權的部分，不取代或免除任何適用的上游許可義務。

## 📜 許可證

本專案採用 [GNU Affero 通用公共許可證 v3.0（AGPLv3）](./LICENSE) 授權。

臨時商用授權僅涵蓋 MAX API 專案方有權授權的新增與修改部分，不取代或免除任何適用的上游許可義務。

如果你修改並透過網路向使用者提供本專案服務，請理解並遵守 AGPLv3 對應的原始碼提供等義務。長期商用授權、機構合作或其他授權問題，請聯絡：maxapi@max-api.ai。

---

<div align="center">

### 💖 感謝使用 MAX API

如果這個專案對你有幫助，歡迎給我們一個 ⭐ Star、關注 Releases、提交可重現 Issue，或參與 MAX-API-Next 社群共建。

**[專案倉庫](https://github.com/MAX-API-Next/MAX-API)** • **[參與共建](https://github.com/MAX-API-Next/MAX-API/issues)** • **[最新發布](https://github.com/MAX-API-Next/MAX-API/releases)** • **[MAX-API-Next 社群](https://github.com/MAX-API-Next)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
