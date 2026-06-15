<div align="center">

![max-api](/web/default/public/logo.png)

# MAX API

🍥 **次世代大規模モデルゲートウェイとAI資産管理システム**

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.md">English</a> |
  <a href="./README.fr.md">Français</a> |
  <strong>日本語</strong>
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
  <a href="#-クイックスタート">クイックスタート</a> •
  <a href="#-主な機能">主な機能</a> •
  <a href="#-デプロイ">デプロイ</a> •
  <a href="#-ドキュメント">ドキュメント</a> •
  <a href="#-ヘルプサポート">ヘルプ</a>
</p>

</div>

## 📝 プロジェクト説明

> [!IMPORTANT]
> - 本プロジェクトは、合法的に許可された AI API ゲートウェイ、組織レベルの認証、マルチモデル管理、利用量分析、コスト管理、プライベートデプロイのシナリオのみを対象としています。
> - ユーザーは、上流の API キー、アカウント、モデルサービス、インターフェース権限を合法的に取得し、上流のサービス利用規約および適用される法律法規を遵守する必要があります。
> - ユーザーは、利用方法が上流のサービス利用規約および適用される法律法規に準拠していることを確認してください。
> - 生成 AI サービスを公衆に提供する場合、ユーザーは適用される規制要件を遵守し、管轄区域で求められる届出、ライセンス、コンテンツセキュリティ、本人確認、ログ保持、税務、上流認可などのすべての義務を履行してください。

---

## 🤝 信頼できるパートナー

<p align="center">
  <em>順不同</em>
</p>

<p align="center">
  <a href="https://www.cherry-ai.com/" target="_blank">
    <img src="./docs/images/cherry-studio.png" alt="Cherry Studio" height="80" />
  </a><!--
  --><a href="https://github.com/iOfficeAI/AionUi/" target="_blank">
    <img src="./docs/images/aionui.png" alt="Aion UI" height="80" />
  </a><!--
  --><a href="https://bda.pku.edu.cn/" target="_blank">
    <img src="./docs/images/pku.png" alt="北京大学" height="80" />
  </a><!--
  --><a href="https://www.compshare.cn/?ytag=GPU_yy_gh_maxapi" target="_blank">
    <img src="./docs/images/ucloud.png" alt="UCloud 優刻得" height="80" />
  </a><!--
  --><a href="https://www.aliyun.com/" target="_blank">
    <img src="./docs/images/aliyun.png" alt="Alibaba Cloud" height="80" />
  </a><!--
  --><a href="https://io.net/" target="_blank">
    <img src="./docs/images/io-net.png" alt="IO.NET" height="80" />
  </a>
</p>

---

## 🙏 特別な感謝

<p align="center">
  <a href="https://www.jetbrains.com/?from=max-api" target="_blank">
    <img src="https://resources.jetbrains.com/storage/products/company/brand/logos/jb_beam.png" alt="JetBrains Logo" width="120" />
  </a>
</p>

<p align="center">
  <strong>感謝 <a href="https://www.jetbrains.com/?from=max-api">JetBrains</a> が本プロジェクトに無料のオープンソース開発ライセンスを提供してくれたことに感謝します</strong>
</p>

---

## 🚀 クイックスタート

### Docker Composeを使用（推奨）

```bash
# プロジェクトをクローン
git clone https://github.com/MAX-API-Next/MAX-API.git
cd max-api

# docker-compose.yml 設定を編集
nano docker-compose.yml

# サービスを起動
docker-compose up -d
```

<details>
<summary><strong>Dockerコマンドを使用</strong></summary>

```bash
# 最新のイメージをプル
docker pull max-api:latest

# SQLiteを使用（デフォルト）
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest

# MySQLを使用
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

> **💡 ヒント:** `-v ./data:/data` は現在のディレクトリの `data` フォルダにデータを保存します。絶対パスに変更することもできます：`-v /your/custom/path:/data`

</details>

---

🎉 デプロイが完了したら、`http://localhost:3000` にアクセスして使用を開始してください！

> [!WARNING]
> 本プロジェクトを公衆向け生成 AI サービスまたは API 再販サービスとして運営する場合、ユーザーは届出、コンテンツセキュリティ、本人確認、ログ保持、税務、決済、上流認可などの必要なコンプライアンス義務を先に完了してください。

📖 その他のデプロイ方法については[デプロイガイド](https://github.com/MAX-API-Next/MAX-API)を参照してください。

---

## 📚 ドキュメント

<div align="center">

### 📖 [公式ドキュメント](https://github.com/MAX-API-Next/MAX-API) | [![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/MAX-API-Next/MAX-API)

</div>

**クイックナビゲーション:**

| カテゴリ | リンク |
|------|------|
| 🚀 デプロイガイド | [インストールドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| ⚙️ 環境設定 | [環境変数](https://github.com/MAX-API-Next/MAX-API) |
| 📡 APIドキュメント | [APIドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| ❓ よくある質問 | [FAQ](https://github.com/MAX-API-Next/MAX-API) |
| 💬 コミュニティ交流 | [交流チャネル](https://github.com/MAX-API-Next/MAX-API) |

---

## ✨ 主な機能

> 詳細な機能については[機能説明](https://github.com/MAX-API-Next/MAX-API)を参照してください。

### 🎨 コア機能

| 機能 | 説明 |
|------|------|
| 🎨 新しいUI | モダンなユーザーインターフェースデザイン |
| 🌍 多言語 | 簡体字中国語、繁体字中国語、英語、フランス語、日本語をサポート |
| 🔄 データ互換性 | オリジナルのOne APIデータベースと完全に互換性あり |
| 📈 データダッシュボード | ビジュアルコンソールと統計分析 |
| 🔒 権限管理 | トークングループ化、モデル制限、ユーザー管理 |

### 💰 認可済み利用量とコスト管理

- ✅ 合法的に許可されたシナリオでの内部チャージとクォータ割り当て（EPay、Stripe）
- ✅ 組織レベルのリクエスト単位、使用量ベース、キャッシュヒットのコスト会計
- ✅ OpenAI、Azure、DeepSeek、Claude、Qwen などのモデルのキャッシュ課金統計
- ✅ 内部管理または認可済み企業顧客向けの柔軟な課金ポリシー

### 🔐 認証とセキュリティ

- 😈 Discord認証ログイン
- 🤖 LinuxDO認証ログイン
- 📱 Telegram認証ログイン
- 🔑 OIDC統一認証
- 🔍 Key使用量クォータ照会（[max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool)と併用）



### 🚀 高度な機能

**APIフォーマットサポート:**
- ⚡ [OpenAI Responses](https://github.com/MAX-API-Next/MAX-API)
- ⚡ [OpenAI Realtime API](https://github.com/MAX-API-Next/MAX-API)（Azureを含む）
- ⚡ [Claude Messages](https://github.com/MAX-API-Next/MAX-API)
- ⚡ [Google Gemini](https://github.com/MAX-API-Next/MAX-API)
- 🔄 [Rerankモデル](https://github.com/MAX-API-Next/MAX-API)（Cohere、Jina）

**インテリジェントルーティング:**
- ⚖️ チャネル重み付けランダム
- 🔄 失敗自動リトライ
- 🚦 ユーザーレベルモデルレート制限

**フォーマット変換:**
- 🔄 **OpenAI Compatible ⇄ Claude Messages**
- 🔄 **OpenAI Compatible → Google Gemini**
- 🔄 **Google Gemini → OpenAI Compatible** - テキストのみ、関数呼び出しはまだサポートされていません
- 🚧 **OpenAI Compatible ⇄ OpenAI Responses** - 開発中
- 🔄 **思考からコンテンツへの機能**

**Reasoning Effort サポート:**

<details>
<summary>詳細設定を表示</summary>

**OpenAIシリーズモデル:**
- `o3-mini-high` - 高思考努力
- `o3-mini-medium` - 中思考努力
- `o3-mini-low` - 低思考努力
- `gpt-5-high` - 高思考努力
- `gpt-5-medium` - 中思考努力
- `gpt-5-low` - 低思考努力

**Claude思考モデル:**
- `claude-3-7-sonnet-20250219-thinking` - 思考モードを有効にする

**Google Geminiシリーズモデル:**
- `gemini-2.5-flash-thinking` - 思考モードを有効にする
- `gemini-2.5-flash-nothinking` - 思考モードを無効にする
- `gemini-2.5-pro-thinking` - 思考モードを有効にする
- `gemini-2.5-pro-thinking-128` - 思考モードを有効にし、思考予算を128トークンに設定する
- Gemini モデル名の末尾に `-low` / `-medium` / `-high` を付けることで推論強度を直接指定できます（追加の思考予算サフィックスは不要です）。

</details>

---

## 🤖 モデルサポート

> 詳細については[APIドキュメント - ゲートウェイインターフェース](https://github.com/MAX-API-Next/MAX-API)

| モデルタイプ | 説明 | ドキュメント |
|---------|------|------|
| 🤖 OpenAI-Compatible | OpenAI互換モデル | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 🤖 OpenAI Responses | OpenAI Responsesフォーマット | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 🎨 Midjourney-Proxy | [Midjourney-Proxy(Plus)](https://github.com/novicezk/midjourney-proxy) | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 🎵 Suno-API | [Suno API](https://github.com/Suno-API/Suno-API) | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 🔄 Rerank | Cohere、Jina | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 💬 Claude | Messagesフォーマット | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 🌐 Gemini | Google Geminiフォーマット | [ドキュメント](https://github.com/MAX-API-Next/MAX-API) |
| 🔧 Dify | ChatFlowモード | - |
| 🎯 カスタム上流 | 合法的に許可された上流エンドポイントの設定をサポート | - |

### 📡 サポートされているインターフェース

<details>
<summary>完全なインターフェースリストを表示</summary>

- [チャットインターフェース (Chat Completions)](https://github.com/MAX-API-Next/MAX-API)
- [レスポンスインターフェース (Responses)](https://github.com/MAX-API-Next/MAX-API)
- [イメージインターフェース (Image)](https://github.com/MAX-API-Next/MAX-API)
- [オーディオインターフェース (Audio)](https://github.com/MAX-API-Next/MAX-API)
- [ビデオインターフェース (Video)](https://github.com/MAX-API-Next/MAX-API)
- [エンベッドインターフェース (Embeddings)](https://github.com/MAX-API-Next/MAX-API)
- [再ランク付けインターフェース (Rerank)](https://github.com/MAX-API-Next/MAX-API)
- [リアルタイム対話インターフェース (Realtime)](https://github.com/MAX-API-Next/MAX-API)
- [Claudeチャット](https://github.com/MAX-API-Next/MAX-API)
- [Google Geminiチャット](https://github.com/MAX-API-Next/MAX-API)

</details>

---

## 🚢 デプロイ

> [!TIP]
> **最新のDockerイメージ:** `max-api:latest`

### 📋 デプロイ要件

| コンポーネント | 要件 |
|------|------|
| **ローカルデータベース** | SQLite（Dockerは `/data` ディレクトリをマウントする必要があります）|
| **リモートデータベース** | MySQL ≥ 5.7.8 または PostgreSQL ≥ 9.6 |
| **コンテナエンジン** | Docker / Docker Compose |

### ⚙️ 環境変数設定

<details>
<summary>一般的な環境変数設定</summary>

| 変数名 | 説明 | デフォルト値 |
|--------|------|--------|
| `SESSION_SECRET` | セッションシークレット（マルチマシンデプロイに必須） | - |
| `CRYPTO_SECRET` | 暗号化シークレット（Redisに必須） | - |
| `SQL_DSN** | データベース接続文字列 | - |
| `REDIS_CONN_STRING` | Redis接続文字列 | - |
| `STREAMING_TIMEOUT` | ストリーミング応答のタイムアウト時間（秒） | `300` |
| `STREAM_SCANNER_MAX_BUFFER_MB` | ストリームスキャナの1行あたりバッファ上限（MB）。4K画像など巨大なbase64 `data:` ペイロードを扱う場合は値を増加させてください | `64` |
| `MAX_REQUEST_BODY_MB` | リクエストボディ最大サイズ（MB、**解凍後**に計測。巨大リクエスト/zip bomb によるメモリ枯渇を防止）。超過時は `413` | `32` |
| `AZURE_DEFAULT_API_VERSION` | Azure APIバージョン | `2025-04-01-preview` |
| `ERROR_LOG_ENABLED` | エラーログスイッチ | `false` |
| `PYROSCOPE_URL` | Pyroscopeサーバーのアドレス | - |
| `PYROSCOPE_APP_NAME` | Pyroscopeアプリ名 | `max-api` |
| `PYROSCOPE_BASIC_AUTH_USER` | Pyroscope Basic Authユーザー | - |
| `PYROSCOPE_BASIC_AUTH_PASSWORD` | Pyroscope Basic Authパスワード | - |
| `PYROSCOPE_MUTEX_RATE` | Pyroscope mutexサンプリング率 | `5` |
| `PYROSCOPE_BLOCK_RATE` | Pyroscope blockサンプリング率 | `5` |
| `HOSTNAME` | Pyroscope用のホスト名タグ | `max-api` |

📖 **完全な設定:** [環境変数ドキュメント](https://github.com/MAX-API-Next/MAX-API)

</details>

### 🔧 デプロイ方法

<details>
<summary><strong>方法 1: Docker Compose（推奨）</strong></summary>

```bash
# プロジェクトをクローン
git clone https://github.com/MAX-API-Next/MAX-API.git
cd max-api

# 設定を編集
nano docker-compose.yml

# サービスを起動
docker-compose up -d
```

</details>

<details>
<summary><strong>方法 2: Dockerコマンド</strong></summary>

**SQLiteを使用:**
```bash
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

**MySQLを使用:**
```bash
docker run --name max-api -d --restart always \
  -p 3000:3000 \
  -e SQL_DSN="root:123456@tcp(localhost:3306)/oneapi" \
  -e TZ=Asia/Shanghai \
  -v ./data:/data \
  max-api:latest
```

> **💡 パス説明:**
> - `./data:/data` - 相対パス、データは現在のディレクトリのdataフォルダに保存されます
> - 絶対パスを使用することもできます：`/your/custom/path:/data`

</details>

<details>
<summary><strong>方法 3: 宝塔パネル</strong></summary>

1. 宝塔パネル（**9.2.0バージョン**以上）をインストールし、アプリケーションストアで**MAX-API**を検索してインストールします。

📖 [画像付きチュートリアル](./docs/BT.md)

</details>

### ⚠️ マルチマシンデプロイの注意事項

> [!WARNING]
> - **必ず設定する必要があります** `SESSION_SECRET` - そうしないとマルチマシンデプロイ時にログイン状態が不一致になります
> - **共有Redisは必ず設定する必要があります** `CRYPTO_SECRET` - そうしないとデータを復号化できません

### 🔄 チャネルリトライとキャッシュ

**リトライ設定:** `設定 → 運営設定 → 一般設定 → 失敗リトライ回数`

**キャッシュ設定:**
- `REDIS_CONN_STRING`：Redisキャッシュ（推奨）
- `MEMORY_CACHE_ENABLED`：メモリキャッシュ

---

## 🔗 関連プロジェクト

### 上流プロジェクト

| プロジェクト | 説明 |
|------|------|
| [One API](https://github.com/songquanpeng/one-api) | オリジナルプロジェクトベース |
| [Midjourney-Proxy](https://github.com/novicezk/midjourney-proxy) | Midjourneyインターフェースサポート |

### 補助ツール

| プロジェクト | 説明 |
|------|------|
| [max-api-key-tool](https://github.com/MAX-API-Next/MAX-API-key-tool) | キー使用量クォータ照会ツール |
| [max-api-horizon](https://github.com/MAX-API-Next/MAX-API-horizon) | MAX API高性能最適化版 |

---

## 💬 ヘルプサポート

### 📖 ドキュメントリソース

| リソース | リンク |
|------|------|
| 📘 よくある質問 | [FAQ](https://github.com/MAX-API-Next/MAX-API) |
| 💬 コミュニティ交流 | [交流チャネル](https://github.com/MAX-API-Next/MAX-API) |
| 🐛 問題のフィードバック | [問題フィードバック](https://github.com/MAX-API-Next/MAX-API) |
| 📚 完全なドキュメント | [公式ドキュメント](https://github.com/MAX-API-Next/MAX-API) |

### 🤝 貢献ガイド

あらゆる形の貢献を歓迎します！

- 🐛 バグを報告する
- 💡 新しい機能を提案する
- 📝 ドキュメントを改善する
- 🔧 コードを提出する

---

## 📜 ライセンス

このプロジェクトは [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE) の下でライセンスされています。

本プロジェクトは、[One API](https://github.com/songquanpeng/one-api)（MITライセンス）をベースに開発されたオープンソースプロジェクトです。

お客様の組織のポリシーがAGPLv3ライセンスのソフトウェアの使用を許可していない場合、またはAGPLv3のオープンソース義務を回避したい場合は、こちらまでお問い合わせください：[GitHub Issues](https://github.com/MAX-API-Next/MAX-API/issues)

---

## 🌟 スター履歴

<div align="center">

[![スター履歴チャート](https://api.star-history.com/svg?repos=MAX-API-Next/MAX-API&type=Date)](https://star-history.com/#MAX-API-Next/MAX-API&Date)

</div>

---

<div align="center">

### 💖 MAX APIをご利用いただきありがとうございます

このプロジェクトがあなたのお役に立てたなら、ぜひ ⭐️ スターをください！

**[公式ドキュメント](https://github.com/MAX-API-Next/MAX-API)** • **[問題フィードバック](https://github.com/MAX-API-Next/MAX-API/issues)** • **[最新リリース](https://github.com/MAX-API-Next/MAX-API/releases)**

<sub>❤️ で構築された MAX-API-Next</sub>

</div>
