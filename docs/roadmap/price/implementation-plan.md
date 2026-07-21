# 实施计划

## 1. 实施策略

采用六阶段 clean-break 重构。阶段用于组织开发和评审，不代表生产运行时存在两套引擎。最终合入必须同时完成新 Schema、新 API、新前端和新账务链路。

```text
Phase 0 账务不变量与基线
  -> Phase 1 纯表达式引擎
  -> Phase 2 规则版本与 Evaluation
  -> Phase 3 全量 micros 与 Usage/Hold/Ledger
  -> Phase 4 Operator 与前端重写
  -> Phase 5 删除旧实现并原子切换
```

估算为 29-44 工程人日，不含 PostgreSQL 环境准备和产品验收时间。全量 micros 涉及客户账务、预算、设置、报表、通知和平台 Usage Payload，不能按单表改名估算。

## 2. Clean-break 约束

- 不增加 `pricing_engine_mode`、双算开关或旧路径 fallback。
- 开发数据库和测试 fixture 直接按新 Schema 重建。
- 旧 API、旧 DTO、旧表、旧金额字段和旧 Helper 在同一变更中删除。
- 所有金额输入、存储、聚合和 API 输出只使用 micros。
- 主线在完整测试通过前不接收会留下半套计费架构的阶段性合并。
- 规则发布错误通过激活上一 Published Version 恢复；引擎或 Schema 错误通过修复代码前进，不保留旧引擎作为回退。

## 3. Phase 0：账务不变量与基线（3-5 人日）

目标：锁定新账务不变量、删除范围和一次性切换边界。

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| PRICE-001 | 盘点所有客户侧 `*_cents` Model/DTO/Schema/查询及传播点 | 完整替换清单 |
| PRICE-002 | 盘点旧价格表、API、前端类型和计价 Helper 的全部调用点 | 依赖图与删除清单 |
| PRICE-003 | 定义 V1 仅 USD 的数据库、服务和 API 门禁 | 币种不变量 |
| PRICE-004 | 定义 MoneyMicros 和受检预算/余额数学 | 纯数学包与测试 |
| PRICE-005 | 定义 Pricing Purpose、Facts、Result、Error Contract | ADR/Go 类型 |
| PRICE-006 | 采集当前 Usage、Hold、Ledger、Operator Debit 的测试与性能基线 | 基线报告 |

退出条件：

- 所有客户侧金额字段都有唯一 micros 替换项，没有未分类传播点。
- V1 Published Rule 明确只支持 USD。
- 产品确认 `usage_cost`、`customer_charge`、`procurement_cost` 三种语义。

## 4. Phase 1：纯表达式引擎（4-6 人日）

目标：完成无数据库依赖、可独立 fuzz/race/benchmark 的 `internal/pricing`。

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| PRICE-101 | 引入 `github.com/expr-lang/expr` 并更新第三方声明 | MIT 依赖 |
| PRICE-102 | 实现 V1 Facts、Result、Line、Tier 和稳定错误码 | `internal/pricing/types.go` |
| PRICE-103 | 实现显式版本解析、SHA-256 校验和未知版本拒绝 | `version.go` |
| PRICE-104 | 实现类型环境、AST 白名单、节点/深度/长度限制 | `compile.go` |
| PRICE-105 | 实现受检 Token/Unit/Block/BPS Helper | `evaluate.go` |
| PRICE-106 | 实现独立 Trace Collector、Line/Tier 合计校验 | `breakdown.go` |
| PRICE-107 | 实现 LRU + singleflight 编译缓存 | `cache.go` |
| PRICE-108 | 实现 RuleAnalysis、验证向量和模拟 | `validate.go` |
| PRICE-109 | 单元、fuzz、race、benchmark | `*_test.go` |

退出条件：

- 相同输入重复执行逐位一致。
- Hash mismatch、未知版本、未知 Fact、禁用 AST、负数、溢出和 Line 合计错误全部失效关闭。
- 并发编译/执行/淘汰无竞态。
- 复杂 Claude 分层、缓存和图片规则通过 Golden Tests。
- 引擎包不导入 Control Plane、Operator、Gin 或数据库代码。

## 5. Phase 2：规则版本与持久化（5-7 人日）

目标：建立 Rule Head、Draft/Published Version、Evaluation 和唯一 Pricing Rule API。

直接改写基线 Schema：删除 `006_model_pricings.sql` 并创建 `006_pricing_rules.sql`，同步修改 `004_usage_records.sql`、`017_departments.sql`、`018_governance_policies.sql`、`027_operator_core.sql`、`033_workspace_user_limits.sql`、`041_customer_billing.sql`、`047_api_key_principal_policy.sql`、`048_operation_usage_billing_outbox.sql`、`055_billing_holds.sql` 及 Repository 内联 Schema。开发数据库必须重建。

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| PRICE-201 | 新增 Rule/Version/Evaluation Model 与 Repository 接口 | Memory/PostgreSQL 实现 |
| PRICE-202 | 重写基线 Schema、约束、索引和 runtime schema mirror | clean-install schema |
| PRICE-203 | 实现 deterministic Rule Selector | global/plan exact/wildcard |
| PRICE-204 | 实现 Draft Validate、Publish CAS、Disable、Activate/Rollback | Pricing Rule Service |
| PRICE-205 | 实现 Admin/Platform/Operator 新 API 与权限 | Gin routes + httptest |
| PRICE-206 | 实现模拟和当前 Usage Facts 导入 | Admin-only service |
| PRICE-207 | 实现 Audit Log 与安全错误映射 | 稳定 API error code |
| PRICE-208 | PostgreSQL 约束、并发发布和重启持久化测试 | Repository contract |

退出条件：

- Published Version 无法更新或删除。
- 并发发布只有一个成功，失败方收到 409。
- Rule Selection 在 Memory/PostgreSQL 结果一致。
- Admin、Platform 与 Operator 不能跨 Purpose/Scope 访问规则。
- 新 API 是仓库内唯一价格管理 API。

## 6. Phase 3：全量 micros、预占、结算与账本（9-13 人日）

目标：一次性把客户侧金额和完整账务链路切到新引擎。

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| PRICE-301 | Canonical Request/Usage -> PricingFacts Builder | 显式 facts normalization |
| PRICE-301A | 注入 CustomerPricingContextResolver 并冻结 Customer/Plan 上下文 | 无 controlplane/operator 循环依赖 |
| PRICE-301B | Policy/Canonical Limits/Budget 全部改为 micros | 无 cents 预算字段 |
| PRICE-302 | Billing Hold 保存 Rule Version 与 estimate Evaluation | 微美元预占 |
| PRICE-303 | Usage Ledger 直接采用 Purpose、amount_micros 和 Evaluation FK | 多 Purpose 幂等键 |
| PRICE-304 | UsageRecord 直接采用 usage_cost micros/status/evaluation | priced/free/unpriced/disputed |
| PRICE-304A | Usage Report 直接汇总 micros | 小额请求不丢精度 |
| PRICE-305 | Final Usage 事务原子写 Evaluation、Ledger、Hold、Outbox | Memory/PostgreSQL parity |
| PRICE-306 | 实现 settlement disputed 与 reconciliation 任务 | 不伪造 fallback 金额 |
| PRICE-307 | Workspace、Wallet、预算、设置、报表、风险、告警、通知和 Platform Sink 全量改用 micros | 单一金额精度 |
| PRICE-308 | 增加 Usage/Trace/Audit 的 Rule Version 与 Tier 证据 | 可查询链路 |
| PRICE-309 | 删除硬编码货币符号，金额展示统一使用 micros + currency | USD V1 展示一致性 |

Facts Builder 的落点建议：

```text
backend/internal/controlplane/pricing_facts.go
backend/internal/controlplane/pricing_rule_service.go
backend/internal/controlplane/pricing_evaluation_service.go
```

纯引擎继续放在 `internal/pricing`，不能回流依赖 Control Plane。

退出条件：

- 请求期间发布新价格不会改变该请求结算 Version。
- estimate/settlement 都能追溯 Expression Hash 和 Facts Hash。
- Provider 已消费后的表达式失败会保存 Usage 并进入 disputed。
- 同一 usage_version 重放不会产生第二条 Ledger。
- Go Model、SQL、JSON 和前端类型中不存在本专题范围内的 cents 金额字段。

## 7. Phase 4：Operator 收敛与管理界面（5-8 人日）

目标：只计算一次 Customer Charge，并提供完整操作界面。

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| PRICE-401 | 直接重写 Operator Customer/Balance/Wallet Schema 与 DTO，只保留 micros | 精确钱包 |
| PRICE-402 | Customer Charge Ledger + Outbox Consumer | 幂等余额扣减与重试 |
| PRICE-403 | 删除 `operator_pricing_rules`、浮点价格和 `rate_multiplier` 字段 | 单一 Rule 事实源 |
| PRICE-403A | Plan 删除增加 Pricing Rule 引用保护 | 无跨 Repository 数据库 FK 的完整性补偿 |
| PRICE-404 | 拆分 Usage Risk/Customer Charge Event；删除同步计价 Observer、`usageChargeCents` 和二次余额写入 | 两个独立持久 Consumer |
| PRICE-405 | Admin/Platform/Operator 共享 Pricing Feature | 列表、编辑、模拟、版本 |
| PRICE-406 | 后端 AST Analysis 驱动 Visual Editor | 不使用前端完整正则解析 |
| PRICE-407 | Publish/回滚 Diff 与影响确认 | 审计证据 |
| PRICE-408 | 组件、API 和浏览器 E2E | 桌面/移动/i18n/a11y |

倍率不再作为 Plan 或价格表字段存在。需要倍率语义时，管理员必须在新表达式中显式使用受检 BPS Helper，并由计费行记录调整前金额、BPS 和调整后金额。新 Schema、Seed 和 API 均不读取旧浮点倍率。

退出条件：

- 新 Customer Charge 由 Usage 事务冻结，Operator Consumer 不再选择规则。
- Outbox 暂时失败可重试，重复投递不重复扣费。
- Operator 数据模型、API 和前端状态只包含 micros 金额。
- `operator_pricing_rules`、旧倍率字段和计价 Observer 已从 Schema 与代码中删除。
- RPM/Token/Spend/Error Rate 风险规则改由 `usage.recorded.v1` Consumer 执行，计价重构不降低风险能力。
- Visual/Raw 切换不损坏不可视化表达式。
- 发布和回滚都有明确影响确认。

## 8. Phase 5：删除旧实现并原子切换（3-5 人日）

目标：删除所有旧价格表、类型和调用点，以一套完整的新 Schema 与运行时原子合入主线。

| ID | 工作项 | 交付物 |
| --- | --- | --- |
| PRICE-501 | 删除旧 Admin Model Pricing API、DTO、Service 和页面 | 无旧管理入口 |
| PRICE-502 | 建立 disputed、unpriced、outbox dead letter 监控 | 运行告警 |
| PRICE-503 | 删除 `model_pricings`、`operator_pricing_rules` 及全部旧列/索引/查询 | clean-install Schema |
| PRICE-504 | 删除 `EstimateModelUsageCostCents`、`usageChargeCents` 及旧测试 fixture | PricingEvaluator 唯一入口 |
| PRICE-505 | 重建开发/测试数据库并写入 V1 Seed Rule | 可启动的新环境 |
| PRICE-506 | 执行静态删除扫描、完整后端/前端/PostgreSQL/E2E 测试 | 合入证据 |
| PRICE-507 | 同一变更合入新 Schema、API、前端和账务链路 | 原子切换 |

退出条件：

- 所有 active Gateway Model 有明确 `usage_cost` Rule，或由产品显式配置为允许 unpriced。
- 所有 Customer Key 可达模型都有 `customer_charge` Rule，包括显式免费规则。
- clean install、进程重启、备份恢复、生产单源构建和关键 E2E 通过。
- 仓库静态扫描确认旧表、旧字段、旧 DTO、旧 Helper 和计价 Observer 均不存在。
- 无 unresolved P0/P1 Pricing Dispute，Outbox 无持续 dead letter。

## 9. 开发数据库重建与 Seed

### 9.1 唯一支持路径

- 删除现有本地和测试数据库后，从改写后的基线 SQL 创建新库。
- 不提供旧 Schema 到新 Schema 的升级脚本，不读取旧表，不探测旧列。
- Repository 内联建表语句必须与 PostgreSQL 基线 SQL 保持同一结构和约束。
- CI 每次从空数据库启动，禁止依赖开发者机器上已有数据。

### 9.2 Seed Rule

Seed 直接使用新契约创建 Draft、验证并发布 Version，不从旧价格行转换。至少包含：

- active Gateway Model 对应的 `usage_cost` 精确规则或全局 `*` 规则；
- Customer Key 可达 Plan 对应的 `customer_charge` 规则；
- 显式免费规则，不以缺少规则表示免费；
- Claude 长上下文、缓存计费和一个非 Token Dimension 的测试规则；
- 每条规则的发布样例、边界样例和预期计费明细。

Seed 必须通过正式 Rule Service 执行，不直接拼接 Published Version 行，从而复用 Hash、Analysis、CAS 和审计不变量。确有价值的本地价格配置由开发者按新表达式重新录入，不建设数据转换工具。

### 9.3 重建验收

```text
drop disposable development database
  -> create clean schema
  -> seed pricing rules
  -> start backend
  -> execute fake-upstream billing journey
  -> restart and replay ledger idempotently
```

重建后发现任何旧表、客户侧 cents 字段或浮点倍率字段都应立即失败。

## 10. 原子落地与恢复策略

### 10.1 合入边界

开发可按 Phase 在功能分支组织提交，但主线只接收满足以下条件的完整变更：

1. 新 Schema、Repository、Service、API、前端和 Seed 同时可用。
2. 旧 Schema、API、DTO、Helper、Observer 和页面同时删除。
3. Gateway、Budget、Hold、Usage、Ledger、Outbox 和 Wallet 全链路只使用 micros。
4. clean install 与完整测试矩阵全部通过。

不提供运行时双路径、按模型开关或旧公式回退。阶段性代码不能以半套状态部署。

### 10.2 恢复

- 规则配置错误：通过 CAS 激活上一 Published Version；已创建 Hold 继续使用其冻结 Version。
- 引擎或 Schema 缺陷：修复代码并向前恢复；结算失败保存 Usage 并进入 disputed。
- UI 缺陷：使用同一新 Rule API 的 Raw 模式处理，不恢复旧页面或旧 DTO。
- Operator Consumer 缺陷：暂停消费并保留 Outbox，修复后按 Ledger ID 幂等恢复。
- Published Version 不可变且不可删除，因此恢复规则指针不会改变历史 Evaluation 的重放结果。

## 11. 文件影响范围

后端新增或重写：

```text
backend/go.mod
backend/internal/pricing/*
backend/internal/controlplane/model.go
backend/internal/controlplane/repository.go
backend/internal/controlplane/pricing_rule_*.go
backend/internal/controlplane/pricing_evaluation_*.go
backend/internal/controlplane/pricing_facts.go
backend/internal/controlplane/billing_hold.go
backend/internal/controlplane/billing_hold_repository.go
backend/internal/controlplane/customer_billing_*.go
backend/internal/controlplane/department_*.go
backend/internal/controlplane/governance_policy_*.go
backend/internal/controlplane/api_key_policy.go
backend/internal/controlplane/cost_allocation.go
backend/internal/controlplane/alert_service.go
backend/internal/controlplane/customer_notification_service.go
backend/internal/controlplane/platform_usage_sink_service.go
backend/internal/controlplane/operation_model.go
backend/internal/controlplane/operation_repository.go
backend/internal/controlplane/operation_service.go
backend/internal/controlplane/service.go
backend/internal/operator/model.go
backend/internal/operator/repository.go
backend/internal/operator/postgres_repository.go
backend/internal/operator/service.go
backend/internal/appcmd/server/runtime.go
backend/internal/server/admin_routes.go
backend/internal/server/platform_routes.go
backend/internal/server/shared_core_routes.go
backend/internal/server/pricing_rule_routes.go
backend/internal/server/operator_routes.go
backend/internal/server/rbac.go
backend/internal/settings/model.go
backend/internal/settings/service.go
backend/migrations/*
```

后端明确删除：

```text
backend/internal/controlplane/model_pricing.go
backend/internal/server/model_pricing_routes.go
Repository 中的 ModelPricing 方法与存储
Service.SetUsageObserver 与 Operator 计价 OnGatewayUsage
旧 ModelPricing/OperatorPricingRule/Balance cents 测试 fixture
```

前端新增或重写：

```text
frontend/src/features/pricing/*
frontend/src/api/control.ts
frontend/src/api/customer.ts
frontend/src/api/operator.ts
frontend/src/types.ts
frontend/src/router/index.ts
frontend/src/views/admin/AdminPricingView.vue
frontend/src/views/admin/AdminShell.vue
frontend/src/views/admin/AdminPoliciesView.vue
frontend/src/views/admin/AdminDepartmentsView.vue
frontend/src/views/admin/AdminUsageView.vue
frontend/src/views/customer/CustomerBillingView.vue
frontend/src/views/operator/OperatorBalancesView.vue
frontend/src/views/operator/OperatorCustomersView.vue
frontend/src/views/operator/OperatorPlansView.vue
frontend/src/views/operator/OperatorPricingView.vue
frontend/src/views/platform/PlatformShell.vue
frontend/src/locales/*
```

前端明确删除：

```text
frontend/src/views/admin/AdminModelPricingsView.vue
frontend/src/views/admin/AdminModelPricingsView.test.ts
ModelPricing/OperatorPricingRule 旧类型与 API Client
/admin/model-pricings 路由和导航项
/platform/model-pricings 路由和导航项
所有客户侧 cents 字段与浮点金额表单状态
```

CI/文档：

```text
docs/test/v1/*
docs/roadmap/price/*
THIRD-PARTY-LICENSES or repository equivalent
.github/workflows/* when adding required tests
```

## 12. 风险与缓解

| 风险 | 等级 | 缓解 |
| --- | --- | --- |
| 错误规则导致错误扣费 | P0 | Draft/Publish、发布样例、不可变版本回滚、disputed |
| 请求期间改价导致漂移 | P0 | Hold 冻结 Rule Version ID |
| Operator 二次定价不一致 | P0 | Ledger 冻结金额 + Outbox，删除 Observer 计算 |
| 研发数据无法沿用 | P2 | 明确重建数据库；必要配置按新规则重新录入 |
| 非 USD 混入 USD 预算/钱包 | P0 | V1 数据库/服务双重拒绝 |
| 表达式滥用 CPU/内存 | P1 | AST/长度/深度限制、无循环、缓存、benchmark |
| 原始请求泄露到计费事实 | P0 | Canonical Fact 白名单与序列化测试 |
| 基线 SQL 与 Runtime Schema 漂移 | P0 | clean-install/runtime schema parity 测试 |
| 多实例并发发布 | P1 | CAS/row lock、唯一约束、409 |
| Control Plane 与 Operator 形成循环依赖 | P1 | 窄 Resolver 接口由 Runtime 注入，Plan 引用服务层校验 |
| Outbox 消费积压导致漏扣 | P0 | retry/dead letter/告警/Customer Key 策略 |
| 并发请求在异步扣费前形成负余额 | P1 | 明确沿用现有产品策略；需要硬门禁时另建 customer_balance_holds |
| AGPL 源码进入 Apache 项目 | P1 | 只依赖 MIT expr，自行实现并做来源审查 |

## 13. Definition of Done

- 所有成功 Usage Cost 和 Customer Charge Ledger 都有 Evaluation 与不可变 Rule Version。
- 所有预算/Hold 使用 micros 比较并冻结 Version。
- Operator 不再执行价格公式。
- `float64` 不出现在客户侧金额 Model、DTO、Schema 或计算中。
- PostgreSQL clean install、重复初始化、重启、备份恢复和 Schema parity 通过。
- Admin/Platform/Operator UI 完成中英文、明暗主题、桌面/移动、键盘和错误态验收。
- P0/P1 测试矩阵全绿，无 unresolved dispute/dead letter。
- 开发数据库和测试 fixture 均从新 Schema 重建，Seed Rule 可重复创建完整环境。
- `rg` 扫描后，后端与前端中不存在 `model_pricings`、`operator_pricing_rules`、`EstimateModelUsageCostCents`、`usageChargeCents`、旧 ModelPricing DTO 或计价 Observer。
- Go Model、SQL、JSON 与前端状态中不存在客户侧 `*_cents` 字段；旧金额输入按未知字段拒绝。
- 仓库只有一个 PricingEvaluator 运行时入口和一条 Customer Charge 扣费路径。
