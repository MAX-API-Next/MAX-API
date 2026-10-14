# 独立 SQLite 日志文件 / Separate SQLite log database

`LOG_SQLITE_PATH` 为可选的 SQLite 日志文件路径。只设置 `SQLITE_PATH` 时，日志与业务数据继续使用同一个数据库。配置 `LOG_SQLITE_PATH` 后，日志及结算日志 receipt 使用独立 SQLite 文件，业务资金和结算操作仍保存在主库。

`LOG_SQLITE_PATH` optionally selects a separate SQLite file for logs and billing log receipts. Without it, the existing shared-database behavior is preserved. Funding and durable settlement operations remain in the primary database.

```dotenv
SQLITE_PATH=/data/ledger.sqlite
LOG_SQLITE_PATH=/data/logs.sqlite
```

- `LOG_SQL_DSN` 留空或设为 `local` 时，可使用 `LOG_SQLITE_PATH`。旧配置 `LOG_SQL_DSN=local` 且未配置新路径时，继续使用 `SQLITE_PATH`。
- `LOG_SQL_DSN` 为 MySQL/PostgreSQL 连接串时，使用该连接串并保持现有行为；同时设置 `LOG_SQLITE_PATH` 会返回配置错误。二选一配置日志目标。
- 提前创建文件的父目录，并为运行进程提供读写权限。显式日志库打开或迁移失败会阻止启动，修复路径、权限或数据库后再启动。
- 路径支持 SQLite 驱动的文件连接参数，建议使用持久文件；两个 SQLite 文件应放在本地可靠存储上。本配置不代替多主机或外部数据库验收。

Leave `LOG_SQL_DSN` empty or set it to `local` when using `LOG_SQLITE_PATH`. Existing `LOG_SQL_DSN=local` deployments without the new setting keep using `SQLITE_PATH`. A remote MySQL/PostgreSQL log DSN and `LOG_SQLITE_PATH` together are rejected. Create the parent directory and grant the service access before startup. Opening or migrating the configured log database must succeed before startup can complete.

## 切换既有部署 / Changing an existing deployment

1. 暂停新请求及相关后台写入，在原配置下完成待恢复的资金与日志副作用，核对结算状态。
2. 备份业务库、日志库和配置。日志与 `billing_log_receipts` 属于同一恢复历史，应一起保留；单独复制日志或创建空日志库不能证明既有记录已经迁移。
3. 使用经过验证的离线迁移过程准备目标文件，再更新配置。此设置只选择连接目标，不自动搬迁、删除或合并旧数据；新文件初始为空时，界面读取的是新文件中的日志。
4. 启动后核对历史日志、receipt、待恢复结算、余额与计数，并保留原文件用于审计和恢复。切回旧配置前，应先核对期间新增的数据与待恢复操作。

Quiesce requests and background writers, finish pending settlement effects with the original database, and back up all databases and configuration. Preserve logs and `billing_log_receipts` together. Prepare the target file with a verified offline migration before changing configuration. This setting selects a database; it does not copy, delete, or merge existing data. Verify history, receipts, settlement state, balances, and counters after startup, and reconcile new writes before switching back.
