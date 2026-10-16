# SQLite 升级验收 / SQLite upgrade acceptance

备份、清单校验和隔离恢复命令见[SQLite 运维备份与恢复](sqlite-operations.md)。

See [SQLite operations backup and restore](sqlite-operations.md) for the archive, manifest verification, and isolated restore commands.

升级前停止接收新请求，在原数据库完成待处理资金结算及日志副作用，停止服务后一起备份业务库、日志库和配置。SQLite 使用 WAL 时，保留尚未合并的 WAL 文件；运行中的单个数据库文件复制不能作为完整备份。

Before upgrading, stop new traffic, finish pending funding settlements and log effects against their original databases, stop the service, and back up the business database, log database, and configuration together. Preserve uncheckpointed WAL files. Copying only a database file while it is in use does not establish a complete backup.

先对备份副本运行升级版本，核对钱包、Token、订阅用量与周期、历史日志、`billing_settlements` 和 `billing_log_receipts`，再重复启动并确认恢复没有再次扣费、退款或生成重复日志。待人工处理、未知结果或跨周期结算保留其核对状态，不能视为升级后自动成功。

Run the upgraded version against an isolated backup copy first. Check wallet and token balances, subscription usage and period, historical logs, `billing_settlements`, and `billing_log_receipts`. Restart again and confirm recovery does not debit, refund, or project a log twice. Manual, unknown-outcome, and cross-period records retain their reconciliation boundaries; upgrading does not imply their successful settlement.

## 日志文件 / Log files

默认同库和旧 `LOG_SQL_DSN=local` 可以继续使用。新增 `LOG_SQLITE_PATH` 时按[独立日志数据库指引](sqlite-log-database.md)准备目标文件；这个参数不复制或合并旧日志。完成原库的 pending effects，并保持日志和 receipt 一起保留，是切换前的必要准备。新空日志文件的启动验证只适用于已完成这些准备的场景。

Existing shared-database and `LOG_SQL_DSN=local` settings remain supported. Follow the [separate log database guide](sqlite-log-database.md) when configuring `LOG_SQLITE_PATH`. The setting does not copy or merge old logs. Finish effects against the original file and retain each log with its receipt before switching. Fresh-file startup acceptance assumes these preparations are complete.

## 自动化覆盖 / Automated coverage

`model/sqlite_upgrade_test.go` 使用 MAX-API `v2.0.0-smartops.pre3` 固定提交 `48260ce6517a9ab68851a7ac499477981d78da63` 的真实 `InitDB/InitLogDB` 生成 schema fixture（39 张表、160 个索引），并填入合成数据。当前版本在独立测试进程中执行主节点迁移及恢复，覆盖共享、旧 local、已有独立目标与完成结算后的新空目标，多次启动、64 位余额、明确零费用、原资金来源与周期、旧 receipt 去重及目标文件的日志标记回填。LF 和 Windows CRLF 格式均验证。fixture 只有 schema，不包含用户数据、密钥或运行时二进制。

The fixture comes from real `InitDB/InitLogDB` execution at the pinned MAX-API pre3 source revision: 39 tables and 160 indexes, with synthetic data added by the tests. Fresh test processes run current master migrations and recovery for shared, legacy-local, existing separate, and quiesced fresh log targets. Coverage includes repeated startup, 64-bit balances, explicit zero usage, original funding and periods, old receipt deduplication, and target-specific retry-marker backfill. Both LF and Windows CRLF inputs are checked. The committed fixture contains schema only, with no user data, credentials, or runtime binary.

这个基线不等于验证了所有历史部署。更老版本、真实旧镜像与用户库、MySQL/PostgreSQL、混合引擎、Redis 多节点、在线切库、实盘掉电、保留清理策略及实际渠道仍需对应环境验收。升级后写入的数据不保证可直接降级；需要回退时使用成套升级前备份，并核对外部已接受请求和未完成结算。

This baseline does not establish compatibility with every historical deployment. Older releases, actual old images and user databases, MySQL/PostgreSQL, mixed engines, Redis clusters, online switching, power interruption, retention cleanup, and real channels require their own acceptance. New-version writes are not guaranteed to support direct downgrade. Restore the complete pre-upgrade backup for rollback and reconcile externally accepted requests and incomplete settlements.
