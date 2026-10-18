# Task Plugin 宿主基础 / Host foundation

`pkg/taskplugin` 提供内部宿主基础，尚未注册公开管理或转发路由。`PackageDocument` 是 MAX-API 的数据包封装，不是 New API JavaScript 插件的直接安装格式；`source` 保持不透明，加载不会执行代码。本阶段不包含 JS VM、插件市场、管理界面或真实插件渠道接入。

`pkg/taskplugin` provides an internal host foundation. It does not register public management or relay routes. `PackageDocument` is a MAX-API data envelope, not a drop-in JavaScript plugin installation format. Source remains opaque and is never evaluated during loading. A JS VM, marketplace, management UI and live plugin channels are outside this stage.

## 加载和任务身份 / Loading and task identity

- `LoadPackage` 最多读取 1 MiB，校验整个文档的 SHA-256。期望摘要必须由宿主从可信渠道提供，不能仅根据上传文件自行计算后当作来源认证。
- `Host.Install` 批量发布插件和路由；任一验证或路由冲突失败时整批不生效。同一 key/version 不允许替换不同内容。
- 路由只匹配 manifest 声明的上游模型；省略 `route.Models` 表示继承 manifest 的范围，指定时进一步收窄。缺失或未知模型拒绝绑定。
- `BindMapped` 固定插件版本、包摘要、原始及映射后模型，以及宿主拥有的 usage contract；`Resolve` 使用历史包恢复任务，不自动切换最新版本。
- `AttachTaskPluginBinding` 校验用户、Token、渠道、请求、模型及冻结用量契约，写入 `Task.PrivateData`。调用方必须在发起付费请求前通过现有事务保存任务；该函数自身不执行数据库写入或上游提交。
- 持久目录在开放流量前恢复历史包和撤销记录。缺失历史包、摘要不匹配、无效契约、身份变化或已撤销版本均拒绝恢复，不自动重提任务。

The loader accepts at most 1 MiB and verifies an externally trusted SHA-256 of the whole document. Batch installation publishes packages and routes together; a conflict leaves the previous generation intact. A key/version cannot silently change contents. Routes match only manifest-declared upstream models; an omitted `route.Models` inherits that set, while an explicit list narrows it. Missing or unknown models cannot bind. Bindings freeze package, usage contract, client model alias and upstream model. Recovery resolves historical packages rather than the latest version.

The service validates task ownership and the frozen usage contract before attaching private metadata. The caller must persist it in the existing transaction before any billable request. The durable catalog restores historical packages and revocations before startup serves traffic. Missing, altered or revoked identities fail closed and never cause automatic submission.

## 持久目录与恢复 / Durable catalog and recovery

`taskplugin.OpenCatalog(ctx, model.NewTaskPluginCatalogStore(db))` 加载内部目录，`catalog.Host()` 提供既有绑定与恢复能力。应用启动由 `InitializeTaskPluginCatalog` 完成恢复；缺表、损坏包或无效状态会阻止启动。目录仍没有公开管理路由或自动加载 JS 的行为。

- `Install` 批量校验并保存完整包，原始文档字节和摘要保持不变；同 key/version 不允许替换内容。安装版本成为该 key 的启用版本，历史版本保留。
- `SetEnabled(false)` 停止新任务绑定，旧任务仍能按历史版本恢复。`Activate` 显式切换已有版本，不自动选择“最新”版本。
- `Revoke` 永久撤销指定版本，新绑定和现有 session 的下一次使用均被拒绝；接口不提供撤销回滚或历史版本删除。
- `task_plugin_catalog_heads` 的版本号条件更新与 `task_plugin_versions` 的变更在同一事务内提交。事务成功后一次发布宿主状态；存储失败、结果未知或刷新失败会暂停宿主，成功 `Refresh` 后恢复。
- 包文档在 MySQL 使用 LONGTEXT，在 SQLite/PostgreSQL 使用 TEXT；版本身份使用固定长度摘要索引。升级会新增这两张表，备份与恢复应包含两表的一致状态。
- 其他节点的变更通过显式 `Refresh` 读取。本阶段没有后台同步任务，不保证跨节点实时撤销；后续执行器接入时必须定义刷新时机。目录容量沿用 64 MiB 历史包原始字节上限。

`OpenCatalog` restores the internal catalog through a GORM store. Startup restores it before serving traffic and stops on missing tables, corrupt bytes or invalid state. Batch installation preserves exact package bytes and immutable key/version identities. Disabling blocks new admission but preserves historical task recovery; activation explicitly selects a retained version. Revocation is permanent and invalidates existing sessions on their next use. Historical versions are never automatically deleted or replaced.

A singleton revision CAS serializes metadata transactions across nodes, including concurrent bootstrap. Publication follows commit; uncertain writes or failed refreshes suspend the managed host until a successful refresh. MySQL stores documents as LONGTEXT; SQLite and PostgreSQL use TEXT. Migrations add `task_plugin_catalog_heads` and `task_plugin_versions`; back up and restore both consistently. Other nodes' changes require explicit `Refresh`; background synchronization and immediate multi-node revocation are outside this stage. Retained document bytes remain capped at 64 MiB. The catalog registers no public routes and never evaluates JavaScript.

## HTTP 能力 / HTTP capability

`Session.NewFetchScope` 只提供 GET：HTTPS、主渠道 origin 与 manifest 额外 host 白名单、禁止环境代理与重定向、每次解析全部 IP 后直连已检查的 IP。回环、私有、链路本地及保留网段均拒绝。渠道 Authorization 只发往精确的渠道 origin，额外 host 不携带该凭据。插件不能提供任意请求头、请求体或 Cookie。

每个 hook 创建并关闭独立 scope：最多 4 次调用、每次最多 15 秒、scope 总期限 30 秒、响应体最多 1 MiB，响应头最多 64 KiB。网络错误不会包含原始 URL、查询参数或认证信息。此能力不支持内网/明文上游，也不用于提交付费任务；既有 Go adaptor 行为不变。

Each hook gets a separate, closed-after-use GET capability: HTTPS only, exact channel origin plus declared additional hosts, no environment proxy, redirects, arbitrary headers, body or cookies. All DNS answers are checked before dialing a validated literal IP. Private, loopback, link-local and reserved addresses are rejected. Channel authorization is attached only to the exact channel origin.

Limits are four calls, 15 seconds per call, a 30-second scope deadline, 1 MiB response bodies and 64 KiB response headers. Network errors omit URLs, query strings and credentials. Private/plaintext upstreams and billable submission are not supported by this read-only capability. Existing Go adaptors are unchanged.

## 计费与验证 / Billing and validation

`ApplyTaskPluginUsage` 只附加经宿主契约验证的用量事实。显式零与缺失用量保持不同；不增加价格、额度扣减、退款或任务终态写入。结算继续使用冻结的计费计划、`task:<id>:finalize`、资金事务和独立日志/receipt effects。

`ApplyTaskPluginUsage` attaches validated facts only and preserves explicit zero versus missing usage. It does not price, debit, refund or publish task terminal state. The existing frozen plan, operation key, funding transaction and separate effects remain authoritative.

```powershell
go test ./pkg/taskplugin ./pkg/taskusage -count=1
go test ./service -run TestTaskPluginHost -count=1
go test ./model -run TestTaskPluginHost -count=1
go test ./pkg/taskplugin ./model ./service -run 'TestCatalog|TestTaskPluginCatalog' -count=1
```

这些合成测试不代表真实供应商、JS 隔离、多节点撤销传播或 MySQL/PostgreSQL 实库验收已通过。

Synthetic tests do not establish live-provider, JS isolation, multi-node revocation propagation or MySQL/PostgreSQL acceptance.
