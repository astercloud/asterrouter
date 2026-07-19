# 测试与发布门禁

## 1. 质量定位

错误计费、重复扣费、漏扣、规则版本漂移、币种混用和不可重放均为 P0。测试遵循仓库 [全项目测试计划](../../test/v1/README.md) 的风险分层，不能用“表达式能执行”代替账务验收。

验证顺序：

```text
纯数学/编译器单元
  -> Rule Service 与 Facts Builder
  -> Memory Repository 契约
  -> PostgreSQL 事务/约束/重启
  -> Gateway + fake upstream
  -> Operator Outbox/余额
  -> Admin/Platform/Operator UI
  -> Clean install/静态删除门禁/发布包
```

## 2. P0 不变量

每项必须有成功和失败路径：

1. Published Version 永不修改或删除。
2. 预估和最终结算使用同一个 Rule Version ID。
3. 相同 Version + Facts 的结果逐位一致。
4. 同一 `(operation, attempt, usage_version, purpose)` 最多一条 Ledger。
5. Ledger 金额、币种、Evaluation 和 Usage 不一致时拒绝重放。
6. Customer Charge 只计算一次，Operator 不重新选择或执行规则。
7. `nil/unpriced` 与 `0/free` 可区分并正确聚合。
8. 非 USD 不能进入 V1 预算、Ledger 或钱包。
9. Hash mismatch、未知 Engine Version 和禁用 AST 必须失效关闭。
10. Provider 已消费后的 Pricing 失败不能丢 Usage，也不能伪造金额。
11. Outbox 重复投递不重复扣余额，失败可恢复。
12. Facts、Evaluation、Audit、错误消息不含 Prompt、Secret、Header 或 Body。
13. 计价 Observer 删除后，RPM、Token、Spend 和 Error Rate 风险规则仍由持久 Usage Event 触发。

## 3. 纯数学测试

### 3.1 MoneyMicros

表驱动覆盖：

- 0、1、9,999、10,000、1,000,000 micros；
- API 十进制美元字符串与 micros 的精确双向转换；
- 超过 6 位小数、科学计数法、NaN 和 Infinity 拒绝；
- `math.MaxInt64` 附近的加法、乘法和累加；
- 负数拒绝；
- 不同币种相加拒绝。

### 3.2 Pricing Helper

`token_cost`：

- 0 Token、0 价格、1 Token；
- `$3/1M * 1000 tokens = 3000 micros`；
- 半单位舍入边界 ±1；
- quantity/rate 溢出；
- 负 quantity/rate。

`unit_cost`、`block_cost`、`multiply_bps`：

- exact block、余数、半单位；
- `10_000 bps` 恒等；
- 0、100%、200% 和安全上限；
- 除数 0；
- 溢出和负数。

性质测试：

- 对非负输入结果非负；
- 对固定非负价格，quantity 增加时单个 Helper 结果不下降；
- micros 与十进制美元字符串往返保持原值；
- 相同整数输入没有平台差异。

## 4. 编译器与 DSL 测试

### 4.1 版本与 Hash

- `v1:` 正常编译；
- 无前缀拒绝发布；
- `v0/v2/超大版本号` 返回 `pricing_version_unsupported`；
- Source 修改一个字节导致 Hash 改变；
- 错误的预计算 Hash 不进入缓存；
- 不能用表达式 A 和 Hash B 污染 B 的缓存 Entry；
- Unicode/换行的 Hash 在重启后稳定。

### 4.2 AST 白名单

允许：

- int/bool/string；
- 比较、逻辑、三元、加法；
- 登记 Fact 和 Helper。

拒绝：

- 浮点字面量；
- 原始乘除模；
- 未登记 Identifier/Function；
- 数组、Map、成员访问和动态索引；
- `header/param/time/random`；
- 表达式超过 8 KiB；
- 节点数、深度、Tier 数、Line 数超限；
- 超长 Line/Tier 字符串；
- 多个命中 Tier、重复 Line Code、Line Helper 的 BPS 数学不一致、纯 cost 绕过 Line、Line 合计不等于结果。

### 4.3 Golden Rules

- 输入/输出平价；
- Claude 200,000 边界、199,999、200,001；
- 缓存读、5m 写、1h 写组合；
- `cache_fields_present=false` 的显式处理；
- 图片按张；
- 音视频按毫秒块；
- 每请求固定费；
- `service_tier` 白名单属性；
- 显式免费规则；
- 显式最低消费规则；
- 显式 BPS 调整规则及调整前后计费行。

### 4.4 Fuzz

Fuzz 入口：

- Version Parser；
- AST Validator；
- Canonical Facts JSON；
- Expression Hash Verification；
- Pricing Helper；
- Line/Tier Trace Collector。

不变量：不 panic、不无限执行、不返回未分类错误、不产生负金额、不绕过 AST 白名单。

## 5. 缓存与并发测试

- 相同 Hash 64/256 并发编译只产生一个共享 Program。
- 不同 Hash 并发编译结果隔离。
- LRU 达到上限逐项淘汰，不整体清空。
- 淘汰后重新编译结果相同。
- 并发 Execute 使用独立 Trace，不串 Tier/Line。
- UsedFacts/Analysis 返回结构不能修改缓存内部状态。
- Hash mismatch 不读取已有同键 Program。
- `go test -race ./internal/pricing -count=1` 必须通过。

## 6. Facts Builder 测试

### 6.1 Token 归一化

覆盖 OpenAI-compatible、Anthropic、Gemini 已实现协议：

- 总输入、非缓存输入、缓存读写和输出事实；
- 字段缺失与显式 0 的区别；
- Anthropic 5m/1h 缓存写入；
- 子项合计大于总输入时拒绝或标记 normalization failure；
- 负值和整数溢出；
- streaming 最终 Usage；
- Provider 不返回 Usage；
- estimated -> reported/reconciled 的 Usage Version 演进。

### 6.2 UsageDimensions

对每个登记维度覆盖：

- 正常 quantity/unit；
- 缺失为“未提供”还是 0 的契约；
- 错误单位；
- 负值、超大值、未知维度；
- estimate reservation 与 final observed quantity；
- Merge/Normalize 后 FactsHash 稳定。

### 6.3 隐私

用 synthetic marker 填充：

- Prompt；
- Authorization/API Key；
- 任意 Header；
- Payload 私有字段；
- Source IP；
- Sticky Key。

断言 marker 不出现在：

- PricingFacts JSON；
- PricingEvaluation；
- Audit Summary；
- Outbox Payload；
- API Error；
- Gateway Trace Pricing Evidence。

## 7. Rule Service 测试

### 7.1 选择优先级

`usage_cost`：

- global exact 优先于 global `*`；
- 缺 exact 时回退 `*`；
- usage_cost + operator_plan 拒绝。

`customer_charge`：

- plan exact > plan `*` > global exact > global `*`；
- 不存在 Rule 时拒绝 Customer Key Admission；
- 显式 0 Rule 被识别为 free；
- disabled Rule 不参与选择；
- 重复选择槽位由唯一约束拒绝，不按 ID 选胜者。

### 7.2 Draft/Publish/Rollback

- Draft 可以更新，Published 不能更新。
- 发布分配单调 revision 并保存 Hash/Analysis。
- Draft Validate 失败不能移动 Active Version。
- 并发发布一个成功、一个 409。
- stale `lock_version` 拒绝。
- 回滚只更新 Active Version 指针。
- 已有 Hold 在回滚后仍读取旧冻结 Version。
- 禁用 Rule 不影响历史 Evaluation 重放。
- Published Version 删除被 FK/服务阻止。

### 7.3 权限

- Admin 可管理 usage_cost/customer_charge。
- Operator 只能管理 customer_charge 和授权 Plan Scope。
- Auditor 只读。
- 跨 Surface、跨 Plan、未知 ID 返回不存在语义。
- Raw Expression、Version History 和 Evaluation 不泄露给无权用户。

## 8. Billing Hold 与 Gateway 集成

使用 fake upstream 覆盖：

1. 入站选择并冻结 Rule Version。
2. 请求转发期间发布新 Version。
3. fake upstream 返回最终 Usage。
4. 结算仍使用旧 Version。
5. 新请求使用新 Version。

场景矩阵：

- 普通 JSON、SSE streaming、取消、断流；
- direct 与 durable operation；
- max_tokens 缺失时保守默认；
- 图片数量、视频/音频时长有界与缺失；
- budget 80%/100% 和实际超预占；
- unpriced 且无 budget；
- unpriced 且有 budget；
- Pricing 编译缓存 miss；
- Settlement 缺 Facts、Hash 不匹配、金额溢出；
- Provider 成功但 Pricing disputed；
- Provider 证明未创建时 Hold release；
- Usage Version 递增和累计结算。

必须断言：

- HTTP/stream 行为；
- Operation、Attempt、Usage；
- Hold 状态、reserved/settled micros；
- Evaluation 状态和 failure code；
- Ledger、Outbox 和 Audit；
- 不存在重复记录；
- 请求响应不因结算后置失败被伪装成上游失败。

## 9. Ledger 与幂等测试

Memory/PostgreSQL 共用契约：

- 同一 Purpose 重放相同内容返回 not applied 且无错误。
- 同一幂等键但金额不同返回 `billing_ledger_conflict`。
- 同一 Usage 的 usage_cost/customer_charge 可同时存在。
- CustomerID 为空时不创建 customer_charge。
- Customer Key Admission 冻结 Resolver 返回的 Plan ID，结算时 Plan 改变不影响当前请求。
- Resolver 缺失、客户 disabled、返回未知币种或暂时失败时失效关闭。
- Usage、Evaluation、Ledger、Hold、Outbox 原子提交。
- 事务任一步注入失败后没有半条成功状态，或按设计留下明确 disputed 状态。
- restart 后重放仍幂等。
- cumulative amount 受检，无 int overflow。
- unpriced 不写金额为 0 的成功 Ledger。
- free 写 amount=0 的成功 Ledger 和 Evaluation。
- Usage Report 直接汇总 micros；大量小额请求不会因逐条格式化而归零或放大。

PostgreSQL 专项：

- FK、CHECK、UNIQUE、Published immutability；
- `FOR UPDATE`/CAS 并发发布；
- 同一 Usage 64 路并发只应用一次；
- deadlock/serialization failure 有界重试；
- transaction rollback；
- schema 重复初始化；
- 进程关闭重开后的 Version/Evaluation/Ledger/Hold 证据。

## 10. Operator Outbox 与余额

- Customer Charge Outbox 与 Ledger 同事务生成。
- fake/real CustomerPricingContextResolver 返回相同选择结果，且 Control Plane 不读取 Operator 价格字段。
- Consumer 按 Billing Ledger ID 读取冻结金额。
- 重复消息、乱序消息和 Consumer 重启不重复扣费。
- 数据库暂时失败后重试成功。
- 超过最大重试进入 dead letter，产生 Audit/Alert。
- dead letter 手工恢复仍使用原 Ledger 金额。
- 客户 Plan 在消费前改变不影响金额。
- Rule Active Version 在消费前改变不影响金额。
- Balance micros 精确扣减，小额多请求累计不丢失。
- Wallet、额度、兑换码、代金券和手工调账全程只接受 micros。
- API 提交任何客户侧 `*_cents` 或浮点金额字段都按未知字段拒绝。
- 非 USD Ledger 拒绝进入 USD Wallet。
- Customer Charge disputed 时不伪扣余额。
- Usage Finalization 后只有 Outbox Consumer 能写 Customer Charge 余额。

风险事件与扣费事件必须分别覆盖：

- `usage.recorded.v1` 只触发 Risk Consumer，不创建 Balance Entry。
- `customer_charge.posted.v1` 只触发 Balance Consumer，不重复执行风险规则。
- 同一 Usage 的两个事件可独立重试、乱序处理和进入 dead letter。
- Risk Consumer 的 spend 规则读取 `total_usage_cost_micros`，阈值边界按 micros 比较。
- RPM、Token、Spend、Error Rate 的 block/review 行为与重构前产品契约一致。
- 删除 Runtime 的同步 Usage Observer 注入后，服务重启仍能消费 pending 风险事件。

## 11. API 测试

使用 `httptest` 覆盖：

- 列表筛选、详情、Draft、Validate、Simulate、Publish、Rollback、Disable；
- 请求/响应 Schema 和稳定 Error Code；
- invalid JSON、未知字段策略、超大 Body；
- stale CAS -> 409；
- forbidden -> 404/403 按现有非披露契约；
- Validate 不持久化；
- Simulate 不写 Ledger/Balance；
- Operator 强制 customer_charge Purpose；
- 任意 Surface 的 `*/model-pricings` API 未注册，旧 ModelPricing 请求返回 404；
- `/admin/model-pricings` 与 `/platform/model-pricings` 前端路由不存在，只能通过各自 `/pricing` 页面进入；
- 新 API 的未知字段拒绝策略覆盖旧价格字段、浮点倍率和客户侧 `*_cents`；
- 审计记录不含表达式之外的请求敏感数据。

## 12. 前端测试

### 12.1 Unit/Component

- Money Decimal String <-> Micros 转换，无浮点。
- Rule Table 筛选、状态和版本摘要。
- Visual Tier 编辑生成 Source 后调用 Validate。
- Raw 错误定位和行列展示。
- `visual_editable=false` 时不自动重写 Source。
- Test Case 成功/失败明细。
- Simulator Facts、Tier、Lines 和总额。
- Publish Diff、影响确认和 disabled 状态。
- 409 冲突保留本地 Draft 并加载服务器版本。
- Version History 和 Rollback。
- Loading/Empty/Error/Read-only。
- Admin/Platform/Operator Purpose 与 Scope 隔离。

### 12.2 Browser E2E

关键旅程：

1. Admin 创建平价 usage_cost Rule，验证、模拟、发布。
2. 发起 fake upstream 请求，在 Usage 详情看到 Version/Tier/Lines。
3. 编辑长上下文 Draft，在 200k 边界模拟并发布。
4. 请求执行期间发布新版本，验证旧请求不漂移。
5. Operator 创建 Plan customer_charge Rule，客户请求后余额精确扣减。
6. 注入 Consumer 暂时失败，UI 看到 pending，恢复后只扣一次。
7. 发布错误版本后回滚，历史 Usage 仍可重放。
8. Auditor 只读，跨 Surface/Plan 拒绝。
9. Platform 在 `/platform/pricing` 发布 global usage_cost Rule，Operator 无法读取该 Surface 的 Draft。

每个旅程覆盖：

- 中英文；
- 明暗主题；
- 桌面和移动视口；
- 键盘发布/对话框焦点；
- 浏览器刷新后的 Draft/Version 状态；
- API、数据库、Audit、Usage、Ledger 和 Balance 结果。

## 13. Clean Install 与删除门禁

### 13.1 Schema 创建

- 空 PostgreSQL 数据库执行全部基线 SQL 后可直接启动服务。
- Memory Repository 和 PostgreSQL Repository 从空状态暴露相同契约。
- 基线 SQL、Repository 内联 Schema 和 Go Model 的表、列、类型、默认值、约束及索引一致。
- Schema 初始化在允许重复执行的边界内保持幂等，不产生重复索引或 Seed Version。
- 数据库重建后不存在 `model_pricings`、`operator_pricing_rules` 和任何客户侧 `*_cents` 列。
- `004`、`017`、`018`、`027`、`033`、`041`、`047`、`048`、`055` 基线 SQL 与对应 Repository 均只创建 micros 字段。
- 启动代码不探测旧表或旧列，也不根据 Schema 状态选择计价路径。

### 13.2 Seed Rule

- Seed 通过正式 Rule Service 创建、验证和发布，不直接写 Published Version。
- 每个 active Gateway Model 命中 `usage_cost` 精确规则或 `*` 规则。
- 每个 Customer Key 可达 Plan 命中 `customer_charge` 规则。
- 显式免费、Claude 长上下文、缓存和非 Token Dimension Rule 均有发布样例。
- Seed 重复执行不创建重复 Rule/Revision，内容冲突时明确失败。
- 删除数据库并重新 Seed 后，规则 Version/Hash、测试向量和选择结果确定一致。

### 13.3 静态删除扫描

CI 对后端、前端、SQL 和测试 fixture 执行 `rg` 门禁，以下内容一经出现即失败：

- `model_pricings`、`operator_pricing_rules` 及其旧 Model/DTO；
- `EstimateModelUsageCostCents`、`usageChargeCents`；
- 客户侧 `*_cents` JSON/Go/SQL/TypeScript 字段；
- 浮点价格与 `rate_multiplier`；
- 注册为计价用途的 Usage Observer；
- 运行时双引擎开关、旧公式回退或旧 Schema 探测。

允许供应商采购成本域保留其独立类型，但必须在扫描白名单中逐项列出文件与理由，不能使用宽目录排除。

## 14. 备份、恢复与进程重启

- 备份包含 Rule、Version、Evaluation、Ledger、Hold、Outbox、Operator Wallet。
- 空库恢复后历史 Evaluation 可重放。
- 恢复后 Active Version、lock_version 和 Outbox lease 正确。
- 恢复不会重新投递已成功 Customer Charge；pending 可以继续。
- 进程重启前后相同 Version/Facts 的结果一致。
- 未完成 Hold 在重启后仍能加载冻结的 V1 Engine Version。
- 发布包包含 expr 依赖许可证声明。

## 15. 性能与稳定性

先采集基线，再设置 ratchet，不用未经测量的绝对数字替代证据。

基准至少包括：

- 平价规则 cached Evaluate；
- 2/8/16 Tier 规则 cached Evaluate；
- 32 Line 上限；
- cold Compile；
- 64/256 并发 Evaluate；
- LRU churn；
- Facts Canonical JSON/Hash；
- Usage Final Transaction 前后对比。

硬要求：

- Evaluate 热路径无网络和数据库访问。
- 已发布 Active Version 在请求热路径不重复解析 Source。
- Cache 上限稳定，无无界内存增长。
- 30 分钟普通/streaming soak 无 goroutine、Trace Collector 或 Program 泄漏。
- Pricing 引入后的 Gateway 吞吐/延迟回归超过已批准阈值时阻断发布。

## 16. 建议命令

实现后按窄到宽运行：

```bash
cd backend
go test ./internal/pricing -count=1
go test ./internal/controlplane -run 'TestPricing|TestBillingHold|TestUsageLedger' -count=1
go test ./internal/operator -run 'TestPricing|TestCustomerCharge' -count=1
go test ./internal/server -run 'TestPricing' -count=1
go test ./...
go test -race ./internal/pricing ./internal/controlplane ./internal/operator -count=1
```

PostgreSQL：

```bash
cd backend
ASTER_TEST_DATABASE_URL='<isolated-url>' go test ./internal/controlplane ./internal/operator ./migrations -count=1
```

前端：

```bash
cd frontend
npm run test:unit -- pricing
npm run typecheck
npm run build
npm run check:enterprise-surface
npm run test:e2e -- pricing
```

发布验收继续执行仓库现有生产单源、Docker/Linux、备份恢复与浏览器旅程。

## 17. CI 门禁

| 阶段 | 必跑 | 发布条件 |
| --- | --- | --- |
| PR | pricing unit/fuzz seed、controlplane/operator、API、frontend unit、PostgreSQL P0 | 全绿 |
| main | PR 全集、production single-origin、clean-schema parity、pricing E2E smoke | 全绿 |
| nightly | race、完整 PostgreSQL、fuzz timebox、全 E2E、soak、benchmark trend | 无 P0/P1 |
| 发布包 | clean install、静态删除扫描、备份恢复、Operator Outbox、Linux artifacts | 人工批准且全绿 |

必须保留 Artifact：

- JUnit/coverage；
- fuzz crash corpus；
- benchmark before/after；
- clean schema/runtime schema diff；
- 静态删除扫描结果；
- Seed Rule Version/Hash 清单；
- Playwright trace/screenshots；
- fake upstream 请求摘要；
- disputed/dead letter 计数；
- 发布规则 Version/Hash 清单。

所有 Artifact 必须脱敏。

## 18. 发布阻断条件

任一条件成立都禁止合入或发布：

- active 模型/客户 Plan 缺必需 Rule；
- Published Version 可变或可删除；
- Hash mismatch 没有失效关闭；
- Memory/PostgreSQL Rule Selection 不一致；
- 请求期间改价导致版本漂移；
- Ledger/Balance 重复应用；
- unpriced 被聚合为免费；
- 非 USD 进入 USD Wallet/Budget；
- Pricing Facts 或错误日志包含敏感 marker；
- 存在 unresolved Pricing Dispute 或 Outbox dead letter；
- V1 Engine Version 在进程重启后无法重放；
- clean install 后发现旧价格表、旧 ModelPricing API/DTO、客户侧 `*_cents` 字段或浮点倍率；
- 发现 `EstimateModelUsageCostCents`、`usageChargeCents`、计价 Observer 或第二条 Customer Charge 计算路径；
- PostgreSQL 测试、备份恢复或发布环境测试被跳过且没有明确批准。
