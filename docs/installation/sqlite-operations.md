# SQLite 运维备份与恢复 / SQLite Operations Backup and Restore

`tools/sqliteops` 只处理**已停写**的 SQLite 文件副本。它不会打开数据库、执行迁移、复制配置内容或修改业务数据。

`tools/sqliteops` operates only on a **quiesced** SQLite file set. It does not open a database, run migrations, copy configuration contents, or modify business data.

## 成套备份 / Complete archive

先停止 API 进程、后台写入任务和结算恢复任务，确认没有新的请求或日志写入，然后执行：

```powershell
go run ./tools/sqliteops backup `
  --quiesced `
  --primary C:\data\ledger.sqlite `
  --log C:\data\logs.sqlite `
  --config C:\data\runtime.env `
  --destination C:\archives\max-api-20261016
```

`--log` 省略或与 `--primary` 相同时，主库同时作为日志库归档。SQLite 的 `-wal` 和 `-shm` 文件存在时会随对应数据库一起归档。配置文件只记录文件名、大小和 SHA-256，**不会复制配置内容或密钥**。

The `--log` argument may be omitted, or may point to the primary file when the application uses a shared database. Existing SQLite `-wal` and `-shm` sidecars are archived with their database. Configuration is fingerprinted by name, size, and SHA-256 only; its contents and secrets are never copied.

## 验证与隔离恢复 / Verify and isolated restore

归档不得覆盖已有目录。验证归档清单和所有文件哈希：

```powershell
go run ./tools/sqliteops verify --backup C:\archives\max-api-20261016
```

恢复到全新的空目录：

```powershell
go run ./tools/sqliteops restore `
  --backup C:\archives\max-api-20261016 `
  --target C:\rehearsals\max-api-20261016
```

验证会拒绝缺失、篡改、路径逃逸和未列入清单的额外文件。恢复只复制已验证的数据库文件和 WAL sidecar；配置仍需人工按安全渠道准备。恢复副本必须先使用当前版本执行初始化、重启和业务快照核对，不能直接接入生产流量。

Verification rejects missing, modified, path-escaping, and unlisted files. Restore copies only verified database files and WAL sidecars; configuration must be supplied separately through a secure channel. Run initialization, restart, and business-snapshot checks against the restored copy before any traffic is admitted.

## 业务快照验收 / Business snapshot rehearsal

仓库中的合成验收会使用当前模型创建主库和独立日志库，执行归档与两次隔离恢复，然后重新打开恢复文件，比较用户额度、Token、渠道用量、订阅周期与预消费、settlement、日志和 receipt；主库还会在保持连接期间写入已提交但未检查点的真实 WAL 标记，并在恢复后核对。它只验证文件归档及当前 SQLite 模型数据的可读性，不代表真实生产库、跨库原子性或外部支付状态已经验证：

```powershell
go test ./model -run TestSQLiteOperationsAcceptancePreservesBusinessSnapshot -count=1
```

The repository rehearsal creates a synthetic primary database and independent log database from the current models, archives them, restores them twice into isolated directories, reopens both copies, and compares wallet quota, token and channel usage, subscription/pre-consume state, settlement, logs, and receipts. It also keeps the primary connection open while a committed, uncheckpointed WAL marker is archived and verifies that marker after restore. It validates the archive and current SQLite model readability only; it does not establish production-database, cross-database atomicity, or external-payment correctness.

## 边界 / Boundaries

- `--quiesced` 是强制门禁；运行中复制单个 SQLite 文件不能证明恢复点一致。
- 归档不代表主库、日志库和外部支付状态已经形成跨系统原子快照。
- manual、unknown outcome、跨订阅周期和外部已接受但本地未完成的结算必须单独对账。
- 新版本写入不保证旧版本可以直接降级；回退使用完整升级前归档，并核对期间新增写入。
- 本工具不验证 MySQL/PostgreSQL、Redis、多主机、真实渠道、OAuth、掉电恢复或浏览器流程。
- 合成业务快照测试不能替代真实进程启动、真实计费/渠道调用或 MySQL/PostgreSQL 兼容性测试。

- `--quiesced` is mandatory; copying one SQLite file while it is live does not establish a consistent restore point.
- An archive is not an atomic snapshot across the business database, log database, and external payment state.
- Manual, unknown-outcome, cross-period, and externally accepted but locally incomplete settlements require separate reconciliation.
- New-version writes are not guaranteed to support direct downgrade; rollback uses the complete pre-upgrade archive and reconciles writes made during the rehearsal.
- This tool does not validate MySQL/PostgreSQL, Redis, multi-host operation, real providers, OAuth, power loss, or browser flows.
- The synthetic business-snapshot test does not replace live-process startup, real billing/provider calls, or MySQL/PostgreSQL compatibility testing.
