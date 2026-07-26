# AsterRouter V5：可信团队接入与供应利用率

> 状态：V5 实施中；M1、M2 与 M3 的 P0 闭环已完成，M4 的容量、恢复与升级证据已补齐但尚未达到完整 Release 门禁。
>
> 基线日期：2026-07-26。当前产品事实以仓库根目录 `README.zh-CN.md`、代码和已发布版本为准；系统长期不变量以 `docs/goal` 为准；体验重构原则以 `docs/refactor/v1` 为准。本文只定义 V5 的增量目标、顺序和验收门槛，不复制 V4 已有设计。

## 当前实施状态（2026-07-26）

| 里程碑 | 状态 | 已交付事实 | 尚未完成 |
| --- | --- | --- | --- |
| M1：统一接入中心 | **P0 完成** | 空实例四步首日流程；Codex、Claude Code、OpenAI SDK、Anthropic SDK 配置生成与真实验证；Session 幂等恢复；API Key、Usage、Trace、Operation 与 Audit 闭环；中英文、明暗主题和三视口浏览器验收 | Windows 原生配置示例属于 P1，不阻塞 M1 P0 |
| M2：协议完整性 | **P0 完成** | Anthropic Count Tokens 精确上游链路；独立 Canonical Embedding Pipeline；12 条当前/前一支持线 `CompatibilityRecord`；支持窗口、只读 API、RBAC、接入中心状态与协议证据守卫 | 官方客户端/SDK 固定版本运行矩阵属于后续证据增强；当前记录明确标注 `protocol_mock`，不冒充客户端运行证据 |
| M3：供应利用率与容量建议 | **P0 完成** | Provider Account、Route Group、Published Model、Application 四维投影；Unknown/Stranded Capacity、证据新鲜度、只读建议、RBAC、管理视图与 Trace 下钻 | Route/预算策略模拟仍按 P2 条件交付，不阻塞 M3 P0 |
| M4：双实例与生产强化 | **部分交付** | PostgreSQL 共享并发租约和 RPM/TPM 窗口；Application/Tenant 聚合并发边界；跨实例半开探测；按 Schema 隔离的统一 Migration Lock；并发插件目录初始化；双实例 Nginx/Compose 模板与运行手册；跨实例 Key/Policy/Usage/撤销旅程；Provider Account、Credential 与 Tenant 64 路并发不超卖；双 Service PostgreSQL 30 分钟持续负载；流中断终态；真实数据库网络断连恢复；Provider/容量指标、告警规则和看板；备份恢复与 `v0.17.0` 升级演练；HA 容器故障切换、PostgreSQL 16 和 Linux Release 机器门禁 | 在包含本轮改动的 SHA 上取得 PostgreSQL 16 全量、HA 容器、Linux 产物安装/升级/回滚及候选浏览器旅程的 CI 证据，并固化 required checks |

本轮实现证据：

- 接入中心从空 Memory 实例完成模型来源、发布模型、创建 API Key 和真实验证；四类客户端均经过 API 与真实浏览器旅程，成功请求可下钻到 Operation、Usage 和 Trace。
- 接入 Session、Provider、Account、Gateway Model、Route 与 API Key 使用确定性标识和 CAS 恢复；Provider Secret 加密，下游完整 Key 不写入数据库、浏览器存储、配置、审计摘要或 Trace。
- 接入链路的 Memory/PostgreSQL Repository 契约、迁移重复初始化、状态约束、并发重放、过期和失败恢复均通过；本地数据库证据使用隔离 PostgreSQL 18.4，CI 的正式门禁仍固定 PostgreSQL 16。
- 接入视图在 `1440x900`、`1280x800`、`390x844` 下完成中英文和明暗主题验收，无页面级横向溢出或浏览器错误。
- Anthropic Count Tokens 使用可验证的上游精确计数，不产生推理 Usage/Cost；Canonical Embedding Pipeline 覆盖输入、编码、维度、Route 能力、Usage、Cost、Trace 与畸形上游响应。
- 兼容性清单使用 `asterrouter.compatibility.v1` Schema，覆盖四类客户端的 CLI、JavaScript、Python 当前版本与前一支持线；`GET /api/v1/onboarding/compatibility-records` 返回 Router/协议版本、测试时间、到期时间、能力、限制和证据等级。
- 兼容状态由执行结果与 30 天支持窗口派生；证据过期或未执行时自动降级为“待确认”。当前证据来自协议模拟套件，接入中心明确显示“未执行官方客户端运行时”，不把内部协议测试表述为真实 SDK 执行。
- PostgreSQL 全量后端测试通过，包含迁移重复初始化、Repository 持久化和并发写入。
- 容量不超卖、跨实例半开探测和并发目录初始化通过 `go test -race`。
- 两个 Service 共享同一 PostgreSQL 时，Key Policy 变更、Usage 写入、撤销和 Audit 在另一实例立即可见。
- Provider Account 和下游 Credential 分别通过 64 路并发竞争测试，配置并发上限为 3 时均只签发 3 个租约。
- 双实例 Nginx/Compose 模板已提供；完整拓扑验收会启动 PostgreSQL 16、两个非 root 应用容器和 Nginx，分别停止任一实例验证连续服务，并在两个实例均停止时验证代理失败。该脚本已进入 CI，本机因没有容器运行时尚未取得执行证据。
- 断流后已发出的内容保持可见，不会切换备用上游；Operation、Attempt、Trace 和 Usage 都以 `stream_error` 终态记录。
- `/ready` 在 PostgreSQL Repository 连接池关闭后立即失败，响应保持脱敏；两个实例经临时 TCP 代理执行真实网络中断和恢复，阻断期健康检查保持可用、Readiness 脱敏返回 503，恢复后两个连接池重新就绪。
- PostgreSQL 双 Service 持续负载交替经过两个 Handler，逐请求验证 Credential 租约、Operation、Attempt、Usage 和 Billing Hold 终态；本地 30 分钟完成 12,686 次普通/流式请求，协程增量为 3。普通 CI 运行短门禁，定时工作流保留长测证据。
- `deploy/HA_RUNBOOK.md` 说明了双实例启动、流中断、数据库故障、外部探针和逐实例升级边界。
- `/metrics` 仅在配置独立 Token 后启用，输出有限基数的 HTTP、Readiness、Application/Tenant/Provider 容量准入与 Provider 水位指标；告警覆盖持续未就绪、Readiness 抖动、5xx、容量拒绝与 Provider 不可调度，Grafana 容量看板模板已提供。`promtool 3.5.0` 本地确认配置语法、8 条规则和告警规则测试通过。
- 两个后端实例连接同一 PostgreSQL 同时启动；健康、就绪、生产单源深链接和两个供应 API 均通过烟测。
- 一个实例优雅退出后，另一个实例继续接受新的登录和供应查询。
- Readiness 依赖失败返回稳定 503 和错误码，底层连接错误只进入服务日志，不向匿名请求泄漏。
- 专用恢复数据库上的备份恢复演练通过，验证 Provider Secret 可用性、用户与 Key、Usage、Trace、Audit、Alert、插件和导出内容恢复；CI 会显式断言该测试实际产生 `pass` 事件。
- `v0.17.0` Schema 夹具可由候选运行时升级并重启读取：历史用户邮箱归一化回填、历史 Tenant 默认并发上限、Provider 容量表及其约束均已验证；CI 同样拒绝零匹配通过。
- 前端 147 项单测、覆盖率门禁、类型检查、生产构建和企业 Surface 检查通过。
- 接入中心浏览器旅程使用本机 Chrome 完成 `7 passed / 8` 按设计跳过；四类客户端均核对版本与证据等级，`1440x900`、`1280x800`、`390x844` 均推进到兼容证据区并通过中英文、明暗主题、溢出和浏览器错误检查。
- 供应管理视图在 `1440x900`、`1280x800`、`390x844` 下无页面级横向溢出，且无控制台错误、页面异常或失败请求。

当前数据库证据使用本地隔离的 PostgreSQL 18.4；备份恢复只作用于刚创建的专用恢复数据库。CI 已固定 PostgreSQL 16 并新增运行时版本断言，Build/Release 已显式检查候选运行时、浏览器旅程和安装升级回滚报告，但仍需在包含本轮改动的 SHA 上实际执行并归档证据；因此不能把“双实例生产就绪”作为当前版本承诺。

## 1. 执行摘要

V5 不再通过增加新的部署角色或平级业务模块扩大产品面，而是选择一个更窄、可验证的企业入口：

> 让团队使用一个受控入口和按应用分配的凭据，在 Codex、Claude Code、IDE、SDK 与内部服务中安全调用企业批准的 AI 供应，并用真实数据判断供应是否充足、闲置或需要扩容。

V5 聚焦五个结果：

1. **十分钟内完成首次受治理调用。** 管理员连接一个已获授权的模型来源，发布模型，创建应用凭据，并在目标客户端完成真实验证。
2. **客户端协议真实可用。** Codex、Claude Code、OpenAI SDK 与 Anthropic SDK 使用公开、版本化、经过兼容测试的入口，不依赖含混文案或手工猜配置。
3. **供应链路可见。** 企业能核对请求使用的 Gateway Model、Route、Provider、账号、错误分类和用量证据，但普通成员看不到上游 Secret。
4. **供应利用率可解释。** 系统区分真实消耗、容量水位、不可用容量和未知额度，不把无法观测的订阅上限伪装成精确 Token 余额。
5. **扩容由证据驱动。** 系统给出带置信度的容量建议，管理员决定是否增加资源；V5 不自动采购、登录或续期上游账号。

V5 的竞争策略不是复制 同类项目 的消费者订阅账号池，而是把 AsterRouter 已有的多云官方凭据链、多协议、多模态、平台委托接入、计量与审计能力，收敛成更容易开始、更容易验证的团队体验。

## 2. 背景与问题

### 2.1 用户当前面对的选择

一个 20 人左右的研发团队通常在三种方案间摇摆：

| 方案 | 表面收益 | 主要问题 |
| --- | --- | --- |
| 按人购买完整订阅 | 采购简单、个人独立 | 固定成本高，使用强弱不均，额度不能统一治理 |
| 低价第三方中转 | 单价低、接入快 | 模型、路由、数据留存和降级行为难以核验 |
| 共享账号密码 | 少买账号 | 凭据扩散、登录冲突、离职回收和审计失效 |

企业真正需要的不是另一种共享密码方式，而是把**已获授权的供应资源**转换为可分配、可撤销、可观察、可审计的 AI 能力。

### 2.2 AsterRouter 当前优势

AsterRouter 已经具备 V5 所需的大部分可信 Core：

- Provider Connection、Provider Account、Gateway Model、Route Group 与候选调度。
- OpenAI、Anthropic、Gemini 兼容协议，以及 AWS Bedrock、GCP Vertex、Azure OpenAI 官方凭据链。
- Workspace/User/Customer/Service Key、模型白名单、配额、预算、容量、熔断、Trace、Usage、Cost 与 Audit。
- Enterprise、Personal、Relay Operator、AI Platform 四种部署角色。
- 图片、音频、Realtime、异步 Job、Artifact 与插件运行时。
- PostgreSQL 生产存储、备份恢复、诊断和 Release 更新链路。

V5 不重建这些能力，而是解决它们尚未形成清晰团队闭环的问题。

### 2.3 当前缺口

| 缺口 | 用户影响 | V5 处理 |
| --- | --- | --- |
| 客户端兼容证据会过期，且协议模拟不等于真实客户端执行 | UI 和文档可能继续展示已经失效或证据等级不足的支持结论 | 使用版本化 `CompatibilityRecord`、证据等级和支持窗口守卫，过期后自动显示“待确认” |
| 利用率散落在 Usage、Trace、容量和账单源中 | 管理员无法回答是否该扩容 | 建立供应利用率投影和容量建议 |
| 当前公开承诺仍以单实例为主 | 团队网关存在维护窗口和扩展上限 | 完成 PostgreSQL 多实例文本主链路验收 |
| 产品范围宽，第一价值表达不够集中 | 市场难以在一分钟内理解产品 | 用“首次受治理调用”统一首日体验和文档 |

## 3. 产品决策

### 3.1 V5 采用的能力

V5 吸收同类项目已经证明有效的产品模式：

- 普通成员、团队负责人和管理员看到与职责匹配的任务，不先学习全部内部对象。
- 一个应用凭据可以用于多个受支持客户端，但每个应用、团队或环境仍保持独立归属。
- 为 Codex、Claude Code 和 SDK 生成可恢复、可验证的配置，不要求用户自行拼接 Base URL。
- 用应用、用户、部门、模型和成本中心归集用量。
- 从低成本单机开始，并为 PostgreSQL 多实例提供明确升级路径。
- 把供应利用率、失败率、并发峰值和成本证据放进同一决策视图。

### 3.2 V5 不采用的能力

V5 明确不实现：

- 把 ChatGPT、Codex、Claude 等消费者订阅账号的浏览器 Session、Cookie 或非公开 Token 转换为生产 Provider Credential。
- 自动登录上游账号、代收验证码、刷新消费者订阅 Token、抓取私有余额或对抗账号风控。
- 共享上游账号密码，或向普通成员暴露 Provider Secret、OAuth Token 和供应商控制台身份。
- 把“技术上可以转发”解释为“供应商已授权企业池化、转售或多人共享”。
- 在缺少上游额度事实时宣称“还剩多少 Token”或计算虚假的订阅利用率。
- 自动采购、扩容、续费或删除 Provider Account。
- 新增第五种部署角色、新建第二套 Project/Key/Usage 事实源，或复制 同类项目 的领域命名。

### 3.3 未来消费者订阅连接器的决策门

消费者订阅连接器不属于 V5。未来只有同时满足以下条件，才允许单独立项评审：

1. 上游书面条款或企业合同明确允许目标使用方式。
2. 使用公开、稳定、可测试的授权与推理协议。
3. Token 生命周期、撤销、审计、数据处理和区域边界有完整安全设计。
4. 连接器可以独立禁用和回滚，不污染标准 Provider Core。
5. 产品文案明确区分技术能力、采购权利和服务保证。

任何单一客户需求、市场热度或短期成本优势都不能跳过这些条件。

## 4. 目标用户与关键旅程

### 4.1 第一用户：AI 服务负责人

他负责为团队连接 AI 供应，并希望快速回答：

- 哪些模型已经可以安全使用？
- Codex、Claude Code 或 SDK 应该配置什么？
- 请求实际走了哪个已批准来源？
- 当前容量是否足够，失败来自哪里？
- 应增加供应，还是先调整模型、路由和限额？

### 4.2 第二用户：团队负责人

他管理应用、成员、预算和成本归属，但不应看到完整上游凭据。他需要：

- 为应用或环境创建最小权限凭据。
- 绑定部门、组织 Group、负责人和成本中心。
- 设置模型范围、过期、并发和预算。
- 查看团队用量、异常和容量影响。
- 在项目结束或人员离职时只撤销对应访问。

### 4.3 第三用户：研发成员

他只需要：

- 看到自己可以使用的模型。
- 获取一次性展示的应用凭据或组织批准的凭据注入方式。
- 选择 Codex、Claude Code、SDK 等目标客户端并完成验证。
- 查看自己的调用、额度和明确错误，不接触 Provider 细节。

### 4.4 V5 首日闭环

```text
选择使用意图
  -> 连接并验证已授权模型来源
  -> 发布一个推荐模型
  -> 创建应用与最小权限凭据
  -> 选择 Codex / Claude Code / SDK
  -> 生成配置并执行真实请求
  -> 展示 Route、Usage、Policy 和 Trace 证据
  -> 进入安静的持续运行状态
```

复杂组织同步、价格规则、插件市场、Artifact Sink 和高级路由不能阻塞该闭环。

## 5. V5 范围

### 5.1 P0：必须交付

1. 统一客户端接入中心：Codex、Claude Code、OpenAI SDK、Anthropic SDK、通用 HTTP。
2. 应用凭据创建、一次性展示、轮换、撤销、最小 Scope 与模型范围。
3. 真实连通性验证：模型可见不等于 Route 可用，必须执行目标协议请求。
4. `POST /v1/messages/count_tokens`。
5. `POST /v1/embeddings` 与独立 Embedding Pipeline。
6. 客户端兼容矩阵、最新与前一主要版本的自动化契约测试。
7. 供应利用率、并发峰值、拒绝率、失败切换和闲置/不可用原因视图。
8. 只读容量建议，包含依据、置信度、适用窗口和反证。
9. PostgreSQL 双后端实例下的 Key、配额、容量、路由、Usage 与撤销一致性验收。
10. 文档、UI、API Schema 和实际行为的漂移守卫。

### 5.2 P1：应交付

- macOS、Linux、Windows 的安全配置示例和恢复流程。
- 配置导出使用环境变量或组织凭据工具，默认不把明文 Key 写进文件。
- 按 Application、User、Department、Group、Model、Provider 和 Cost Center 的利用率钻取。
- 供应健康、账单证据和价格新鲜度参与容量建议。
- 配置验证结果与 Request ID、Trace 和兼容矩阵关联。
- Prometheus 指标和脱敏诊断包覆盖接入与容量主链路。
- 单机到多实例的无业务对象迁移升级路径。

### 5.3 P2：条件交付

- 在证据充分时提供成本/可靠性策略模拟，不直接修改生产 Route。
- 企业批准的客户端配置模板分发与版本管理。
- 更细粒度的环境隔离，例如同一 Application 下的 Development/Staging/Production Credential。
- 对已公开 Provider 额度 API 的标准化 Quota Observation Adapter。

P2 不能延迟 P0，也不能在没有真实用户证据时预留复杂抽象。

## 6. 产品信息架构

V5 不新增部署角色和一级后台。它复用 `docs/refactor/v1` 定义的产品投影：

| 用户看到的对象 | 复用的系统事实 |
| --- | --- |
| 应用 | Gateway Principal + API Key + Policy + Usage Scope |
| 已发布模型 | Gateway Model + Route Group + Provider Account Mapping |
| 模型来源 | Provider Connection + Provider Account + Inventory |
| 接入配置 | Client 类型 + Published Model + Gateway URL + Credential 引用 |
| 供应利用率 | Capacity、Usage、Attempt、Billing Snapshot 和 Health 的时间窗口投影 |
| 容量建议 | 利用率证据 + Policy + SLO 目标生成的只读决策草案 |

### 6.1 接入中心

接入中心属于“应用”详情中的任务，不新增独立全局产品：

1. 选择客户端。
2. 选择当前应用允许的模型。
3. 选择凭据注入方式。
4. 显示配置差异和影响范围。
5. 执行真实验证。
6. 显示成功证据或可操作错误。
7. 提供恢复原配置的方法。

服务器不直接修改员工电脑上的配置文件。它只生成经过版本校验的配置片段、命令和验证请求。

### 6.2 供应视图

普通成员只看到模型可用性和自己的额度。团队负责人看到应用与团队范围。平台管理员可以进一步查看：

- Provider 与账号健康。
- Route 候选、排除原因和 Fallback。
- RPM、TPM、并发与可观测额度窗口。
- 成本、价格和账单证据新鲜度。
- 供应利用率、峰值、拒绝率和容量建议。

Secret、完整 OAuth Token、浏览器 Session、其他部门明细和未授权采购价始终隐藏。

## 7. 客户端接入设计

### 7.1 支持矩阵

| 客户端 | V5 入口 | 验证要求 |
| --- | --- | --- |
| Codex CLI / Desktop / IDE | `/v1/models`、`/v1/responses` | 非流式与流式 Responses、工具调用、Reasoning、错误格式 |
| Claude Code | `/v1/messages`、`/v1/messages/count_tokens` | Bearer 与 `x-api-key`、流式、工具调用、计数、错误格式 |
| OpenAI SDK | `/v1/models`、Chat、Responses、Embeddings | Python/JavaScript 最新与前一主要版本 |
| Anthropic SDK | Messages、Count Tokens | Python/JavaScript 最新与前一主要版本 |
| 通用 HTTP | 公开 Gateway API | curl 示例与 OpenAPI 契约 |

“支持客户端”必须同时满足：配置可生成、真实请求通过、失败可诊断、恢复流程可执行。只有页面图标或静态示例不算支持。

### 7.2 配置安全

- 默认通过环境变量或组织凭据工具注入 AsterRouter Key。
- 如客户端只支持文件保存，必须提示权限要求、作用域和恢复方式。
- 配置中只出现 AsterRouter Gateway URL 与下游 Key，不出现 Provider Secret。
- Key 不进入 URL、命令历史、截图、日志、诊断包和遥测。
- 每个配置明确绑定 Application、Credential、允许模型和环境。
- 验证请求使用最小输出和明确模型，不自动读取或上传项目文件。

### 7.3 兼容事实源

兼容矩阵由 CI 产物生成或校验，至少记录：

- 客户端/SDK 名称与版本。
- AsterRouter 版本与协议版本。
- 测试时间、测试套件和结果。
- 已验证能力与已知限制。
- Mock、兼容上游或授权 Live Provider 的证据等级。

UI 和文档只能展示仍在支持窗口内的 Compatibility Record。过期或未验证时显示“待确认”，不能继续显示“完全支持”。

当前版本快照：

| 客户端 | 语言/运行形态 | 当前版本 | 前一支持线 | 当前证据 |
| --- | --- | --- | --- | --- |
| Codex | CLI | `0.145.0` | `0.144.6` | `protocol_mock` |
| Claude Code | CLI | `2.1.220` | `1.0.128` | `protocol_mock` |
| OpenAI SDK | JavaScript | `6.49.0` | `5.23.2` | `protocol_mock` |
| OpenAI SDK | Python | `2.48.0` | `1.109.1` | `protocol_mock` |
| Anthropic SDK | JavaScript | `0.115.0` | `0.114.0` | `protocol_mock` |
| Anthropic SDK | Python | `0.120.0` | `0.119.0` | `protocol_mock` |

对 `1.x` 及以上版本，前一支持线取前一 major 的最高公开版本；对仍处于 `0.x` 的包，取前一 minor 的最高公开版本。版本来源与测试引用保存在清单中；当前证据只执行内部协议套件，固定版本官方客户端/SDK 运行矩阵尚未执行。

## 8. 协议补齐

### 8.1 Anthropic Count Tokens

新增 `POST /v1/messages/count_tokens`，但不得用粗糙字符数估算冒充精确结果：

1. 先执行 Credential、模型范围和协议能力校验。
2. 解析 Published Model 的版本化 Tokenizer Profile；存在多个可执行 Route 时，它们必须声明同一 Tokenizer Compatibility Group，否则返回不支持。
3. 调用公开上游计数能力或经过版本验证的精确 Tokenizer，不占用推理费用预算。
4. 返回 Anthropic 兼容结果和 `x-request-id`。
5. 写入操作与审计证据，但不创建推理 Usage/Cost 记录。
6. 无法保证语义时返回 `unsupported_feature`，不返回伪精确数字。

计数请求仍受请求大小、QPS 和安全策略限制，防止成为免费计算或拒绝服务入口。

### 8.2 Canonical Embedding Pipeline

Embeddings 不复用文本 Chat IR，也不使用 `native_media` 伪装：

```text
OpenAI Embeddings Decoder
  -> Canonical Embedding Request
  -> Credential / Policy / Quota
  -> Embedding Capability Filter
  -> Provider Adapter
  -> Canonical Embedding Result
  -> Usage / Cost / Trace / Audit
  -> OpenAI Embeddings Encoder
```

首期范围：

- 字符串与字符串数组输入。
- `encoding_format` 的明确支持矩阵。
- 输入数量、单项大小、总大小和输出维度限制。
- 上游模型、维度、Usage 和价格快照记录。
- 非流式请求；不引入 Batch、文件上传或向量数据库。
- Route 创建时强制检查 `embedding` 模态与 Adapter Capability。

## 9. 供应利用率模型

### 9.1 术语

| 指标 | 定义 |
| --- | --- |
| Demand | 通过 Credential 与 Policy 校验、需要目标模型能力的请求工作量 |
| Served Demand | 成功获得合格 Route 并达到协议提交点的工作量 |
| Rejected Demand | 因容量、额度、策略或无可用 Route 被拒绝的工作量 |
| Configured Capacity | 管理员配置或 Provider 明确报告的 RPM、TPM、并发与额度上限 |
| Observed Load | 实际请求、Token、并发、时长或媒体加权工作量 |
| Headroom | 在证据有效时，Configured Capacity 与安全水位之间的剩余空间 |
| Stranded Capacity | 因模型、地区、Policy、健康、协议或 Route 不匹配而不能服务当前 Demand 的容量 |
| Unknown Capacity | Provider 没有公开上限或额度证据，不能计算精确剩余量的供应 |

### 9.2 核心指标

每个 Provider Account、Route Group、Published Model 和 Application 至少计算：

- 请求数、成功率、429/5xx、Fallback 率和无候选率。
- 输入、输出、缓存、Reasoning Token 与请求成本。
- 当前及 P50/P95/P99 并发。
- RPM/TPM/并发水位；有公开证据时增加额度窗口水位。
- 容量拒绝、策略拒绝、账号错误和协议不兼容的独立计数。
- 高峰时段、空闲时段、冷却时长和健康覆盖率。
- Served Demand、Rejected Demand、Stranded Capacity 和 Unknown Capacity。
- 数据新鲜度、样本数、证据来源和置信度。

不同维度不能相加为一个虚假的“Token 利用率”。例如并发、RPM 和月度费用是不同约束，必须分别展示，综合状态只说明哪个约束最先饱和。

### 9.3 订阅与不透明额度

如果 Provider 只表示“订阅”“不限量”或不公开剩余额度：

- 可以记录请求、Token、并发、失败率和已观测窗口。
- 可以显示历史相对负载和限流发生时间。
- 不得推导精确总额度、剩余 Token 或采购节省。
- `-1` 等特殊值必须保存原始语义，不能当成负余额或无限可调度容量。
- 建议只能表述为“按历史负载推测可能需要验证/扩容”，并标记低置信度。

## 10. 容量建议

V5 只生成建议，不自动修改供应或预算。

### 10.1 建议类型

| 建议 | 触发证据 | 禁止条件 |
| --- | --- | --- |
| 增加容量 | 持续高水位、容量拒绝、SLO 受损、备用来源不足 | 数据窗口不足、故障尚未分类、Policy 主动限流 |
| 暂缓扩容 | 高峰短暂且可由现有 Fallback 覆盖 | 已发生持续拒绝或单点风险 |
| 调整 Route | 存在健康闲置容量且能力/地区/Policy 等价 | 模型质量或协议能力不可比 |
| 调整限额 | 某应用异常占用共享供应 | 不能把组织预算不足伪装成技术容量问题 |
| 下调或移除供应 | 长期空闲且不存在容灾、地区或专属用途 | 证据未知、处于季节性低谷、承担必要备用 |

### 10.2 证据门槛

每条建议必须包含：

- 时间窗口、样本数、峰值和趋势。
- 受影响 Application、Model、Route 与 SLO。
- 主要约束是 RPM、TPM、并发、费用、健康还是 Policy。
- 建议动作、预计收益、风险和撤销方式。
- 置信度与导致置信度下降的缺失事实。
- 如果不采取动作，预计影响何时出现。

建议默认 `observe_only`。涉及 Route、额度、预算和 Provider Account 的写操作继续走现有确认、版本 CAS 和审计路径。

## 11. 身份、应用与凭据治理

V5 不新增 同类项目 式 `Project` 事实源。团队用量归属复用 Application、Principal、Department、Group 和 Cost Center：

- 人员交互优先使用 User Credential 或归属明确的 Application Credential。
- CI、Agent 和后台任务使用 Service Principal，不绑定可能离职的自然人。
- 共享 Application Key 必须有 Owner、用途、环境、到期和轮换责任人。
- 默认 Scope 只允许列出授权模型和调用目标协议；Job/Artifact 等能力按需增加。
- Key 创建后完整明文只展示一次；数据库只保留 Hash/Fingerprint。
- 用户禁用、部门调整、角色变化和 Key 撤销必须在所有实例的下一次请求生效。
- 普通用户不能看到其他部门的 Key、Usage、Provider、采购价或审计明细。

V5 可以提供“团队应用”体验，但它只是现有事实的组合投影，不能创建第二套成员、权限、额度和账务模型。

## 12. 可信供应与模型身份表达

AsterRouter 不能对第三方模型身份做密码学鉴真。V5 对外表达必须准确：

- 企业配置并批准了哪个 Provider Connection。
- 使用了哪个 Provider Account、上游模型和 Route。
- 凭据类型、配置来源和最近验证时间。
- 是否发生 Fallback、协议转换或能力过滤。
- Provider 返回了什么稳定请求标识和用量事实。
- 哪些信息来自管理员声明，哪些来自公开 API，哪些仍未知。

“可信模型”表示可信边界、配置和调用链路可核对，不表示 AsterRouter 能证明供应商内部权重、训练版本或未公开降级行为。

## 13. 多实例与部署

### 13.1 V5 部署梯度

| 形态 | 依赖 | 适用范围 |
| --- | --- | --- |
| 开发 | Memory Repository | 单进程、本地功能开发，不保留生产状态 |
| 单机生产 | AsterRouter + PostgreSQL | 小团队、可接受维护窗口的稳定负载 |
| 双实例生产 | 反向代理 + 2 个 AsterRouter + PostgreSQL | V5 企业文本与 Embedding 主链路基线 |
| 扩展形态 | 多实例 + PostgreSQL + 可选 Redis/对象存储 | 更高并发、亲和、Durable 和多模态工作负载 |

V5 不引入 SQLite 作为第二个生产数据库。保持 PostgreSQL 单一生产事实源比追求更少容器更重要。

### 13.2 双实例正确性

双实例验收必须证明：

- Migration 只有一个 Owner，其他实例安全等待。
- Key 创建、轮换、撤销和 Policy 变更跨实例立即生效。
- RPM、TPM、并发和 Provider Capacity 不因副本数增加而超卖。
- Route 发布、熔断、冷却、半开探测与恢复状态一致。
- Usage、Cost、Trace、Attempt 和 Audit 不重复、不丢失。
- 流式请求在单实例退出后明确终止，不伪装为成功，也不自动重复上游调用。
- 新请求由健康实例继续服务；数据库不可用时 Readiness 失败并停止接收新流量。
- 可选 Redis 故障时按能力选择保守降级或停止派发，不改变 PostgreSQL 财务事实。

## 14. 数据与 API 增量

### 14.1 优先复用的事实

V5 首先复用：

- `GatewayPrincipal`、`APIKey`、`GovernancePolicy`。
- `GatewayModel`、`ModelRoute`、`ProviderAccount`。
- `AIOperation`、`AIAttempt`、`UsageRecord`、`Trace`、`Audit`。
- Provider Billing Snapshot、Effective Pricing 与健康证据。
- Department、Group、User、Cost Allocation。

### 14.2 最小新增投影

| 投影 | 用途 | 约束 |
| --- | --- | --- |
| `CompatibilityRecord` | 记录客户端/SDK 与协议能力验证结果 | 优先由 CI 生成，不作为运行时权限事实 |
| `SupplyUtilizationWindow` | 保存可重复计算的窗口聚合 | 原始 Usage/Attempt/Capacity 仍是事实源 |
| `CapacityRecommendation` | 保存只读建议、证据摘要和生命周期 | 不能直接修改 Route、Account、Policy 或预算 |

客户端配置默认按当前 Application、Model 和 Compatibility Record 动态生成，不持久化包含明文 Key 的 Configuration Bundle。

### 14.3 公开 API 增量

- `POST /v1/messages/count_tokens`
- `POST /v1/embeddings`

### 14.4 控制面 API 草案

- `POST /api/v1/onboarding/sessions`
- `POST /api/v1/onboarding/sessions/:id/model-source`
- `POST /api/v1/onboarding/sessions/:id/published-model`
- `POST /api/v1/onboarding/sessions/:id/api-key`
- `POST /api/v1/onboarding/sessions/:id/verification`
- `GET /api/v1/onboarding/compatibility-records`
- `GET /api/v1/admin/api-keys/:id/client-config?client=codex|claude_code|openai_sdk|anthropic_sdk`
- `POST /api/v1/admin/api-keys/:id/client-verifications`
- `GET /api/v1/supply/utilization`
- `GET /api/v1/supply/recommendations`

实现前必须先核对 `docs/refactor/v1/07-领域投影与API重构.md`，复用其命名、幂等和返回投影；本文不授权创建重复 Endpoint。

## 15. 实施里程碑

里程碑是依赖顺序，不是日期承诺。上一阶段未达到退出条件时，不进入下一阶段的自动化写路径。

### M0：事实收敛与基线

交付：

- 生成当前协议、Provider、客户端和部署能力矩阵。
- 修复 README、UI、API 文档与实际 Endpoint 的矛盾。
- 建立 Time to First Governed Call、配置失败、容量拒绝和协议错误基线。
- 明确 V5 Feature Flag、迁移策略和回滚点。
- 建立消费者订阅非目标与市场文案守卫。

退出条件：

- 每项“当前支持”都有代码、测试或 Release 证据。
- 未交付能力在 UI 和文档中明确标记。
- 没有把 `subscription` 账务语义描述成消费者订阅账号接入。

### M1：统一接入中心

**实施状态：P0 已完成。** 接入主体复用现有 API Key 事实源，界面中的 Application 仅作为组合投影，不新增成员、权限、额度或账务边界。

交付：

- Application、Published Model 和 Credential 首日流程。
- Codex、Claude Code、OpenAI SDK、Anthropic SDK 配置生成。
- 安全 Key 注入、真实请求验证、错误解释和恢复步骤。
- 接入 Session 幂等恢复，失败不残留不可见半成品。

退出条件：

- 新用户可以从空实例完成首次受治理调用。
- 每个客户端均有自动化浏览器和 API 旅程。
- 生成配置不包含 Provider Secret，默认不持久化下游 Key。

### M2：协议完整性

**实施状态：P0 已完成。** Count Tokens、Embedding、版本化 `CompatibilityRecord` 与支持窗口守卫均已交付；官方客户端/SDK 固定版本运行矩阵作为后续证据增强，不改变当前 `protocol_mock` 的诚实标注。

交付：

- Anthropic Count Tokens。
- Canonical Embedding Pipeline 与 OpenAI 兼容 Endpoint。
- 最新与前一主要客户端/SDK 版本的 Compatibility Record。
- 协议错误、Usage、Cost 和 Trace 一致性。

退出条件：

- 不支持的字段明确失败，不静默丢弃。
- Count Tokens 不生成推理账单。
- Embeddings 的模型、维度、Usage 和价格可追溯。

### M3：供应利用率与容量建议

**实施状态：P0 已完成。** 条件性的策略模拟继续保留在 P2，不影响本里程碑的 P0 退出判定。

交付：

- Supply Utilization Window 和角色范围投影。
- Account、Route Group、Published Model、Application 维度视图。
- Unknown/Stranded Capacity 与证据新鲜度。
- 只读容量建议；Route/预算模拟按 P2 条件交付。

退出条件：

- 任一综合状态都能下钻到原始证据。
- 不透明额度不会显示伪精确利用率。
- 建议在数据不足、故障未分类或模型不可比时保持 `inconclusive`。

### M4：双实例与生产强化

**实施状态：第二阶段实施中，里程碑尚未完成。** 已建立 PostgreSQL 共享容量与迁移互斥基础，提供双实例反向代理模板、运行手册和容量指标，并通过跨实例凭据旅程、Application/Tenant 聚合容量、64 路容量竞争、双 Service PostgreSQL 30 分钟持续负载、流中断终态、真实网络中断恢复、备份恢复和 `v0.17.0` Schema 升级演练；HA 容器故障切换与 Release Journey 门禁已实现，等待候选 SHA 环境证据后才能完成里程碑。

交付：

- PostgreSQL 双实例部署模板、反向代理和健康检查。
- 跨实例 Key/Policy/Capacity/Circuit/Usage 正确性。
- 负载、故障注入、备份恢复、升级和回滚证据。
- Prometheus 指标、告警规则和生产 Runbook。

退出条件：

- 并发压测不突破账号、应用和租户硬上限。
- 单实例退出不影响新请求继续进入健康实例。
- 不发生重复结算、重复 Fallback 或静默成功。

### M5：发布与证据闭环

交付：

- 安装、接入、治理、容量规划和故障恢复文档。
- 当前版本兼容矩阵和已知限制。
- Release Journey、升级前检查和诊断包。
- 产品文案、示例成本与实际能力最终复核。

退出条件：

- 所有 P0 指标达到门槛。
- 生产文档和界面没有路线图能力冒充当前能力。
- Release 可以从上一稳定版本升级并完成回滚演练。

## 16. 验收指标

### 16.1 产品指标

| 指标 | V5 门槛 |
| --- | --- |
| Time to First Governed Call | 标准路径 P50 不高于 10 分钟，P90 不高于 20 分钟 |
| 首次闭环完成率 | 不低于 85% |
| 标准路径无外部文档完成率 | 不低于 80% |
| 客户端配置首次验证成功率 | 在受支持环境中不低于 90% |
| Provider 变更时应用零改动比例 | 已发布稳定模型场景达到 100% |
| 容量建议证据可下钻率 | 100% |

### 16.2 协议指标

- Codex、Claude Code、OpenAI SDK、Anthropic SDK 的最新与前一主要版本通过契约测试。
- 支持能力无静默字段丢失；不支持能力返回稳定错误码。
- 每个成功推理请求都有 Request ID、最终 Route、Usage 状态和 Trace。
- Count Tokens 的计数来源可解释且不产生推理 Cost。
- Embeddings 的输入项数、维度和 Usage 结算通过边界测试。

### 16.3 容量与可靠性指标

- 64 路并发测试不突破配置的 Application、Tenant 和 Provider Account 并发上限。
- RPM/TPM 窗口在双实例下不因本地计数产生超卖。
- 熔断半开只允许受控探测，重复失败按策略重新冷却。
- 30 分钟持续负载后无 Permit、Lease、Usage 或 Billing Hold 泄漏。
- 数据库失联时实例在健康阈值内退出 Readiness，不继续接受无法正确计量的新请求。
- 容量窗口缺失或陈旧时不产生高置信度扩容/缩容建议。

### 16.4 安全指标

- Provider Secret、下游完整 Key、OAuth Token、Prompt 和 Response 不进入默认指标、审计摘要、诊断包或客户端配置。
- Key 撤销、用户禁用和角色变化在双实例的下一次请求生效。
- 普通成员无法枚举 Provider Account、其他部门 Usage 和采购价。
- 所有 Route、预算、容量和 Credential 写操作都有 Actor、影响、版本和结果审计。
- 请求 Payload 记录默认关闭；显式启用必须具备用途、范围、保留期和访问审计。

## 17. 测试策略

| 层级 | 重点 |
| --- | --- |
| 单元测试 | Token 计数、Embedding 校验、利用率公式、建议门禁、配置脱敏 |
| 契约测试 | OpenAI/Anthropic 协议、客户端版本、Provider Adapter 能力与错误映射 |
| Repository 测试 | 窗口聚合、CAS、租约、跨实例限额、撤销和幂等 |
| 集成测试 | PostgreSQL、双后端、反向代理、Mock/授权 Live Provider |
| 浏览器测试 | 首日闭环、角色可见性、配置生成、真实验证、恢复流程 |
| 并发与故障测试 | 容量超卖、实例退出、数据库抖动、熔断、流中断和重复请求 |
| 安全测试 | Secret 泄漏、越权查询、Key 冲突、SSRF、日志/导出脱敏 |
| Release Journey | 新装、升级、回滚、备份恢复、诊断和兼容矩阵发布 |

Live Provider 测试必须使用明确授权的测试账号、预算上限和脱敏证据。CI 默认依赖 Mock/兼容测试，不要求开发者提供个人订阅账号。

## 18. 风险与缓解

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 为追赶竞品引入消费者订阅连接器 | 条款、安全和维护边界失控 | 维持 V5 非目标和独立决策门 |
| V5 再次扩成全场景项目 | 首日闭环延期 | P0 优先，P2 不阻塞，新增能力必须关联验收指标 |
| 利用率公式掩盖未知容量 | 错误采购与误导节省 | Unknown/Stranded 独立建模，所有建议携带证据等级 |
| 客户端版本快速变化 | 配置失效 | Compatibility Record、最新+前一版本支持窗口、可回滚模板 |
| Count Tokens 结果不精确 | 上下文规划和费用判断错误 | 只接受精确来源，否则返回不支持 |
| Embedding 复用 Chat 模型 | 协议和计量污染 | 独立 Canonical Pipeline 与 Capability 门禁 |
| 多实例增加并发错误 | 超卖、重复结算 | PostgreSQL 原子约束、Lease/Fence、故障注入与 Remote Test |
| 新投影成为第二事实源 | 数据漂移 | 可重复计算、记录来源版本、原始事实优先 |
| 产品文案夸大模型真实性或节省 | 信任损失 | 允许/禁止表述清单和 Release 前事实复核 |

## 19. 对外表达边界

### 19.1 可以表达

- 使用企业自己批准和配置的 Provider。
- 员工不接触上游凭据，只使用按应用分配的 AsterRouter Credential。
- 请求 Route、用量、成本和错误可以追溯。
- 多个已授权 Provider Account 可以在策略、容量和健康约束下统一调度。
- 系统用真实使用数据帮助判断是否需要扩容。
- AsterRouter 支持私有化部署，数据和凭据由企业控制。

### 19.2 不能表达

- “三个消费者订阅账号一定可以供二十人无限使用”。
- “保证绕过供应商限额、风控或并发限制”。
- “AsterRouter 可以密码学证明上游一定使用了某个模型”。
- “所有闲置 Token 都能自动转给其他成员”。
- “使用网关天然符合所有 Provider 服务条款”。
- 在没有同等模型、同等请求和有效价格证据时宣称节省百分比。

成本案例必须列出假设、供应类型、时间窗口、并发、失败率、价格来源和不确定性。`600 x 1.x` 之类不可审计的表达不能进入正式产品材料。

## 20. 工程任务分解

### Backend

- 协议能力注册与 Compatibility Record 输出。
- Anthropic Count Tokens Handler、Service、Adapter Capability 和审计。
- Canonical Embedding Request/Result、Route 门禁、Usage 与 Cost。
- Supply Utilization 聚合查询和窗口持久化。
- Capacity Recommendation 规则、证据和 `inconclusive` 状态。
- 双实例 Capacity、Quota、Circuit、撤销和 Usage 一致性。
- 客户端验证 API 与稳定错误目录。

### Frontend

- Application 首日闭环和接入中心。
- Codex、Claude Code、SDK 配置生成、脱敏与恢复视图。
- 真实验证状态、Request ID 和错误动作。
- 供应利用率、Unknown/Stranded Capacity 和建议证据视图。
- 角色范围、部门隔离和高风险动作确认。
- 删除或修正与真实协议能力矛盾的旧文案。

### Deployment

- 双后端 + PostgreSQL + 反向代理部署模板。
- Readiness、优雅终止、连接池和 Migration Owner。
- 多实例压测、故障注入、备份恢复与升级 Runbook。
- Prometheus 指标、基础告警和容量看板模板。

### Documentation

- 客户端兼容矩阵。
- Codex、Claude Code、OpenAI SDK、Anthropic SDK 接入与恢复指南。
- Embeddings、Count Tokens 和错误参考。
- 供应利用率与容量建议口径。
- 单机到双实例升级、故障处理和安全清单。
- 市场表述事实检查表。

## 21. Definition of Done

V5 只有同时满足以下条件才算完成：

1. 管理员可以从空实例完成一个真实客户端的首次受治理调用。
2. Codex、Claude Code 和两类官方 SDK 具备版本化兼容证据。
3. Count Tokens 与 Embeddings 进入统一 Credential、Policy、Route、Usage、Trace 和 Audit 边界。
4. 管理员能够区分供应饱和、策略拒绝、故障、闲置、不可用和未知容量。
5. 容量建议只读、可解释、可反驳，不自动采购或修改高风险配置。
6. PostgreSQL 双实例下不超卖容量、不重复结算，撤销和 Policy 变更及时生效。
7. 普通成员无法接触上游凭据或越权查看其他组织范围。
8. 文档、UI 和 API 对当前能力的表达一致，没有把路线图冒充 Release。
9. 没有引入消费者订阅 Session/Cookie/OAuth 自动化或第二套 Project/Key/Usage 事实源。
10. 所有新增路径通过风险匹配的单元、契约、浏览器、并发、安全和 Release Journey 验收。

## 22. 与既有文档的关系

- [V4 总体路线图](../v4/README.md)：继续定义 AI Access Supply Platform、Direct/Durable、多模态、Artifact 和插件边界。
- [产品定位](../../product-positioning.md)：继续定义标准 Provider、业务场景和信任边界。
- [体验重构](../../refactor/v1/README.md)：定义 Application、Published Model、首日闭环和产品表面收敛。
- [目标架构](../../goal/README.md)：定义系统长期不变量、领域事实、多实例、安全、SRE 和验收。
- [有效价格](../effectivepricing/README.md)：定义价格证据、缓存亲和和经济切换，不由 V5 复制。
- [省钱系统](../../savemoney/README.md)：定义可验证节省与成本调度；V5 只补充团队供应利用率和扩容决策入口。

若本文与上述长期事实冲突，必须先修正本文或发起架构决策，不允许实现阶段静默选择其中一套。
