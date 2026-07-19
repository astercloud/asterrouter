# 表达式引擎设计

## 1. 设计目标

表达式引擎是纯函数：

```text
PricingRuleVersion + PricingFacts -> PricingResult | PricingError
```

相同的规则版本、事实和引擎版本必须得到完全相同的结果。执行过程不得访问当前时间、随机数、网络、数据库、全局业务配置或可变价格记录。

建议代码边界：

```text
backend/internal/pricing/
  types.go          领域输入、输出和错误
  facts.go          事实校验与 Canonical JSON
  version.go        版本前缀与 Hash
  compile.go        expr 编译、AST 校验和依赖分析
  evaluate.go       固定点 Helper 与执行
  breakdown.go      Tier 和计费行 Trace
  cache.go          有界并发缓存
  validate.go       发布验证与模拟
  *_test.go
```

该包不依赖 `controlplane`、`operator`、Gin 或 Repository。业务层通过适配器构造 `PricingFacts`。

## 2. 公开接口

建议领域接口：

```go
type Evaluator interface {
	Compile(source string) (CompiledRule, RuleAnalysis, error)
	Evaluate(compiled CompiledRule, facts PricingFacts) (PricingResult, error)
}

type PricingResult struct {
	AmountMicros  int64
	Currency      string
	MatchedTier   string
	Lines         []PricingLine
	EngineVersion int
	ExpressionHash string
	FactsHash     string
}

type PricingLine struct {
	Code         string
	Quantity     int64
	Unit         string
	UnitsPerBlock int64
	RateMicros   int64
	MultiplierBPS int64
	AmountMicros int64
}
```

`CompiledRule` 只能由引擎创建，不序列化到数据库。数据库保存表达式、引擎版本、Hash 和分析结果；进程重启后重新编译。

## 3. 金额精度

### 3.1 统一单位

V1 使用以下单位：

- 表达式最终结果：`int64` 微美元，`1 USD = 1_000_000 micros`。
- Token 单价：每百万 Token 的微美元，例如 `$3 / 1M` 表示 `3_000_000`。
- 单次价格：每个原生单位的微美元。
- 比例：基点，`10_000 bps = 1.0`，不使用 `float64`。
- 钱包、预算、账本和 API 金额字段统一使用微美元；前端只负责格式化显示。

舍入规则固定为：

1. 每个受检 Helper 以 `int64` 做溢出检测。
2. Token/比例换算使用 half-away-from-zero；V1 禁止负最终金额，因此实际为非负 half-up。
3. 表达式、持久化和 API 始终传递微美元；美元字符串只在展示层派生。
4. 不设置每请求最低 1 cent。需要最低消费时必须用条件分支选择 `fixed_line`，不能在已经产生 Trace 的 Line 外层调用 `max`。

### 3.2 受检 Helper

V1 提供：

| Helper | 签名 | 语义 |
| --- | --- | --- |
| `token_cost` | `(quantity, micros_per_1m) -> int` | Token 数乘每百万 Token 价格 |
| `unit_cost` | `(quantity, micros_per_unit) -> int` | 原生单位数量乘单价 |
| `block_cost` | `(quantity, units_per_block, micros_per_block) -> int` | 毫秒、字节等按块计价 |
| `multiply_bps` | `(amount, basis_points) -> int` | 固定点倍率 |
| `token_line` | `(code, quantity, micros_per_1m) -> int` | 计算并记录 Token 明细 |
| `adjusted_token_line` | `(code, quantity, micros_per_1m, bps) -> int` | 计算并记录含 BPS 的 Token 明细 |
| `unit_line` | `(code, quantity, unit, micros_per_unit) -> int` | 计算并记录原生单位明细 |
| `adjusted_unit_line` | `(code, quantity, unit, micros_per_unit, bps) -> int` | 计算并记录含 BPS 的原生单位明细 |
| `block_line` | `(code, quantity, unit, units_per_block, micros_per_block) -> int` | 计算并记录按块明细 |
| `adjusted_block_line` | `(code, quantity, unit, units_per_block, micros_per_block, bps) -> int` | 计算并记录含 BPS 的按块明细 |
| `fixed_line` | `(code, unit, amount_micros) -> int` | 记录固定费用或显式最低费用 |
| `tier` | `(name, amount) -> int` | 记录命中 Tier 并返回 amount |
| `min` / `max` | `(a, b) -> int` | 整数最值 |

所有 Helper 必须拒绝负数、除数为 0 和溢出。`*_line.code` 在一次执行中必须唯一；表达式只能命中一个 `tier`。Line Helper 同时完成受检计算和 Trace，调用者不能传入一个与数量/单价不一致的 amount。纯 `*_cost` Helper 只允许用于条件或选择 Line 分支，最终金额分支必须由 Line Helper 和整数 0 组成。没有 `tier` 的平价规则自动标记为 `base`，复杂条件规则要求显式 Tier。

## 4. PricingFacts V1

### 4.1 Token 事实

| 标识符 | 类型 | 语义 |
| --- | --- | --- |
| `total_input_tokens` | int | 完整输入上下文 Token；条件判断使用该值 |
| `uncached_input_tokens` | int | 已规范化的非缓存输入 Token |
| `cache_read_tokens` | int | 缓存读取 Token |
| `cache_write_5m_tokens` | int | 5 分钟或通用缓存写入 Token |
| `cache_write_1h_tokens` | int | 1 小时缓存写入 Token |
| `output_tokens` | int | 文本输出 Token |
| `cache_fields_present` | bool | Provider 是否明确提供缓存字段，而不是值是否非零 |

约束：

- Provider 没有缓存字段时，Normalizer 设置 `cache_fields_present=false`、`uncached_input_tokens=total_input_tokens`，缓存数量为 0。
- Provider 有缓存字段时，Normalizer 必须验证各输入分量与总输入的一致性，并设置明确的 normalization status。
- 引擎不根据表达式 UsedVars 自动修改任何 Token。

### 4.2 通用 Usage Dimension

以下现有维度以同名 `int` 变量暴露：

- `input_images`
- `output_images`
- `partial_images`
- `input_video_milliseconds`
- `output_video_milliseconds`
- `input_audio_milliseconds`
- `output_audio_milliseconds`
- `realtime_audio_milliseconds`
- `input_characters`
- `actions`
- `batch_items`
- `input_bytes`
- `output_bytes`
- `transfer_bytes`
- `session_milliseconds`

新增维度必须通过新的表达式引擎版本暴露。V1 环境不能因代码新增字段而隐式改变。

### 4.3 请求属性

V1 只暴露 Canonical Request 已有或明确新增的白名单属性：

| 标识符 | 类型 | 来源 |
| --- | --- | --- |
| `protocol` | string | `gatewaycore.CanonicalRequest.Protocol` |
| `operation` | string | Canonical operation |
| `modality` | string | text/image/audio/video/realtime |
| `lane` | string | direct/durable |
| `stream` | bool | 是否流式 |
| `output_count` | int | 请求输出数量 |
| `service_tier` | string | 协议 Adapter 规范化后的白名单值；未实现前固定为空 |

禁止暴露：

- 原始请求 Body、JSONPath 和消息内容；
- 任意 Header；
- API Key、用户、租户、Secret、Provider Account；
- 原始时间函数；
- Source IP 和 Sticky Key。

若未来确需按客户或 Plan 区分价格，应通过规则选择 Scope 完成，而不是把身份信息放入表达式。

### 4.4 事实可用性

`PricingFacts` 同时保存：

- `Phase`：`estimate | settlement | replay`；V1 表达式不可读取 Phase；
- `ObservedAt`：冻结时间，仅用于审计；V1 表达式不可读取；
- `AvailableFacts`：明确哪些维度来自请求估算、Provider 报告或对账；
- `NormalizationStatus`；
- `FactsHash`：规范化 JSON 的 SHA-256。

规则分析结果保存 `required_facts`。运行前缺少必需事实时返回 `pricing_fact_missing`，不能把未知值默认为免费。

## 5. V1 表达式

### 5.1 平价 Token 规则

```text
v1:tier("base",
  token_line("input", uncached_input_tokens, 3_000_000)
  + token_line("output", output_tokens, 15_000_000)
)
```

金额结果单位为微美元。

### 5.2 Claude 长上下文与缓存

```text
v1:total_input_tokens <= 200_000
  ? tier("standard",
      token_line("input", uncached_input_tokens, 3_000_000)
      + token_line("cache_read", cache_read_tokens, 300_000)
      + token_line("cache_write_5m", cache_write_5m_tokens, 3_750_000)
      + token_line("cache_write_1h", cache_write_1h_tokens, 6_000_000)
      + token_line("output", output_tokens, 15_000_000))
  : tier("long_context",
      token_line("input", uncached_input_tokens, 6_000_000)
      + token_line("cache_read", cache_read_tokens, 600_000)
      + token_line("cache_write_5m", cache_write_5m_tokens, 7_500_000)
      + token_line("cache_write_1h", cache_write_1h_tokens, 12_000_000)
      + token_line("output", output_tokens, 22_500_000))
```

### 5.3 图片按张计费

```text
v1:tier("image",
  unit_line("output_images", output_images, "count", 40_000)
)
```

### 5.4 固定请求费与白名单属性

```text
v1:service_tier == "priority"
  ? tier("priority",
      fixed_line("request", "request", 20_000)
      + adjusted_token_line("input", uncached_input_tokens, 5_000_000, 20_000))
  : tier("standard",
      token_line("input", uncached_input_tokens, 5_000_000))
```

只有 Adapter 已生成 `service_tier` Canonical Fact 后，该变量才允许被规则引用。

### 5.5 显式最低消费

```text
v1:token_cost(uncached_input_tokens, 3_000_000)
    + token_cost(output_tokens, 15_000_000) < 10_000
  ? tier("minimum", fixed_line("minimum", "request", 10_000))
  : tier("base",
      token_line("input", uncached_input_tokens, 3_000_000)
      + token_line("output", output_tokens, 15_000_000))
```

`min/max` 和纯 Cost Helper 不能包裹已经执行的 Line Helper，否则未被最终选择的 Line 也会进入 Trace。AST 数据流校验必须拒绝这种写法。

## 6. 语法与 AST 白名单

V1 允许：

- 已登记标识符；
- int、bool、string 字面量；
- `+`；
- `== != < <= > >=`；
- `&& || !`；
- 三元条件；
- 已登记 Helper 调用；
- 仅用于条件分组的括号。

V1 禁止：

- 浮点字面量；
- 原始 `* / %`，所有比例和除法必须走受检 Helper；
- 任意负金额和无约束减法；
- 数组、Map、成员访问、动态索引、闭包和高阶函数；
- expr 内置的未登记函数；
- 动态函数名或动态维度名；
- `header`、`param`、`time.Now`、时区加载和随机数。

建议限制：

| 限制 | V1 值 |
| --- | --- |
| 表达式 UTF-8 长度 | 8 KiB |
| AST 节点数 | 256 |
| AST 最大深度 | 32 |
| Tier 数 | 16 |
| 单次命中计费行数 | 32 |
| 字符串字面量 | 128 bytes |
| 编译超时 | 由发布请求 Context 控制，默认 2 秒 |

表达式语言不包含循环，但仍需限制表达式体积和嵌套，避免管理员配置造成 CPU/内存放大。

## 7. 编译流程

```text
source
  -> 解析显式版本
  -> 拒绝未知版本
  -> 长度检查
  -> expr.Compile + 强制 int64 返回
  -> AST 节点/深度/操作符/函数白名单
  -> 提取 required_facts、tiers、line codes
  -> 静态检查重复 line、最终金额数据流、不可达/缺失 tier 的可识别模式
  -> 生成 RuleAnalysis
  -> 对 Canonical Source 计算 SHA-256
```

Hash 规则：

- Hash 由服务端对精确保存的 UTF-8 Source 计算。
- `CompileByHash` 必须重新计算并比较，不接受“调用者已算好所以跳过验证”。
- 缓存键为 `engine_version + expression_hash`。
- 发布后表达式不可修改；修改产生新 Version ID 和新 Hash。

`RuleAnalysis` 至少包括：

```json
{
  "engine_version": 1,
  "required_facts": ["uncached_input_tokens", "output_tokens"],
  "tiers": [
    {"name": "standard", "conditions": ["total_input_tokens <= 200000"]},
    {"name": "long_context", "conditions": ["total_input_tokens > 200000"]}
  ],
  "line_codes": ["input", "output"],
  "visual_editable": true
}
```

前端只消费该分析结果，不使用正则表达式重新解释完整 DSL。

## 8. 编译缓存

要求：

- 内容寻址，有界 LRU，初始上限 512 个规则版本；上限配置只影响性能。
- 缓存 Entry 完全不可变；UsedFacts、Tier 和分析 Map 返回副本或只读结构。
- 使用 singleflight 合并同一 Hash 的并发编译。
- 达到上限逐项淘汰，不整体清空缓存。
- 发布规则无需调用全局 `InvalidateCache`；新 Source 天然产生新 Hash。
- 缓存 miss 或进程重启必须从数据库版本重新编译并得到相同结果。
- `go test -race` 覆盖读取、并发编译、淘汰和重建。

## 9. 执行流程

```text
验证 version/hash/currency
  -> 验证 facts 和 required_facts
  -> 获取或编译 Program
  -> 为本次执行创建独立 Trace Collector
  -> expr.Run(program, immutable env)
  -> 验证 int64、非负、上限、唯一 Tier、唯一 Line
  -> 验证 Line Helper 的受检数学和 sum(lines) == result
  -> 生成 deterministic PricingResult
```

每次执行的 Trace Collector 独立创建，不存入编译缓存。`PricingResult.Lines` 按表达式实际执行顺序输出，同时生成稳定排序后的 Canonical JSON 用于 Hash。

金额上限由业务层提供，例如不得超过 `math.MaxInt64` 且不得超过配置的单请求安全上限。安全上限不是免费 fallback；越界返回 `pricing_amount_out_of_range`。

## 10. 发布验证

发布必须通过以下层级：

1. **静态验证：** 版本、语法、AST、事实、Helper、长度、Hash。
2. **内置向量：** 0、小值、常用值、Tier 边界、边界 ±1、大值。
3. **管理员样例：** 草稿中保存的命名测试用例及期望 Tier/金额。
4. **性质验证：** 对受支持的可视化规则检查非负、确定性和无溢出；不声称证明任意 Raw 公式单调。
5. **选择冲突：** 发布后不能与相同 Purpose/Scope/Model 的 active Rule 冲突。
6. **币种门禁：** V1 Published Rule 只接受 USD。

Smoke Vector 只能发现问题，不能替代运行时检查。

## 11. 模拟与重放

模拟支持两种输入：

- 管理员手工填写规范化事实；
- 从某条 Usage Record 复制非敏感事实，并明确显示其 normalization status。

模拟默认不持久化。带 `persist_evidence=true` 的发布审批模拟可以保存为审计证据，但不能写 Usage Ledger 或余额。

重放必须使用原 Evaluation 的：

- `pricing_rule_version_id`；
- `engine_version`；
- Canonical Facts；
- Expression Hash；
- Facts Hash。

重放结果不一致是 P0 事件，必须阻断发布。

## 12. 稳定错误码

| 错误码 | 含义 |
| --- | --- |
| `pricing_expr_empty` | 表达式为空 |
| `pricing_expr_too_large` | 超出体积限制 |
| `pricing_version_unsupported` | 未知引擎版本 |
| `pricing_expr_compile_failed` | expr 语法或类型错误 |
| `pricing_expr_forbidden_ast` | 使用了未允许节点、操作符或函数 |
| `pricing_expr_hash_mismatch` | Source 与 Hash 不一致 |
| `pricing_fact_missing` | 必需事实未知 |
| `pricing_fact_invalid` | 事实为负、冲突或归一化失败 |
| `pricing_arithmetic_overflow` | 受检数学溢出 |
| `pricing_amount_out_of_range` | 最终金额越界 |
| `pricing_tier_invalid` | Tier 缺失、重复或非法 |
| `pricing_breakdown_invalid` | 明细重复或合计不一致 |
| `pricing_rule_unavailable` | 选择不到可用规则 |

API 返回错误位置时只包含行列、节点类型和安全描述，不回显请求事实中的敏感数据。

## 13. 版本演进

- 无前缀表达式不允许发布。
- V1 环境发布后冻结。新增事实、Helper 或舍入语义必须进入 V2。
- 未知版本失效关闭，不能走默认分支当作 V1。
- V2 不在本方案预留运行时兼容分支；需要时单独设计并一次性切换。
