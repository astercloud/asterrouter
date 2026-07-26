# 运营、可观测性与 SRE

## 1. 运营目标

运维系统必须回答四个问题：现在是否可用、谁受到影响、为什么发生、采取动作后是否恢复。技术健康与业务健康同时成立才算 Ready。

## 2. 信号标准

| 信号 | 必备维度 | 用途 |
| --- | --- | --- |
| Metrics | role、profile、tenant_class、model、provider、route、status | SLO、容量、趋势和告警 |
| Logs | timestamp、level、service、request_id/job_id、reason_code | 事件调查，不含敏感正文 |
| Traces | Gateway 阶段、DB/Redis、Adapter、Sink | 延迟分解和依赖定位 |
| Events | 状态变化、配置发布、插件、License、安全 | 审计与自动化响应 |
| Profiles | CPU、Heap、Goroutine、锁 | 性能与资源诊断，受控开启 |

Tenant ID 和 Credential 只使用内部 ID/指纹，不把用户邮箱、Key、Prompt 或对象 URL 放入高基数字段。

## 3. 黄金指标

### 3.1 Gateway

- 请求率、成功率、客户端错误率、平台错误率。
- P50/P95/P99 总延迟、首 Token/首事件延迟。
- Route 候选数、无候选、重试次数、重试放大率。
- 流式提交后错误、客户端断开、Realtime 活跃 Session。

### 3.2 Provider Supply

- Account 健康、熔断状态、冷却数、过期/余额风险。
- RPM/TPM/并发/加权容量使用率与拒绝率。
- Provider 成功率、延迟、错误分类和模型能力漂移。
- Effective Price 陈旧度、价格置信度和成本异常。

### 3.3 Durable 与 Artifact

- Admission 接受/拒绝、Queue Depth、最老等待时间、公平性偏差。
- Dispatch/Reconcile 延迟、Lease 失效、Unknown Dispatch、DLQ。
- Job 各状态数量、取消延迟、Callback 重放与乱序。
- Artifact 摄取/交付/删除成功率、字节量、陈旧与孤儿对象。

### 3.4 Metering

- Usage 缺失、估算比例、结算延迟、重复冲突。
- Hold 未释放、Ledger 对账差异、Pricing 未匹配/错误。
- Usage Sink 投递延迟、重试和 Dead Letter。
- 客户费用、Provider 成本、毛利和节省证据覆盖率。

### 3.5 Plugin 与 AsterCloud

- 安装/升级/回滚结果、Sidecar 启动时间、重启次数和调用错误。
- Catalog/License/Feed 最后成功时间、过期倒计时、签名失败。
- AsterCloud API/Worker Queue、签名操作、包下载、审核时长和激活成功率。

## 4. SLI 与初始 SLO 候选

以下为建立生产基线后的目标候选，不在无数据时直接作为合同承诺：

| 服务 | SLI | 初始候选 |
| --- | --- | --- |
| Gateway Admission | 有效请求获得明确响应比例 | 99.95% / 月 |
| Direct Gateway | 排除 Provider 合同外故障后的平台成功率 | 99.9% / 月 |
| Management API | 成功管理请求比例 | 99.9% / 月 |
| Durable Admission | 成功持久化并返回 Job 的比例 | 99.95% / 月 |
| Usage Settlement | 终态后目标时间内完成结算比例 | 99.99% / 15 分钟 |
| Catalog/License | 可获取有效本地 Snapshot | 99.9%，且支持离线窗口 |
| Artifact Delivery | Ready 产物在策略时间内可访问/投递 | 按 Store 与 Sink 分级 |

错误预算消耗超过阈值时停止非必要功能发布，优先修复可靠性。

## 5. 健康检查

| Endpoint | 含义 | 检查范围 |
| --- | --- | --- |
| `/health` 或 `/health/live` | 进程存活 | 事件循环/进程，不访问慢依赖 |
| `/ready` 或 `/health/ready` | 可接收目标角色流量 | DB、必要配置、迁移、Role 关键依赖 |
| `/version` | 构建身份 | version、commit、build time、schema/protocol |

插件、单个 Provider、AsterCloud 或可选 Artifact Store 故障不应一律把整个 API 标成 Not Ready。Readiness 按 Role 和启用能力计算，并在详细状态中展示降级范围。

## 6. 告警分级

| 等级 | 定义 | 示例 | 响应 |
| --- | --- | --- | --- |
| SEV-1 | 大范围不可用、数据/安全风险 | 跨租户、重复扣费、数据库不可写 | 立即响应、冻结危险变更 |
| SEV-2 | 核心能力显著降级 | 主 Provider 池故障、队列持续积压 | 值班响应、执行 Runbook |
| SEV-3 | 局部或有替代路径 | 单插件崩溃、单 Sink DLQ | 工作时段处置 |
| SEV-4 | 趋势与维护项 | License 将过期、磁盘增长 | 计划处理 |

告警必须包含影响范围、当前值/阈值、起始时间、关键 Dashboard 和 Runbook，不发送只有“失败了”的消息。

## 7. 标准 Runbook

### 7.1 Provider 错误率升高

1. 按 Account、Model、Region、错误类确认范围。
2. 检查凭据、余额、有效期、熔断和最近配置。
3. 通过 Simulator 验证替代候选和策略影响。
4. 降权/冷却故障账号，保留审计和过期时间。
5. 验证成功率、延迟、成本和重试放大恢复。

### 7.2 Durable 队列积压

1. 看最老 Job、租户公平性、Provider Permit 和 Worker 心跳。
2. 区分供给容量不足、Worker 不足、DB/Redis 故障或毒性 Job。
3. 隔离反复失败项，扩 Worker 但不突破 Provider 容量。
4. 验证 Lease、重复 Dispatch 和 Settlement 指标。

### 7.3 Sidecar 崩溃

1. 查看插件版本、Supervisor 状态、退出码和权限变更。
2. 停止自动重启风暴，隔离插件 Route/Contribution。
3. 回滚到上一签名版本或禁用插件。
4. 验证 Core Gateway 与账务未受影响。

### 7.4 Usage/账务差异

1. 以 Operation/Attempt 定位 Usage Version 与价格快照。
2. 检查重复事件、Provider 后补 Usage、舍入和规则版本。
3. 停止错误规则的新流量，发布新版本。
4. 用冲正/替代分录纠正，不改历史记录。

## 8. 变更管理

高风险配置采用变更集：Draft -> Validate -> Simulate -> Approve -> Publish -> Observe -> Complete/Rollback。

高风险项包括 Route 权重、Provider Secret、价格、全局策略、插件权限、License Root Key、数据保留和 Schema。发布记录包含影响对象、前后摘要、操作者、审批、版本、回滚点和观测窗口。

## 9. 容量规划

- API 以 RPS、并发连接、流式时长和 Payload 大小建模。
- Worker 以归一 Job Work Unit、Provider Permit 和 Artifact 字节建模。
- PostgreSQL 关注事务率、活跃连接、表/索引增长、WAL 和慢查询。
- Redis 关注内存、Key TTL、Lua 延迟、Stream/Queue 和故障转移。
- Artifact Store 关注吞吐、请求率、生命周期、跨区费用和失败重试。
- Sidecar 关注每插件 CPU/内存、并发、启动与崩溃预算。

扩容指标必须是可执行工作量，而不是原始 Queue Depth。没有 Provider 容量时增加 Worker 只会放大竞争。

## 10. 备份与恢复

备份至少包含 PostgreSQL、Plugin Package/Active 资产索引、必要配置与 Secret 引用、Local Artifact（若使用）。S3 Artifact 使用版本、生命周期和清单机制，不默认打入数据库备份。

每次恢复演练验证：

- 备份校验和与解密材料可用；
- 新环境恢复后 Instance/License 语义明确；
- Job、Outbox、Artifact 和插件状态完成 Reconcile；
- Provider Secret 可解密但不在输出中暴露；
- 抽样请求、Usage 和账务可追溯。

## 11. 诊断包

诊断包默认只包含版本、Profile、Role、配置存在性、健康摘要、脱敏指标、错误码和有限日志。不得包含数据库连接串、Secret、Key Hash、Prompt/Response、对象签名 URL 或用户 PII。生成与下载均审计并设置自动过期。

## 12. 运营验收

- 任一 P0 告警在预生产环境演练并链接可执行 Runbook。
- Dashboard 能从平台总览下钻到 Profile、Tenant、Model、Provider、Job 和 Operation。
- 对 Redis、DB、Object Store、AsterCloud、Provider、Sidecar 分别执行故障注入。
- 从告警到确认恢复的每一步都有机器可验证信号。
- 恢复演练达到 [多实例部署与容灾设计](./19-多实例部署与容灾设计.md) 的 RPO/RTO 门槛。
