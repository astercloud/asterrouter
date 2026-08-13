# 路由策略算法验收矩阵

> 状态：`CURRENT`
> 适用版本：`v0.24.0`
> 事实源：[`README.md`](./README.md)、[`scenario-registry.json`](./scenario-registry.json)
> 产品边界：企业 AI Gateway 的静态策略、运行时调度、路由试算与执行证据

## 1. 决策链路

路由不是单一排序函数。一次企业请求按以下顺序收敛：

1. 鉴权与访问策略检查模型、操作、模态、额度和预算。
2. Planner 解析网关模型和路由组，选择生效中的路由策略。
3. 静态策略应用模型/协议准入、原生协议、资源批次、四种偏好和价格护栏。
4. 真实 Gateway 按应用、主体和凭据生成有效成本 cohort，再在当前资源批次内应用已批准的动态优化。
5. 真实 Gateway 应用会话粘性；粘性作用域同时包含访问策略版本和路由策略 ID/版本。
6. 调度器逐候选申请熔断、并发、RPM 和 TPM 许可，只允许首字节前故障切换。
7. Trace 保存最终线路、策略标识、策略版本、偏好、淘汰原因和每次尝试结果。

试算器与 Planner 共用第 3 步的硬约束，并只读投影当前容量。试算器不持有最终应用、主体、凭据、sticky key 或有效成本 cohort，因此不能代替第 4 至第 6 步的真实 Gateway 证据。

## 2. 验收矩阵

| 维度 | 预期合同 | 直接证据 | 证明层 | 状态 |
| --- | --- | --- | --- | --- |
| 策略标识与版本 | 创建为 v1；更新后递增；Planner、Simulator 和 Trace 暴露实际策略 ID、版本与偏好 | `backend/internal/controlplane/service_test.go#TestRoutingPolicyLifecycleNormalizesStrategyAndVersionsUpdates`；`backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicySimulatorMatchesPlannerHardConstraints`；`backend/internal/server/gateway_routes_test.go#TestRoutingPolicyUpdateInvalidatesStickyGatewaySelection` | L1/L2 | 通过 |
| 模型准入 | allowlist 之外或 denylist 命中的基础/限定模型均阻断，Planner 与 Simulator 原因一致 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyModelScopeAndPriceGuardrails`；`backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicySimulatorMatchesPlannerHardConstraints` | L1/L2 | 通过 |
| 协议准入 | 13 种受支持协议可配置；未知协议拒绝；deny 优先于 allow；Planner 与 Simulator 原因一致 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyProtocolMatrixIsCompleteAndRejectsUnknownValues`；`backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyProtocolAdmissionAndNativeProtocolAreEnforcedByPlanner`；`backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicySimulatorMatchesPlannerHardConstraints` | L1/L2 | 通过 |
| 原生协议 | `native_protocol_only` 仅保留无需跨协议转换的上游格式 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyProtocolAdmissionAndNativeProtocolAreEnforcedByPlanner` | L2 | 通过 |
| 四种偏好 | 成本优先、速度优先、稳定优先、综合均衡在信号冲突时均确定性选中对应线路 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyPresetsResolveConflictingSignalsDeterministically` | L2 | 通过 |
| 输入价格绝对上限 | 超限候选淘汰并记录 `routing_policy_input_price_exceeded` | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyPriceExclusionsRemainExplainable` | L1 | 通过 |
| 输出价格绝对上限 | 超限候选淘汰并记录 `routing_policy_output_price_exceeded` | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyPriceExclusionsRemainExplainable` | L1 | 通过 |
| 相对最低价 | 每个资源批次独立计算最低价；超过倍数的同批次候选淘汰并记录 `routing_policy_relative_price_exceeded`；更便宜或零价的备用批次不得提前淘汰主批次 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyPriceExclusionsRemainExplainable`；`backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyOrdersBatchesAndControlsFailover`；`frontend/e2e/routing-policy.spec.ts#@e2e-routing-policy-001` | L1/L2/Gate A | 通过 |
| 价格事实缺失 | 部分候选有价格事实时，缺失候选记录 `routing_policy_price_fact_missing` | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyPriceExclusionsRemainExplainable` | L1 | 通过 |
| 低价池 | 自动模式使用归一化比例与最少候选；严格模式只保留最低价；成本优先在池内按价格排序 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyAutomaticLowPricePoolUsesNormalizedDefaults` | L1 | 通过 |
| 有序资源批次 | 只使用策略列出的账号并保持批次顺序；粘性和动态有效成本均不得跨越当前批次 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyOrdersBatchesAndControlsFailover`；`backend/internal/controlplane/effective_pricing_service_test.go#TestEffectivePricingDecisionCanaryOrdersCandidateAndRollbackStopsIt` | L1/L2 | 通过 |
| 首字节前故障切换 | 关闭时只尝试首选线路；开启时首选失败后尝试备用线路；Trace 区分 excluded、failed、selected | `backend/internal/server/gateway_routes_test.go#TestRoutingPolicyFailoverToggleControlsRealGatewayAttempts`；`backend/internal/server/gateway_routes_test.go#TestGatewayStreamingFallsBackBeforeFirstClientEvent` | L2 | 通过 |
| 首字节后禁止切换 | 流已经向客户端输出后发生断连，不调用备用线路，避免重复响应 | `backend/internal/server/gateway_routes_test.go#TestGatewayStreamingInterruptionRecordsErrorWithoutUnsafeFailover` | L2 | 通过 |
| 粘性路由 | 同一作用域优先复用账号；新会话可复用供应商；不同客户和路由组隔离；TTL 到期失效 | `backend/internal/controlplane/gateway_scheduler_test.go#TestGatewayCandidateAffinityReusesAccountThenSupplierWithinScope` | L1 | 通过 |
| 策略版本使粘性失效 | 路由策略从 v1 更新到 v2 后，不得复用旧绑定，真实请求按 v2 重新选择 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyVersionInvalidatesAffinityScope`；`backend/internal/server/gateway_routes_test.go#TestRoutingPolicyUpdateInvalidatesStickyGatewaySelection` | L1/L2 | 通过 |
| 动态有效成本 | 只在 `smart_optimization=true` 且决策为 canary/active 时生效；不跨批次；回滚立即停止；真实 HTTP 请求和 Trace 使用被提升线路 | `backend/internal/controlplane/effective_pricing_service_test.go#TestEffectivePricingDecisionCanaryOrdersCandidateAndRollbackStopsIt`；`backend/internal/server/gateway_routes_test.go#TestRoutingPolicySmartOptimizationReordersRealGatewayRequest` | L1/L2 | 通过 |
| cohort 稳定与隔离 | 同一客户跨会话 cohort 稳定且不暴露原始身份；不同客户隔离；抽样比例近似配置值 | `backend/internal/controlplane/gateway_scheduler_test.go#TestGatewayCandidateAffinityReusesAccountThenSupplierWithinScope`；`backend/internal/controlplane/effective_pricing_service_test.go#TestEffectivePricingCanaryUsesStableCohortDistribution` | L1 | 通过 |
| 路由/账号/供应商状态 | 停用路由、停用账号、停用供应商、账号过期、账号冷却和熔断打开均被 Planner 与 Simulator 淘汰并给出精确原因 | `backend/internal/controlplane/gateway_pipeline_test.go#TestPlanCanonicalGatewayRequestRecordsCandidateExclusions` | L2 | 通过 |
| 熔断恢复 | 达阈值后打开；到期进入半开；只允许一个探测请求；成功后恢复关闭 | `backend/internal/controlplane/gateway_scheduler_test.go#TestProviderAccountCircuitOpensAndHalfOpenProbeIsExclusive` | L1 | 通过 |
| 账号容量 | 达并发上限时不拨号繁忙线路，转到可用线路，并在 Trace 标记 skipped | `backend/internal/server/gateway_routes_test.go#TestGatewayChatCompletionSkipsAccountAtConcurrencyCapacity` | L2 | 通过 |
| RPM/TPM | 许可按滑动窗口拒绝超额请求，明确返回 `rpm_exhausted` 或 `tpm_exhausted` | `backend/internal/controlplane/gateway_scheduler_test.go#TestProviderAccountPermitEnforcesRPMAndTPM` | L1 | 通过 |
| 账单健康阻断 | 仅生效中的强账单证据可阻断；无效密钥和耗尽的 key quota 不进入候选；不健康线路不得被有效成本提升 | `backend/internal/controlplane/provider_billing_routing_health_test.go#TestGatewayRoutingUsesOnlyActiveHardBillingEvidence`；`backend/internal/controlplane/provider_billing_routing_health_test.go#TestGatewayRoutingFiltersExhaustedKeyQuota`；`backend/internal/controlplane/provider_billing_routing_health_test.go#TestEffectivePricingDoesNotPromoteBillingUnhealthyCandidate` | L1/L2 | 通过 |
| 企业预算阻断 | 预算 hold 失败时请求不访问上游，返回 402，并记录 Usage 与 Trace | `backend/internal/server/gateway_routes_test.go#TestGatewayChatCompletionEnforcesWorkspaceKeyBudgetAndRecordsTrace` | L2 | 通过 |
| Planner/Simulator 一致性 | 共用原生协议、价格、低价池和故障切换裁剪；模型/协议全局阻断结果一致 | `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicySimulatorMatchesPlannerHardConstraints`；`backend/internal/controlplane/gateway_pipeline_test.go#TestPlanCanonicalGatewayRequestRecordsCandidateExclusions` | L2 | 通过 |
| Simulator 无副作用 | 连续试算不消耗 RPM/TPM 或并发许可；不可用候选仍以原因投影 | `backend/internal/controlplane/gateway_scheduler_test.go#TestGatewaySimulationDoesNotConsumeRateCapacity`；`backend/internal/controlplane/gateway_scheduler_test.go#TestGatewaySimulationIncludesSkippedCircuitCandidate` | L1/L2 | 通过 |
| Trace/attempt/exclusion | 最终 Trace 包含策略 ID、版本、偏好和动态决策；尝试列表包含策略淘汰、调度跳过、上游失败及最终选择 | `backend/internal/server/gateway_routes_test.go#TestRoutingPolicyFailoverToggleControlsRealGatewayAttempts`；`backend/internal/server/gateway_routes_test.go#TestRoutingPolicySmartOptimizationReordersRealGatewayRequest`；`backend/internal/server/gateway_contract_test.go#TestGatewayTraceIncludesPlannerExclusionEvidence` | L2 | 通过 |
| 浏览器策略闭环 | 可见控件创建 v1、更新 v2、刷新持久化、运行试算；三视口、中英文、13 种协议、策略偏好和价格淘汰原因可见且无横向溢出 | `frontend/e2e/routing-policy.spec.ts#@e2e-routing-policy-001` | Gate A | 通过 |

## 3. 已知边界与产品决策

### 3.1 完全缺少可比 USD 价格事实

当前行为以资源批次为边界：即使同协议或备用批次存在其他价格，只要当前批次完全没有可比 USD 事实，就保留该批次原候选顺序，不因企业尚未导入采购价而中断 Gateway。这个默认值保证首次部署可用，但管理员目前不能显式选择以下治理方式：

- 强制阻断无价格候选；
- 只允许已定价候选并在不足时降级；
- 保留候选但产生告警。

在产品决策确定前，不能把当前行为描述为“严格成本合规”。直接证据为 `backend/internal/controlplane/routing_policy_runtime_test.go#TestRoutingPolicyAutomaticLowPricePoolUsesNormalizedDefaults`。

### 3.2 试算器不声明最终运行时顺序

Simulator 没有应用 ID、主体 ID、凭据 ID、sticky key 和有效成本 cohort，也不会真实占用容量。它可以解释静态策略与当前容量投影，但不能声明以下结论：

- 某一真实客户的最终粘性线路；
- 某一 canary cohort 是否命中动态有效成本决策；
- 从试算到请求发出期间容量没有变化；
- 首字节前后故障切换真实发生。

这些结论分别由 `TestRoutingPolicyUpdateInvalidatesStickyGatewaySelection`、`TestRoutingPolicySmartOptimizationReordersRealGatewayRequest`、`TestGatewayChatCompletionSkipsAccountAtConcurrencyCapacity` 和两条 Streaming Gateway 测试证明。

## 4. 回归命令

```bash
cd backend
GOMAXPROCS=1 go test -p=1 ./internal/controlplane ./internal/server -count=1

cd ../frontend
npm run typecheck
npm run test:unit
npm run build
npm run generate:e2e-capabilities
npm run check:e2e-coverage
npm run test:e2e -- e2e/routing-policy.spec.ts
```

`check:e2e-coverage` 是浏览器治理门禁；其失败不能用算法单元测试通过来替代。PostgreSQL、生产单源构建和远程环境按 [`README.md`](./README.md) 的 Gate B、Platform 与 Remote 合同单独验收。

## 5. 2026-08-13 验证记录

本轮在 macOS arm64、本地 memory repository、隔离 fake upstream / OIDC / SMTP / S3 / official services 上完成：

- 全量 Chromium Browser：`82 passed`、`62 skipped`、`0 failed`。跳过项为重复视口的状态型 journey，以及需要 PostgreSQL 或 release binary 的显式环境合同。
- 策略与运行时核心 race：`internal/controlplane`、`internal/server`、`internal/plugins`、`internal/system` 全部通过。
- 后端普通全包：`go test ./...` 通过，包含 migrations 包；聚合语句覆盖率 `62.9%`。
- 前端：`38` 个测试文件、`138` 个单元测试通过；语句覆盖率 `71.71%`，分支覆盖率 `60.30%`；typecheck 和生产构建通过。
- 生产单源：编译后的 Go 二进制通过 readiness、公开设置、SPA 深链、官网图片和 `SIGTERM` 关闭验证。
- E2E 治理：`43` 条产品路由、`49` 个场景、`192` 个 API 操作的 success / negative / boundary / browser 缺口均为 `0`。

本轮修复了三个测试基础设施问题：

1. 应用生命周期场景曾遗留带预算的全局访问策略，导致后续独立 Gateway fixture 在计费保留阶段返回 402；策略现限定到本场景 Workspace Key，并有创建响应和顺序回归断言。
2. fake OIDC 曾转发逐跳头并关闭上游连接，导致 Chromium 刷新出现 `ERR_TOO_MANY_RETRIES`；现过滤逐跳头并复用 keep-alive 连接。
3. fake OIDC 与后端 discovery 曾存在启动竞态；E2E 编排现等待 HTTPS listener 发布 ready 信号后再启动后端。

未执行项：

- 本机没有 Docker，且现有 `127.0.0.1:5432` 是 PhpWebStudy 管理的 PostgreSQL 18 运行库，没有明确的专用测试 URL；为避免触碰未知数据，本轮未执行 PostgreSQL 16 Gate B、备份恢复和重启持久化。
- 当前平台不是 Linux amd64，未执行 Linux release archive、容器、安装、升级和回滚生命周期。
- 未对远程部署执行验证；Remote 证明必须在再次明确授权后使用隔离测试数据运行。
