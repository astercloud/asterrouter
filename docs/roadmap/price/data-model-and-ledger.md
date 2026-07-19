# 数据模型与账本设计

## 1. 设计原则

- 已发布价格不可修改，只能发布新版本或重新激活旧版本。
- Rule Head 负责稳定选择槽位，Rule Version 负责冻结公式和币种。
- Pricing Evaluation 是一次计算的证据，不等同于 Billing Ledger。
- 同一 Usage 可以有 `usage_cost` 和 `customer_charge` 两个 Evaluation 与 Ledger Entry。
- Provider 的 `procurement_cost` 保持独立，不写入客户侧 Ledger Purpose。
- 金额事实只使用微美元；数据库、Go Model、API 和前端状态中不保留 cents 金额字段。
- 免费是金额为 0 的显式规则，不是缺少规则。

## 2. 目标关系

```mermaid
erDiagram
    PRICING_RULE ||--o{ PRICING_RULE_VERSION : versions
    PRICING_RULE_VERSION ||--o{ PRICING_EVALUATION : evaluates
    AI_OPERATION ||--o{ PRICING_EVALUATION : owns
    USAGE_RECORD ||--o{ PRICING_EVALUATION : settles
    PRICING_EVALUATION ||--o| BILLING_LEDGER_ENTRY : posts
    BILLING_HOLD ||--o{ BILLING_HOLD_PRICING_VERSION : freezes
    PRICING_RULE_VERSION ||--o{ BILLING_HOLD_PRICING_VERSION : snapshots
    PRICING_EVALUATION ||--o{ BILLING_HOLD_PRICING_VERSION : evidences
    BILLING_LEDGER_ENTRY ||--o| OPERATOR_BALANCE_ENTRY : debits
    AI_OPERATION ||--o| BILLING_HOLD : holds

    PRICING_RULE {
        text id PK
        text purpose
        text scope_type
        text scope_id
        text model
        text status
        text active_version_id FK
        bigint lock_version
    }
    PRICING_RULE_VERSION {
        text id PK
        text rule_id FK
        int revision
        int engine_version
        text currency
        text expression
        text expression_hash
        jsonb used_facts
        jsonb analysis
        text state
    }
    PRICING_EVALUATION {
        text id PK
        text purpose
        text phase
        text operation_id FK
        text attempt_id
        int usage_version
        text pricing_rule_version_id FK
        text facts_hash
        jsonb facts
        bigint amount_micros
        text currency
        text matched_tier
        jsonb line_items
        text status
        text failure_code
    }
    BILLING_HOLD_PRICING_VERSION {
        text hold_id PK_FK
        text purpose PK
        text pricing_rule_version_id FK
        text estimate_evaluation_id FK
        text settlement_evaluation_id FK
    }
    BILLING_LEDGER_ENTRY {
        text id PK
        text purpose
        bigint amount_micros
        text pricing_evaluation_id FK
    }
```

## 3. `pricing_rules`

Rule Head 表示一个不可重叠的选择槽位。建议字段：

| 字段 | 类型 | 约束/语义 |
| --- | --- | --- |
| `id` | text | `prule_...`，主键 |
| `name` | text | 管理员名称 |
| `purpose` | text | `usage_cost` 或 `customer_charge` |
| `scope_type` | text | `global` 或 `operator_plan` |
| `scope_id` | text | global 必须为空；operator_plan 保存已校验的 Plan ID |
| `model` | text | 精确 Gateway Model ID 或 `*` |
| `status` | text | `active` 或 `disabled` |
| `active_version_id` | text nullable | 指向本 Rule 的已发布版本 |
| `lock_version` | bigint | CAS 发布、禁用和回滚 |
| `created_by` / `updated_by` | text | 审计 Actor |
| `created_at` / `updated_at` | timestamptz | UTC |

数据库与服务约束：

- 唯一键为 `(purpose, scope_type, scope_id, model)`。
- `usage_cost` V1 只允许 `scope_type=global`。
- `customer_charge` 允许 global 和 operator_plan。
- `model != '*'` 时必须引用已存在的 Gateway Model；模型后续 disabled 不删除规则。
- `scope_type=operator_plan` 时由 Operator API 和 `CustomerPricingContextResolver` 校验 `scope_id`。V1 不创建跨 `controlplane/operator` Repository 的数据库 FK，避免启动建表顺序和包依赖循环；Plan 删除服务必须反查 Pricing Rule。任何拥有 Published Version 的 Rule Head 仍引用该 Plan 时都拒绝删除，不能只靠禁用 Rule 绕过历史完整性。
- Rule 创建后不允许改变 Purpose、Scope 或 Model；需要改变选择槽位时创建新 Rule 并禁用旧 Rule。
- 删除只允许尚未发布且从未被引用的草稿 Rule。已发布 Rule 只能禁用。

## 4. `pricing_rule_versions`

建议字段：

| 字段 | 类型 | 约束/语义 |
| --- | --- | --- |
| `id` | text | `pver_...`，主键 |
| `rule_id` | text | FK `pricing_rules(id) ON DELETE RESTRICT` |
| `revision` | integer | 同一 Rule 内递增，发布时分配 |
| `engine_version` | integer | V1 固定为 1 |
| `currency` | text | V1 Published Rule 固定 USD |
| `expression` | text | 唯一计算事实源，最大 8 KiB |
| `expression_hash` | text | 64 字符小写 SHA-256 |
| `used_facts` | jsonb | 编译分析的事实名数组 |
| `analysis` | jsonb | Tier、Line、视觉可编辑能力等派生证据 |
| `authoring_mode` | text | `visual` 或 `raw`，不参与计算 |
| `state` | text | `draft` 或 `published`；是否 active 由 Rule Head 指针表达 |
| `created_by` / `published_by` | text | Actor |
| `created_at` / `published_at` | timestamptz | UTC |

约束：

- 唯一键 `(rule_id, revision)`；草稿可以用临时 revision 0，发布事务内分配正式 revision。
- `published` 后禁止 UPDATE/DELETE。Repository 和 PostgreSQL Trigger/权限至少有一层强制保护。
- `active_version_id` 必须属于同一 Rule 且 state 为 published。
- expression、engine_version 和 Hash 必须一致，服务端在写入前重新计算。
- `used_facts` 和 `analysis` 是派生证据，表达式是唯一执行事实源；引擎升级测试应重新分析并比对。
- 回滚只把 Rule Head 的 `active_version_id` CAS 指回历史 Published Version，不复制或修改历史版本。

## 5. `pricing_evaluations`

Evaluation 保存一次预估、结算或重放的计算证据。

| 字段 | 类型 | 约束/语义 |
| --- | --- | --- |
| `id` | text | `peval_...` |
| `purpose` | text | `usage_cost` 或 `customer_charge` |
| `phase` | text | `estimate`、`settlement` 或 `replay` |
| `operation_id` | text | FK Operation，模拟可为空并使用独立审计表 |
| `attempt_id` | text | 最终 Usage 对应 Attempt，可为空 |
| `usage_version` | integer | estimate 为 0，settlement > 0 |
| `usage_record_id` | text | settlement 时引用 Usage Record |
| `pricing_rule_id` | text | 冗余稳定选择证据 |
| `pricing_rule_version_id` | text | 不可变版本 FK ON DELETE RESTRICT |
| `engine_version` | integer | 执行时引擎版本 |
| `expression_hash` | text | 执行前验证后的 Hash |
| `facts_hash` | text | Canonical Facts SHA-256 |
| `facts` | jsonb | 非敏感规范化事实 |
| `amount_micros` | bigint | 成功时非负 |
| `currency` | text | V1 USD |
| `matched_tier` | text | 最终 Tier |
| `line_items` | jsonb | Canonical PricingLine 数组 |
| `normalization_status` | text | Usage 事实质量 |
| `status` | text | `succeeded`、`failed` 或 `disputed` |
| `failure_code` | text | 稳定错误码，不保存异常正文 |
| `created_at` | timestamptz | UTC |

唯一约束：

- estimate：`(operation_id, purpose, phase)` 唯一。
- settlement：`(operation_id, attempt_id, usage_version, purpose, phase)` 唯一。
- replay 不覆盖原 Evaluation，使用新 ID 并通过 `replay_of_id` 引用原记录。

一致性约束：

- `succeeded` 必须有 Rule Version、事实 Hash、表达式 Hash、币种和非负金额。
- `failed/disputed` 必须有稳定 `failure_code`；金额不得伪装成成功金额。
- `sum(line_items.amount_micros) == amount_micros`。
- Facts 不能含 Payload、Header、Secret 或原始身份标识；由服务层构建白名单结构。

## 6. Billing Hold

直接重写 `billing_holds` 金额与价格引用：

| 新字段 | 类型 | 语义 |
| --- | --- | --- |
| `reserved_amount_micros` | bigint | 权威预占金额 |
| `settled_amount_micros` | bigint | 权威结算金额 |

规则版本和 Evaluation 使用关联表保存，避免 `usage_cost`、`customer_charge` 继续扩展 Hold 宽表：

```text
billing_hold_pricing_versions(
  hold_id,
  purpose,
  pricing_rule_version_id,
  estimate_evaluation_id,
  settlement_evaluation_id,
  PRIMARY KEY (hold_id, purpose)
)
```

其中 `billing_holds.reserved_amount_micros/settled_amount_micros` 只代表 `usage_cost` 预算口径；`customer_charge` 的预估证据保存在关联表，不混入同一金额列。

删除 `reserved_amount_cents`、`settled_amount_cents` 和 `price_snapshot_id`。治理策略、Canonical Limits 与请求上限统一改为 `monthly_budget_micros`、`max_cost_micros`，预算比较不再做 cents/micros 转换。

Hold 状态机保持 `reserved -> committed -> settled/released/disputed`，增加以下原因码：

- `pricing_rule_unavailable`
- `pricing_fact_missing`
- `pricing_evaluation_failed`
- `pricing_ledger_conflict`
- `customer_charge_delivery_failed`

## 7. Billing Ledger

直接重写 `billing_ledger_entries`：

| 新字段 | 类型 | 语义 |
| --- | --- | --- |
| `purpose` | text | `usage_cost` 或 `customer_charge` |
| `amount_micros` | bigint | 权威金额 |
| `pricing_evaluation_id` | text | 成功 Evaluation FK |
| `pricing_rule_version_id` | text | 冗余查询索引和防删除 FK |

唯一约束为：

```text
(operation_id, attempt_id, usage_version, purpose)
```

幂等冲突比较：

- `amount_micros`
- `currency`
- `purpose`
- `pricing_evaluation_id`
- `usage_record_id`
- `request_fingerprint`

同一 Usage 可有两条 Ledger：

```text
usage_cost       -> Usage Report / Budget / Billing Hold
customer_charge  -> Operator Balance Outbox
```

没有 CustomerID 时不创建 `customer_charge` Ledger。

## 8. Usage Record

直接删除 `UsageRecord.CostCents`，使用以下字段：

| 字段 | 类型 | 语义 |
| --- | --- | --- |
| `usage_cost_micros` | bigint nullable | 内部 Usage 成本；nil 表示未计价，0 表示显式免费 |
| `usage_cost_currency` | text | V1 USD |
| `usage_pricing_evaluation_id` | text | 对应 usage_cost Evaluation |
| `pricing_status` | text | `priced`、`free`、`unpriced` 或 `disputed` |

必须区分：

- `nil + unpriced`：缺少规则或失败；
- `0 + free`：规则成功并明确返回 0；
- `>0 + priced`：成功计价；
- `nil + disputed`：Provider 已消费但结算不能完成。

报表不得把 unpriced 当作 0 成本。聚合结果增加 `priced_requests`、`unpriced_requests`、`disputed_requests` 和 `cost_available`。

Usage Report 直接汇总 `usage_cost_micros`。API 返回 `total_usage_cost_micros` 和各分组的 `usage_cost_micros`，前端统一格式化为美元字符串。

## 9. Operator 余额与 Outbox

### 9.1 不再在 Observer 里计价

当前 `OnGatewayUsage` 会再次选择 `operator_pricing_rules` 并用浮点倍率计算。目标流程改为：

1. Usage 最终结算事务计算并保存 `customer_charge` Evaluation 和 Ledger。
2. 同一事务写 `customer_charge.posted.v1` Outbox，Payload 只含 Ledger ID、Customer ID、精确金额、币种和幂等键。
3. Operator Consumer 读取 Ledger 并更新余额，不接收或执行表达式。
4. 唯一 reference 使用 `customer_charge:<billing_ledger_id>`。
5. 临时失败重试，永久失败进入 dead letter 和 Admin Alert。

现有 `OnGatewayUsage` 还调用 `evaluateRiskRules`，不能随计价逻辑一起丢失。重构时必须拆成两个事件：

- `usage.recorded.v1`：由专用 Risk Consumer 处理 RPM、Token、Spend 和 Error Rate，不读取价格规则、不写余额；
- `customer_charge.posted.v1`：由 Balance Consumer 按冻结 Ledger 扣款，不执行风险规则、不重新计价。

删除 `UsageObserver` 的同步 Runtime 注入、Operator `OnGatewayUsage` 中的计价分支和全部余额写入。风险评估改为消费持久 Usage Event；其 Spend 输入直接使用 Usage Report 的 `total_usage_cost_micros`。两个 Consumer 使用各自 Event Type 和幂等键，避免一个消费者失败阻塞另一个。

### 9.2 余额精度

`operator_customers` 只保留：

- `balance_micros BIGINT`
- `credit_micros BIGINT`

`operator_balance_entries` 只保留：

- `amount_micros BIGINT`
- `balance_after_micros BIGINT`
- `currency TEXT NOT NULL DEFAULT 'USD'`
- `billing_ledger_id TEXT`

删除 `balance_cents`、`credit_cents`、`amount_cents` 和 `balance_after_cents`。手工额度 API 只接受 micros，禁止不同币种进入同一钱包。

`customer_wallets/customer_billing_entries` 属于 Workspace Customer Surface，不自动并入 Operator 客户余额，但其 `gift_balance`、`profit_balance`、账单项、兑换码和代金券金额也在本次重构中统一改为 micros，仓库内不再维护第二种货币精度。

同一精度重构还必须覆盖所有客户侧金额传播点：

| 范围 | 新字段/行为 |
| --- | --- |
| Workspace Identity | `workspace_users.balance_micros` |
| 注册设置 | `default_balance_micros`，删除 `default_balance_cents` Setting Key |
| Department/Governance/API Key | `monthly_budget_micros` |
| Usage/Cost Allocation | `usage_cost_micros`、`total_usage_cost_micros`，排序与占比直接基于 micros |
| Risk/Alert | Spend 阈值、当前消耗和 Audit Metadata 使用 micros |
| Customer Notification | 余额阈值与费用摘要使用 micros + currency，禁止硬编码 `¥` |
| Platform Usage Sink | Payload 使用 `usage_cost_micros` 和 `currency` |
| Customer Billing | Wallet、Entry、Voucher、Redemption Code、充值下限均使用 micros，并带 USD 约束 |

所有相关 Go Model、Repository 查询、API DTO、TypeScript 类型、表单校验、导出字段和测试 fixture 必须同步改名。金额展示统一从 `micros + currency` 派生，不把格式化字符串作为账务输入。

本方案的 Customer Charge Outbox 保证最终一致和不重复扣费，不新增并发余额预占。若产品要求“余额不足时请求入站即拒绝”，需要独立的 `customer_balance_holds` 与授信状态机，并与 Billing Hold 原子协调；该能力不能通过读取当前 Balance 后直接判断实现，否则并发请求会超卖。

## 10. 规则选择与快照

选择输入：

```go
type RuleSelector struct {
	Purpose PricingPurpose
	PlanID  string
	Model   string
}
```

选择输出：

```go
type SelectedRule struct {
	RuleID          string
	RuleVersionID   string
	EngineVersion   int
	ExpressionHash  string
	Currency        string
}
```

规则选择与获取 Active Version 必须在一个一致性读取中完成。预占事务保存 `RuleVersionID` 后，最终结算直接按该 ID 获取版本，不重新运行选择算法。

Customer Charge 可以在预占时和 Usage Cost 一起冻结；如果请求期间客户 Plan 改变，当前请求仍使用入站时 Plan 对应版本，下一请求使用新 Plan。

### 10.1 Operator 上下文解析边界

`operator` 当前依赖 `controlplane`，所以 Control Plane 不能反向导入 Operator 类型。Runtime 注入以下窄接口：

```go
type CustomerPricingContextResolver interface {
	ResolveCustomerPricingContext(context.Context, string) (CustomerPricingContext, error)
	ValidatePricingPlan(context.Context, string) error
}
```

返回值只包含 Customer ID、冻结 Plan ID、状态和钱包币种。处理规则：

- Customer Key Admission 时解析一次并把 Plan ID 保存到 Operation/Hold Pricing Context。
- Admin/Operator 创建 `operator_plan` Scope 时调用 `ValidatePricingPlan`，不能仅接受任意字符串。
- Final Settlement 使用冻结 Plan ID，不再次读取客户当前 Plan。
- 客户 disabled、上下文缺失或解析失败时，Customer Key Admission 失效关闭。
- Resolver 不选择 Rule、不执行表达式、不返回余额；避免形成第二个计价入口。
- Memory 测试使用 fake resolver，PostgreSQL 集成测试使用真实 Operator Repository。

## 11. 事务边界

### 11.1 入站 Admission 事务

原子保存：

- AI Operation/Job；
- `usage_cost` estimate Evaluation；
- 必要时 `customer_charge` estimate Evaluation；
- Billing Hold 及两个冻结 Version ID；
- 预算预占状态。

任一受约束 Purpose 无法定价时，事务不创建 Operation。

Billing Hold 通过 `billing_hold_pricing_versions` 同时冻结两个 Purpose；不得为 Customer Charge 增加另一组平行宽表字段。

### 11.2 Final Usage 事务

原子保存：

- Usage Record；
- `usage_cost` settlement Evaluation 和 Ledger；
- 可用时 `customer_charge` settlement Evaluation 和 Ledger；
- Billing Hold 状态与 settled micros；
- Platform Usage Outbox；
- Usage Risk Outbox；
- Customer Charge Outbox。

如果 `usage_cost` 成功但 `customer_charge` 失败：

- Usage 和 usage_cost Ledger 仍需保存；
- customer_charge Evaluation 记 disputed；
- Customer Charge 不写伪金额 Ledger；
- 写入专用 reconciliation task/outbox 和告警；
- Customer Key 可按策略临时阻断后续请求，避免持续漏扣。

PostgreSQL 应在单事务内实现；Memory Repository 必须模拟相同的全有或明确部分状态契约，不能静默忽略失败。

## 12. 发布、回滚与并发

发布事务：

1. `SELECT pricing_rules ... FOR UPDATE` 或 CAS `lock_version`。
2. 验证 Draft、规则选择唯一性和当前 Active Version。
3. 分配下一 revision。
4. 把 Draft 标记为 Published，写 published actor/time。
5. 更新 `active_version_id` 和 `lock_version + 1`。
6. 写 Audit Log。

并发发布只有一个成功，另一个返回 `pricing_rule_version_conflict`，客户端必须刷新差异。

回滚与发布使用相同 CAS，不修改历史 Version。已有 Hold 不受 Active Version 指针变化影响。

## 13. 删除与保留

- Published Version、成功/争议 Evaluation 和已引用 Ledger 默认不物理删除。
- 数据保留清理可以删除超过保留期且无 Ledger/Hold 引用的 replay Evaluation。
- 规则表达式属于财务证据，至少与 Usage/Billing Ledger 保留期一致。
- Rule 禁用不级联删除 Version。
- Operator Plan 删除前必须确认没有 Customer，且没有拥有 Published Version 的 Pricing Rule Scope 引用。
- 备份恢复必须包含规则、版本、Evaluation、Ledger、Hold、Outbox 和 Operator Balance Entry。

## 14. 索引

至少建立：

```text
pricing_rules(purpose, scope_type, scope_id, model, status)
pricing_rule_versions(rule_id, revision DESC)
pricing_rule_versions(expression_hash, engine_version)
pricing_evaluations(operation_id, purpose, phase)
pricing_evaluations(usage_record_id, purpose)
pricing_evaluations(pricing_rule_version_id, created_at DESC)
billing_ledger_entries(operation_id, attempt_id, usage_version, purpose) UNIQUE
billing_ledger_entries(pricing_rule_version_id, created_at DESC)
operator_balance_entries(billing_ledger_id) UNIQUE WHERE billing_ledger_id IS NOT NULL
```

## 15. Schema 事实源

当前仓库同时维护 Repository 内联 Schema 与 `backend/migrations/*.sql`。由于研发数据库允许重建，本次直接改写现有基线 Schema：

- 从基线中删除 `model_pricings` 和 `operator_pricing_rules`。
- 直接把 Hold、Ledger、Usage、Policy、Operator、Customer Wallet 的 cents 列改成 micros。
- 在基线中加入 Pricing Rule、Version、Evaluation 和 Hold Version 关联表。
- 同步改写 `controlplane` 与 `operator` Repository 内联 Schema。
- 更新测试 fixture，只支持按新 Schema clean install，不提供旧开发数据库升级脚本。
- Schema parity 与重复初始化测试仍为必需门禁。

开发者应用该重构后必须重建本地数据库。不得通过保留旧列、运行时探测列是否存在或双写两套 Schema 来绕过重建。
