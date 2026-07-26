# AsterRouter 产品体系需求文档（PRD）

## 0. 文档信息

| 项目 | 内容 |
| --- | --- |
| 产品 | AsterRouter + AsterCloud + Plugin Ecosystem |
| 文档状态 | Target v1.0 |
| 基线日期 | 2026-07-22 |
| 目标用户 | 企业 AI 平台、Relay Operator、SaaS/AI 平台、多模态团队、插件开发者、SRE |
| 核心原则 | Local-first、Core owns truth、Cloud optional、Plugins constrained |

## 1. 背景与问题

AI 应用通常从单一 Provider 和一个 API Key 起步，随后迅速遇到多供应商协议差异、账号容量、密钥安全、模型切换、成本、长任务、媒体产物、租户权限和故障恢复问题。各业务团队重复实现代理与重试会形成不一致的数据面和无法解释的费用。

AsterRouter 当前已经具备覆盖广泛的 Core、Profile、多模态和插件能力，AsterCloud 也具备目录、授权和开发者服务。下一阶段的主要问题不再是“有没有功能”，而是建立统一边界、稳定契约、安全插件生态和可验证的规模化运行。

## 2. 产品愿景

任何团队都能在自己控制的数据边界内，以一个稳定 Gateway 接入可信 AI 供应；用相同的身份、策略、路由、计量和审计模型覆盖文本、多模态和实时场景；按需通过官方或私有插件扩展，而无需把敏感请求交给一个强依赖的外部云平台。

## 3. 目标与非目标

目标见 [产品定位与系统边界](./01-产品定位与系统边界.md#3-产品目标)。本 PRD 额外强调：

- P0 目标是正确、稳定、可运维的私有部署主链路。
- AsterCloud 和插件增强体验，但不降低无云、无插件时 Core 的正确性。
- 多 Region、任意热路径插件和复杂市场交易属于条件能力，不提前承诺。

## 4. 用户与场景

| 场景 | 主要角色 | 核心结果 |
| --- | --- | --- |
| 企业内部 AI | 企业管理员、员工、平台工程师 | 部门治理下安全调用多个模型 |
| API/中转运营 | Operator、客户、财务 | 多账号供应、客户隔离和可信结算 |
| 外部 SaaS 集成 | 平台管理员、应用开发者 | 保留业务身份系统并统一 AI 数据面 |
| 多模态创作 | 内容用户、Provider 工程师 | 可恢复任务、产物交付和按规格计量 |
| 插件生态 | 插件开发者、审核员、实例管理员 | 安全发布、安装、运行、更新和撤销 |
| 官方服务 | AsterCloud 运营、客户管理员 | 可信目录、授权、更新和情报服务 |

## 5. 功能需求

### 5.1 接入、身份与权限

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-IAM-001 | 支持本地账号、OIDC、MFA 和 Session 撤销 | P0 | 登录与敏感操作策略可配置、可审计 |
| FR-IAM-002 | 支持 Profile/Tenant/Principal/RoleBinding | P0 | Repository 级隔离与负向测试通过 |
| FR-IAM-003 | 支持 Workspace/User/Customer/Service Key | P0 | 明文一次展示、Hash、Scope、过期、禁用 |
| FR-IAM-004 | 支持 Key Family 轮换与紧急撤销 | P0 | 重叠窗口和近实时失效可验证 |
| FR-IAM-005 | 支持 External Auth Integration | P0 | HMAC/JWT、重放保护、Tenant 绑定 |
| FR-IAM-006 | 支持机器身份与自然人生命周期分离 | P1 | 员工离职不破坏生产应用 |

### 5.2 Gateway 与供应

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-GW-001 | 提供稳定 OpenAI-compatible Gateway API | P0 | 兼容矩阵、错误码、限制和版本策略明确 |
| FR-GW-002 | 支持 Direct JSON/SSE 与 Realtime | P0 | 提交点、背压、中断和 Usage 正确 |
| FR-GW-003 | 支持图片、视频、音频 Durable Job | P0 | 幂等、取消、回调/轮询、恢复通过 |
| FR-GW-004 | 支持 Gateway Model 与多 Route | P0 | 客户模型稳定，决策可解释 |
| FR-GW-005 | 支持多 Provider Account 健康与调度 | P0 | 熔断、冷却、优先级、权重有效 |
| FR-GW-006 | 支持 RPM/TPM/并发/加权容量 | P0 | 多副本不超卖，Direct/Durable 公平 |
| FR-GW-007 | 支持多云 Provider 类型与 Adapter | P1 | 通过统一 Conformance Suite |

### 5.3 策略、计量与成本

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-FIN-001 | Scope、模型、模态、操作、CIDR、速率和预算策略 | P0 | Simulator 与实际结果一致 |
| FR-FIN-002 | 生成版本化标准 Usage | P0 | 每种模态完整、重放幂等 |
| FR-FIN-003 | 客户价格和 Provider 成本分别版本化 | P0 | 独立 Evaluation 与 Ledger |
| FR-FIN-004 | 支持 Billing Hold 与最终结算 | P0 | 不足不调用，差额释放，故障可恢复 |
| FR-FIN-005 | 支持 Effective Price 与成本约束路由 | P1 | 来源/TTL/置信度和硬约束明确 |
| FR-FIN-006 | 支持 Savings Evidence 和对账 | P1 | 可追溯 Operation、基线、实际成本 |

### 5.4 Job 与 Artifact

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-JOB-001 | Job 使用持久状态、Lease、公平队列 | P0 | 崩溃恢复与租户公平通过 |
| FR-JOB-002 | Provider Dispatch 有安全幂等/未知状态 | P0 | 不盲重放任务创建 |
| FR-ART-001 | Artifact 支持五种 Policy | P0 | ACL、TTL、存储和交付语义明确 |
| FR-ART-002 | 支持 Local/S3-compatible Store | P0 | 校验和、失败状态和短期访问 |
| FR-ART-003 | 支持客户 Sink 与删除证据 | P1 | Tenant Binding、重试、保留与审计 |

### 5.5 插件平台

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-PLG-001 | 版本化 Manifest/Package/Contribution | P0 | Schema 与兼容矩阵可自动校验 |
| FR-PLG-002 | 签名、Checksum、SBOM 和权限差异 | P0 | 篡改与权限升级被阻止 |
| FR-PLG-003 | 安装、启用、禁用、更新、回滚、卸载 | P0 | 状态可恢复，数据默认保留 |
| FR-PLG-004 | Sidecar Supervisor 与 Host API | P0 | 本机隔离、短期 Token、资源限制 |
| FR-PLG-005 | 前端贡献与 Surface 控制 | P0 | CSP、路由隔离、无 Secret 暴露 |
| FR-PLG-006 | Developer Upload/Review/Publish | P1 | 扫描、人工审核、签名、Channel 完整 |
| FR-PLG-007 | 安全公告和版本撤销 | P0 | 在线/离线实例都能识别风险 |

### 5.6 AsterCloud

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-CLD-001 | 发布签名 Catalog、Package、Core Release | P0 | 防篡改、防回滚、可离线导入 |
| FR-CLD-002 | 管理 Product/SKU/License/Entitlement | P0 | 履约幂等、实例绑定、审计 |
| FR-CLD-003 | 在线激活与离线 License | P0 | 断网、过期、撤销和换机规则明确 |
| FR-CLD-004 | Security Advisory 与 Key Rotation | P0 | 影响版本与客户端动作可验证 |
| FR-CLD-005 | Official Service/Feed | P1 | Schema 兼容、签名/加密、TTL |
| FR-CLD-006 | Provider Intelligence/Probe | P1 | 探测授权、最小数据、结果可追溯 |

### 5.7 运维与交付

| ID | 需求 | 优先级 | 验收摘要 |
| --- | --- | --- | --- |
| FR-OPS-001 | 健康、版本、Metrics、Logs、Traces | P0 | Role/业务健康可区分 |
| FR-OPS-002 | 备份、恢复、诊断与升级回滚 | P0 | 生产演练且无敏感泄露 |
| FR-OPS-003 | API/Worker/Plugin Host 分角色 | P1 | 与 all-in-one 语义一致 |
| FR-OPS-004 | SLO、告警、Runbook 和故障注入 | P1 | P0 场景全部演练 |

## 6. 非功能需求

| ID | 需求 | 目标 |
| --- | --- | --- |
| NFR-SEC-001 | 租户隔离 | 跨租户越权测试零通过 |
| NFR-SEC-002 | Secret 保护 | 日志/UI/诊断/插件目录零明文 |
| NFR-COR-001 | 幂等正确性 | 重放、崩溃、竞态不重复外部副作用或 Ledger |
| NFR-AV-001 | 数据面自治 | AsterCloud 不可达不影响有效本地授权 |
| NFR-AV-002 | Gateway 可用性 | 建立基线后目标 99.95%/月候选 |
| NFR-PERF-001 | 热路径依赖 | 不逐请求访问 AsterCloud，不执行无界插件调用 |
| NFR-DR-001 | 财务事实 RPO | 目标 RPO=0，依赖事务与 Outbox |
| NFR-DR-002 | 服务恢复 | 单 Region HA 目标 RTO 30 分钟内，需演练确认 |
| NFR-COMP-001 | API/插件兼容 | SemVer + Compatibility Matrix + Conformance Test |
| NFR-OBS-001 | 可解释性 | 权限、Route、价格、降级均有 reason code |
| NFR-PRIV-001 | 数据最小化 | 云端遥测默认不含私有调用内容 |

## 7. 产品指标

| 类别 | 指标 |
| --- | --- |
| 激活 | 安装到首个成功调用时间、首个插件启用成功率 |
| 可靠性 | 平台成功率、P95、Queue Wait、结算延迟、Artifact 交付率 |
| 治理 | 最小权限 Key 比例、策略覆盖率、越权阻断、Key 轮换率 |
| 供应 | 可用 Route 数、容量拒绝、Provider 故障恢复、价格陈旧度 |
| 财务 | Usage 完整率、Ledger 重复率、成本覆盖、对账差异、节省证据率 |
| 插件 | 审核周期、安装成功、Sidecar 崩溃率、兼容覆盖、安全修复时间 |
| 云服务 | Catalog/License 可用、签名失败、离线履约成功、公告触达时间 |

## 8. 发布范围

阶段范围与门槛见 [实施路线图与验收](./10-实施路线图与验收.md)。任何功能只有同时具备 API/UI、数据模型、权限、审计、观测、迁移、回滚和测试证据，才可从 Preview 进入 GA。

## 9. 未决策项

| 决策 | 最晚时间 | 默认行为 |
| --- | --- | --- |
| Plugin Host 进程是否首阶段独立 | M3 设计冻结 | 与 Core 同进程管理 Supervisor，但保持接口边界 |
| License Grace Period 具体时长 | M4 商业/安全评审 | 使用签名授权内显式值，不做永久宽限 |
| 多 Region 云平台选择 | M6 启动前 | 云中立单 Region HA，不预埋 AWS 专属领域模型 |
| 热路径 WASM 插件 | M5 后评估 | 不开放 |
| 合同 SLO | 有生产基线后 | 仅内部目标，不对外承诺 |
