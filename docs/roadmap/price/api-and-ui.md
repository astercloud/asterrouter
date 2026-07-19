# API 与管理界面设计

## 1. 信息架构

Admin、Platform 与 Operator 使用各自权限 Surface，但共享规则编辑与模拟组件：

| Surface | 路由 | 默认 Purpose | 可管理 Scope |
| --- | --- | --- | --- |
| Admin | `/admin/pricing` | `usage_cost` | global |
| Platform | `/platform/pricing` | `usage_cost` | global |
| Operator | `/operator/pricing` | `customer_charge` | global、operator_plan |

页面不是两套定价实现：

- API 都调用 Control Plane 的 Pricing Rule Service。
- 前端复用 `PricingRuleTable`、`PricingRuleEditor`、`PricingSimulator`、`PricingVersionHistory`。
- Surface 只决定权限、Purpose、Scope 选项和发布确认文案。
- 供应商采购价继续留在 `/admin/effective-pricing`，不进入本页面。

## 2. Admin/Platform API

新增 `/api/v1/admin/pricing-rules` 和 `/api/v1/platform/pricing-rules`，两者共享 Handler 与 Service，依靠现有 Surface 鉴权隔离：

| 方法 | 路径 | 语义 |
| --- | --- | --- |
| `GET` | `/pricing-rules` | 按 purpose/status/model/scope 查询 Rule Head 与 Active Version 摘要 |
| `POST` | `/pricing-rules` | 创建不可变选择槽位和首个 Draft |
| `GET` | `/pricing-rules/:id` | Rule、Active Version、Draft 与引用摘要 |
| `PUT` | `/pricing-rules/:id/draft` | 保存草稿，要求 `expected_lock_version` |
| `POST` | `/pricing-rules/validate` | 无持久化编译、分析和样例验证 |
| `POST` | `/pricing-rules/simulate` | 对 Draft/Source/Published Version 执行非持久模拟 |
| `POST` | `/pricing-rules/:id/publish` | 编译、验证、发布并激活新版本 |
| `POST` | `/pricing-rules/:id/activate/:version_id` | CAS 回滚/重新激活历史 Published Version |
| `POST` | `/pricing-rules/:id/disable` | 禁用新请求选择，不影响历史 Hold |
| `GET` | `/pricing-rules/:id/versions` | 版本历史与引用计数 |
| `GET` | `/pricing-evaluations/:id` | 查看脱敏事实、Tier 和计费行 |
| `POST` | `/pricing-evaluations/:id/replay` | 管理员重放并保存对比证据 |

Operator API 使用 `/api/v1/operator/pricing-rules` 提供相同核心能力，但服务端固定 `purpose=customer_charge`，并校验 Plan Scope 权限。不能通过请求 Payload 越权创建 `usage_cost` Rule。

## 3. DTO

### 3.1 Rule 摘要

```json
{
  "id": "prule_01...",
  "name": "Claude Sonnet internal cost",
  "purpose": "usage_cost",
  "scope_type": "global",
  "scope_id": "",
  "model": "claude-sonnet",
  "status": "active",
  "lock_version": 4,
  "active_version": {
    "id": "pver_01...",
    "revision": 3,
    "engine_version": 1,
    "currency": "USD",
    "expression_hash": "...",
    "tiers": ["standard", "long_context"],
    "authoring_mode": "visual",
    "published_at": "2026-07-16T10:00:00Z",
    "published_by": "admin@example"
  }
}
```

响应不在列表中返回完整表达式或 Facts，降低误曝光和 Payload 体积。详情接口才返回 Source。

### 3.2 创建 Rule

```json
{
  "name": "Claude Sonnet customer price",
  "purpose": "customer_charge",
  "scope_type": "operator_plan",
  "scope_id": "plan_enterprise",
  "model": "claude-sonnet",
  "currency": "USD",
  "authoring_mode": "visual",
  "expression": "v1:tier(...)"
}
```

Admin Surface 允许显式 Purpose；Operator Surface 忽略/拒绝非 `customer_charge` Purpose。

### 3.3 Validate

请求：

```json
{
  "engine_version": 1,
  "currency": "USD",
  "expression": "v1:...",
  "test_cases": [
    {
      "name": "200k boundary",
      "facts": {
        "total_input_tokens": 200000,
        "uncached_input_tokens": 200000,
        "output_tokens": 1000
      },
      "expected_tier": "standard",
      "expected_amount_micros": 615000
    }
  ]
}
```

响应：

```json
{
  "valid": true,
  "expression_hash": "...",
  "analysis": {
    "required_facts": ["total_input_tokens", "uncached_input_tokens", "output_tokens"],
    "tiers": [
      {"name": "standard", "conditions": ["total_input_tokens <= 200000"]},
      {"name": "long_context", "conditions": ["total_input_tokens > 200000"]}
    ],
    "line_codes": ["input", "output"],
    "visual_editable": true
  },
  "test_results": [
    {"name": "200k boundary", "passed": true, "amount_micros": 615000, "tier": "standard"}
  ],
  "warnings": []
}
```

验证失败响应使用稳定错误码、行列和安全描述：

```json
{
  "valid": false,
  "errors": [
    {
      "code": "pricing_expr_forbidden_ast",
      "line": 4,
      "column": 9,
      "message": "operator '*' is not allowed; use a checked pricing helper"
    }
  ]
}
```

### 3.4 Simulate

```json
{
  "rule_version_id": "pver_01...",
  "facts": {
    "total_input_tokens": 210000,
    "uncached_input_tokens": 190000,
    "cache_read_tokens": 20000,
    "cache_fields_present": true,
    "output_tokens": 1000,
    "protocol": "anthropic_messages",
    "modality": "text",
    "lane": "direct"
  }
}
```

响应：

```json
{
  "amount_micros": 1174500,
  "amount_display": "$1.174500",
  "currency": "USD",
  "matched_tier": "long_context",
  "expression_hash": "...",
  "facts_hash": "...",
  "lines": [
    {
      "code": "input",
      "quantity": 190000,
      "unit": "token",
      "units_per_block": 1000000,
      "rate_micros": 6000000,
      "multiplier_bps": 10000,
      "amount_micros": 1140000
    }
  ]
}
```

所有金额字段只接受 `*_micros`；出现 `amount_cents` 或其他 cents 字段按未知字段拒绝。

### 3.5 Publish

```json
{
  "draft_version_id": "pdraft_01...",
  "expected_lock_version": 4,
  "expected_active_version_id": "pver_previous",
  "confirmation": {
    "expression_hash": "...",
    "acknowledge_customer_impact": true
  }
}
```

发布响应返回新 Revision、Hash、审计 ID 和 activation time。并发冲突返回 HTTP 409，不自动覆盖。

## 4. 权限与审计

### 4.1 权限

| 操作 | Admin | Platform | Operator | Auditor |
| --- | --- | --- | --- | --- |
| 读取 usage_cost | 全局计费管理员 | Platform 管理员 | 否 | 只读 |
| 发布 usage_cost | 全局计费管理员 | Platform 管理员 | 否 | 否 |
| 读取 customer_charge | 全局计费管理员 | 否 | Operator 授权范围 | 只读 |
| 发布 customer_charge | 全局计费管理员 | 否 | Operator 计费管理员 | 否 |
| 模拟 | 与读取权限一致 | 与读取权限一致 | 与读取权限一致 | 可模拟 Published，不可模拟未发布 Draft |
| 重放 Evaluation | 全局计费管理员 | 仅 usage_cost | 仅自身 Purpose/范围 | 只读查看结果 |

沿用现有 Admin、Platform、Operator Surface 门禁。跨 Surface、跨 Plan 或未知 Rule 统一返回不存在语义，防止枚举。

### 4.2 审计事件

- `pricing_rule.create`
- `pricing_rule.draft_update`
- `pricing_rule.validate`
- `pricing_rule.publish`
- `pricing_rule.activate_version`
- `pricing_rule.disable`
- `pricing_rule.simulate_persisted`
- `pricing_evaluation.replay`
- `pricing_evaluation.disputed`
- `pricing_charge.delivery_dead_letter`

发布审计必须保存：

- Rule 选择槽位；
- 前后 Version ID 和 Hash；
- Actor；
- Validation Summary；
- 影响范围估算，如近 24 小时命中请求数；
- 不保存请求 Prompt、Header 或 Body。

## 5. 直接替换旧接口

同一重构直接执行：

- 删除所有 Surface 的 `*/model-pricings` API 注册及 `ModelPricing`、`ModelPricingRequest` DTO。
- 删除前端 `/admin/model-pricings` 路由，新的 Admin 规则页面只注册为 `/admin/pricing`。
- 删除前端 `/platform/model-pricings` 路由，Platform 规则页面只注册为 `/platform/pricing`。
- Operator 的新规则资源使用 `/api/v1/operator/pricing-rules`，契约只包含 Rule/Draft/Version，不接受输入/输出 cents 或 `rate_multiplier`。
- 删除前端旧 `ModelPricing`、`OperatorPricingRule` 类型和对应 API Client。
- 删除服务端旧错误码分支、字段投影和简单平价特殊逻辑。
- 未同步升级的前端或调用方直接失败，不提供 Deprecated Header、旧字段投影或自动转换。

## 6. 页面结构

### 6.1 规则列表

桌面布局：

```text
┌─────────────────────────────────────────────────────────────────────┐
│ 表达式计费                                       [新建规则] [刷新] │
├─────────────────────────────────────────────────────────────────────┤
│ Purpose [usage_cost]  Scope [全部]  Status [active]  [搜索模型...] │
├─────────────────────────────────────────────────────────────────────┤
│ 模型        范围       模式       当前版本   Tier   状态     操作  │
│ claude...   Global     Expression v3           2     Active   ...   │
│ gpt-...     Global     Expression v1           1     Active   ...   │
└─────────────────────────────────────────────────────────────────────┘
```

规则行展示：

- Gateway Model；
- Purpose 与 Scope/Plan；
- Active Revision、Hash 短前缀和发布时间；
- Tier 数、计费维度摘要和币种；
- Draft 是否存在；
- 状态与操作菜单。

不在列表中直接展示完整表达式。

### 6.2 编辑器

编辑器使用全页工作面，不塞进现有小型 CRUD Modal。顶部固定显示 Rule 选择槽位，这些字段创建后只读。

视图使用 Tabs：

1. **可视化：** Tier 条件、各计费维度价格和固定费用。
2. **表达式：** Monaco/轻量代码编辑器、只读变量和 Helper 参考、行列错误。
3. **测试用例：** 命名 Facts、预期 Tier、预期金额。
4. **模拟：** 手工 Facts 或选择历史 Usage，显示明细。
5. **版本：** Hash、Actor、时间、Diff、引用数量和回滚。

可视化模式只编辑后端 `analysis.visual_editable=true` 的规则。Raw 表达式不符合可视化子集时：

- 保持 Raw Source 不变；
- 禁止切换后自动重写；
- 可视化 Tab 显示只读分析和“此表达式仅支持原始模式”；
- 用户可显式执行“转换为可视化模板”，该动作生成新 Draft 并展示 Diff。

### 6.3 可视化 Tier 编辑

每个 Tier 是一个连续表格区段，不使用嵌套卡片：

```text
Tier: standard
条件: total_input_tokens <= [200000]

计费项             单价             单位             启用
非缓存输入          [$3.000000]      / 1M tokens      [x]
缓存读取            [$0.300000]      / 1M tokens      [x]
缓存写入 5m         [$3.750000]      / 1M tokens      [x]
缓存写入 1h         [$6.000000]      / 1M tokens      [x]
输出                [$15.000000]     / 1M tokens      [x]
```

行为约束：

- Tier 顺序和边界由后端验证，UI 即时提示空洞、重叠和不可达条件。
- 价格输入使用十进制定点字符串，提交前转换为微美元整数。
- 维度启用后必须填写价格，显式免费填写 0。
- 视觉编辑器生成表达式后立即调用 Validate，不依赖前端正则判断正确性。

### 6.4 模拟器

模拟器并列展示：

- 规范化 Facts；
- 命中 Rule/Version/Tier；
- 每条计费行的数量、单价和金额；
- 总微美元和友好金额；
- 与当前 Active Version 的差异。

从历史 Usage 导入时只读取白名单 Facts。页面明确标识 `reported/observed/estimated/reconciled` 和 normalization status，不展示 Prompt 或 Provider Secret。

### 6.5 发布与回滚

发布按钮打开确认对话框，展示：

- Rule Purpose/Scope/Model；
- 当前与新 Version；
- 表达式 Hash；
- Tier、维度和价格 Diff；
- 测试用例结果；
- 近 24 小时预计命中请求数量；
- 对客户扣费有影响时的明确确认复选框。

回滚同样是发布级操作，必须选择历史 Version、查看 Diff 并确认。回滚不删除当前 Version。

## 7. 状态与错误体验

- Loading：表格骨架保持列宽，不改变布局。
- Empty：提供新建 Rule 命令，不显示营销说明。
- Validation Error：定位到表达式行列或可视化字段。
- Conflict：保留本地 Draft，加载服务器新版本并提供 Diff，不能静默覆盖。
- Publish Failed：Draft 保留，显示稳定错误码和 Audit Reference。
- Disputed Evaluation：在规则列表和 Usage 详情显示告警入口，可跳转到证据页。
- Read-only：Auditor 和历史 Published Version 的输入控件不可编辑。

## 8. 响应式与可访问性

- 桌面表格在窄屏切换为按字段分组的列表行，不横向压缩长表达式。
- 编辑器顶部操作在移动端换行，发布命令保持可见且不遮挡 Tabs。
- 所有输入有显式 Label、错误关联和单位，不只依赖 Placeholder。
- Tier、状态和 Diff 不能只用颜色表达。
- 表格、Tabs、对话框、菜单和模拟器支持键盘操作与焦点恢复。
- 表达式错误通过 `aria-describedby` 关联行列摘要。
- 中英文文案、明暗主题、桌面和移动视口进入组件/E2E 测试。

## 9. 前端文件建议

```text
frontend/src/features/pricing/
  api.ts
  types.ts
  money.ts
  PricingRuleTable.vue
  PricingRuleEditor.vue
  PricingTierEditor.vue
  PricingExpressionEditor.vue
  PricingTestCases.vue
  PricingSimulator.vue
  PricingVersionHistory.vue
  PricingPublishDialog.vue
  *.test.ts

frontend/src/views/admin/AdminPricingView.vue
frontend/src/views/operator/OperatorPricingView.vue
```

Platform 与 Admin 复用 `AdminPricingView.vue`，由 Route Loader 注入 Surface API Base。共享组件不得依赖具体 Surface 路由；权限、Purpose 和 Scope 由 Props/Route Loader 注入。

## 10. 与 new-api 前端的取舍

吸收：

- 可视化和 Raw 两种编辑方式；
- Tier 预设、变量分组、请求模拟和价格明细；
- Raw 表达式不能无损解析时保留原文并退出可视编辑；
- 保存前预览最终规则。

不采用：

- 前端用正则维护第二套完整表达式解析器；
- `|||` 或字符串拆分维护独立请求倍率语法；
- 任意 Header、Param、时区和当前时间进入 V1 UI；
- 前端自行决定表达式是否合法；
- 把表达式 Base64 塞进日志作为主要审计证据。

后端 AST 分析结果是 UI 的结构化事实，表达式与 Version ID 是账务事实。
