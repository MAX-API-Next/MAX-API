# Task Plugin 宿主基础 / Host foundation

`pkg/taskplugin` 提供内部宿主基础，尚未注册公开管理或转发路由。`PackageDocument` 是 MAX-API 的数据包封装，不是 New API JavaScript 插件的直接安装格式；`source` 保持不透明，加载不会执行代码。本阶段不包含 JS VM、插件市场、管理界面或真实插件渠道接入。

`pkg/taskplugin` provides an internal host foundation. It does not register public management or relay routes. `PackageDocument` is a MAX-API data envelope, not a drop-in JavaScript plugin installation format. Source remains opaque and is never evaluated during loading. A JS VM, marketplace, management UI and live plugin channels are outside this stage.

## 加载和任务身份 / Loading and task identity

- `LoadPackage` 最多读取 1 MiB，校验整个文档的 SHA-256。期望摘要必须由宿主从可信渠道提供，不能仅根据上传文件自行计算后当作来源认证。
- `Host.Install` 明确激活所选版本并批量发布插件和路由，包括操作员主动选择的版本回退；任一验证或路由冲突失败时整批不生效。同一 key/version 不允许替换不同内容。
- `Host.Retain` 仅恢复历史包，不改变活动版本和路由；可以一次保留同一 key 的多个版本。补载历史任务所需的包时使用此接口，不使用 `Install`。摘要冲突、撤销和总容量限制仍适用；恢复流量前仍须恢复撤销记录。
- 路由只匹配 manifest 声明的上游模型；省略 `route.Models` 表示继承 manifest 的范围，指定时进一步收窄。缺失或未知模型拒绝绑定。
- `BindMapped` 固定插件版本、包摘要、原始及映射后模型，以及宿主拥有的 usage contract；`Resolve` 使用历史包恢复任务，不自动切换最新版本。
- `AttachTaskPluginBinding` 校验用户、Token、渠道、请求、模型及冻结用量契约，写入 `Task.PrivateData`。调用方必须在发起付费请求前通过现有事务保存任务；该函数自身不执行数据库写入或上游提交。
- 重启时必须重新加载所需历史包，并在开放流量前恢复宿主的撤销记录。缺失历史包、无效契约、身份变化或已撤销版本均拒绝恢复，不自动重提任务。当前没有自动保存插件目录或撤销清单的管理服务。

The loader accepts at most 1 MiB and verifies an externally trusted SHA-256 of the whole document. `Host.Install` explicitly activates the chosen versions, including operator-selected rollbacks, and publishes packages and routes together; a conflict leaves the previous generation intact. `Host.Retain` restores history without changing active versions or routes, and accepts several versions of one key in a batch. Use `Retain` rather than `Install` when loading historical task packages. Digest conflicts, revocations and the total retention limit still apply; revocations must be restored before traffic resumes. A key/version cannot silently change contents. Routes match only manifest-declared upstream models; an omitted `route.Models` inherits that set, while an explicit list narrows it. Missing or unknown models cannot bind. Bindings freeze package, usage contract, client model alias and upstream model. Recovery resolves historical packages rather than the latest version.

The service validates task ownership and the frozen usage contract before attaching private metadata. The caller must persist it in the existing transaction before any billable request. The host must reload retained historical packages and revocations before serving traffic after restart. Missing or revoked identities fail closed and never cause automatic submission. Automatic catalog/revocation storage is not implemented here.

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
```

这些合成测试不代表真实供应商、JS 隔离、多节点撤销传播或 MySQL/PostgreSQL 实库验收已通过。

Synthetic tests do not establish live-provider, JS isolation, multi-node revocation propagation or MySQL/PostgreSQL acceptance.
