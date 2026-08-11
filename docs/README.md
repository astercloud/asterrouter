# AsterRouter 企业产品与策略重构蓝图

> 决策状态：`CURRENT`，AsterRouter 唯一产品与重构事实源
> 实现状态：`TARGET`，未完成项不得作为已交付能力对外宣传
> 适用范围：产品、设计、前端、后端、测试、交付与国际化
> 更新日期：2026-08-10

## 1. 一页结论

AsterRouter 只服务企业 AI 使用场景，定位为可私有部署的企业 AI 网关与策略控制平台。它连接企业应用、员工与已授权的 AI 供应商，统一完成接入、访问控制、智能路由、成本治理、用量分析和审计。

本次重构锁定以下决策：

1. 只保留企业产品，不再提供 Personal、Relay Operator 或其他安装模式。
2. 只保留两个用户界面：一个管理控制台和一个受限服务门户。
3. 原 Platform 能力并入“应用接入”，用于企业连接内部应用、SaaS、OEM 或合作方系统，不再形成独立后台。
4. 删除客户余额、套餐、充值、转售定价、兑换码和中转风控等运营领域。
5. 策略成为一级产品能力，统一分为“访问策略”和“路由策略”。
6. 中文文案按中国企业用户的表达习惯设计，英文及其他语言独立产品化表达，不做逐字翻译。
7. 软件尚无存量用户，不保留旧 Profile、Surface、路由或数据模型兼容层。

一句话事实源：

> AsterRouter 后续只向“企业组织 + 统一管理控制台 + 受限服务门户 + 访问策略 + 路由策略”演进；个人模式、中转运营模式和多套 Surface 均为 `dead`，禁止恢复。

## 2. 治理状态

| 分类 | 内容 | 规则 |
| --- | --- | --- |
| `current` | 企业组织、应用接入、模型供应、API 凭据、访问治理、路由、用量、成本、审计 | 唯一允许继续演进的产品主链 |
| `compat` | 无 | 无存量用户，不建立兼容层 |
| `deprecated` | 无 | 需要退出的内容直接删除，不长期挂起 |
| `dead` | Personal、Relay Operator、独立 Platform、Customer Portal、六套 Surface、Profile 安装选择、客户余额/套餐/充值/风控 | 删除代码、接口、数据表、测试与文案，禁止新增引用 |

现有代码中的四种 Profile 和六套 Surface 只是待删除实现，不再构成产品事实。重构期间若代码与本文冲突，以本文为目标决策，但对外只能声明已经通过验收的能力。

## 3. 产品定位

### 3.1 产品定义

AsterRouter 是面向企业的平台基础设施，位于企业调用方与 AI 供应商之间：

```text
企业员工 / 内部应用 / 外部业务系统
                 |
                 v
       AsterRouter 企业 AI 网关
  身份 -> 访问策略 -> 路由策略 -> 调度
                 |
                 v
        已授权的 AI 供应商与账号
```

它解决六个企业问题：

- 统一接入：企业应用只连接一个稳定的 Gateway API。
- 权限治理：明确谁、通过哪个应用、可以使用哪些模型和能力。
- 稳定供给：在健康、容量和合规约束内选择供应线路并故障切换。
- 成本控制：限制预算和单次成本，并在合格线路中执行成本优化。
- 证据闭环：记录每次访问决策、路由选择、用量、成本和审计事实。
- 私有部署：供应商凭据、请求证据和企业身份保持在企业控制边界内。

### 3.2 目标用户

| 用户 | 核心任务 |
| --- | --- |
| 企业管理员 | 初始化组织、安全和系统边界 |
| AI 平台管理员 | 管理应用、模型服务、供应线路和策略 |
| 安全管理员 | 管理身份、权限、数据处理和审计 |
| 财务与成本负责人 | 管理预算、成本归集、异常和节省证据 |
| 应用负责人 | 为所属应用申请模型、凭据和策略，查看用量 |
| 开发者与员工 | 在授权范围内获取接入信息并查看个人或所属应用用量 |
| 审计员 | 只读查看策略版本、变更记录、调用证据和导出结果 |

### 3.3 明确边界

AsterRouter 不负责：

- 面向个人用户的轻量代理或个人工作台。
- AI 中转销售、客户充值、套餐、余额、兑换和转售风控。
- 接管外部 SaaS/OEM 的最终用户、登录、订阅、订单和支付。
- 未授权账号、浏览器 Cookie、逆向协议或非公开 Provider API。
- 让插件绕过策略直接选择供应账号、签发凭据或修改用量事实。

## 4. 对标取舍

AKRouter 值得借鉴的是把“策略”做成用户可以理解、配置、验证和解释的产品，而不是它围绕商户与线路市场建立的业务维度。

| 维度 | 借鉴 | AsterRouter 的企业化优化 |
| --- | --- | --- |
| 准入条件 | 请求进入策略前先判断资格 | 增加组织、部门、应用、调用身份、数据区域和合规约束 |
| 成本限制 | 支持价格上限和低成本偏好 | 区分硬预算、预估成本、实际成本、成本中心和节省证据 |
| 可选线路 | 显式选择候选线路 | 候选来自企业已批准的模型服务、供应商、账号与区域 |
| 线路分组 | 复用线路集合 | 线路组只描述供应集合，不承载权限或计费逻辑 |
| 调度偏好 | 成本、稳定性、优先级可配置 | 硬约束与软偏好分离，避免权重掩盖合规和容量限制 |
| 故障切换 | 失败后尝试备用线路 | 明确提交点、幂等性、最大尝试次数和不可重试错误 |
| 策略解释 | 展示选择结果 | 同时展示匹配范围、排除原因、价格版本和最终决策 |
| 策略试算 | 发布前预览 | 支持真实对象上下文、历史请求回放和差异比较 |

不借鉴以下内容：

- 以商户、余额或转售套餐为产品根对象。
- 用多套后台区分同一组 Provider、Model、Route 和 Usage 能力。
- 把线路价格当作唯一决策变量。
- 让模糊的加权总分越过权限、预算、合规或健康硬约束。

## 5. 产品形态

### 5.1 一个管理控制台

统一入口为 `/console`。管理员看到什么由 RBAC 和资源范围决定，不再通过 Profile 或 Surface 切换产品。

```text
管理控制台
├── 工作台
├── 应用接入
├── 模型服务
├── 策略管理
├── 用量与成本
├── 组织与权限
└── 系统管理
```

一级导航职责：

| 导航 | 回答的问题 | 主要对象 |
| --- | --- | --- |
| 工作台 | 今天是否正常，有什么必须处理 | 待办、告警、异常、最近变更、接入进度 |
| 应用接入 | 谁在调用，如何安全接入 | 应用、调用身份、访问凭据、外部集成 |
| 模型服务 | 企业提供哪些模型，从哪里获得供应 | 已发布模型、供应商、供应账号、供应线路、线路组 |
| 策略管理 | 谁能用什么，以及请求如何选路 | 访问策略、路由策略、绑定、试算、版本 |
| 用量与成本 | 谁用了多少、花了多少、是否异常 | 用量、成本、预算、成本中心、Trace、导出 |
| 组织与权限 | 谁可以查看和管理哪些资源 | 成员、部门、用户组、角色、授权 |
| 系统管理 | 实例如何运行和扩展 | 身份源、插件、通知、存储、备份、审计、系统设置 |

设计规则：

- 一级导航按企业任务组织，不按数据库表或后端模块组织。
- Provider Account、Attempt、Trace Event 等工程对象放在详情或诊断层。
- 同一业务事实只有一个主要修改入口。
- 无权限的导航不展示，服务端仍必须逐请求鉴权。
- 平台集成是“应用接入”的一种接入方式，不产生 `/platform` 后台。

### 5.2 一个服务门户

受限入口为 `/portal`，服务企业员工、开发者和应用负责人：

```text
服务门户
├── 概览
├── 我的应用
├── 接入信息
├── 用量
└── 账户
```

门户是管理控制台中同一事实的权限投影，不复制 Application、Credential、Policy 或 Usage 业务逻辑。门户不展示供应账号、全局线路、其他部门成本或系统设置，也不包含客户账单、余额和充值。

### 5.3 安装与启动

新安装只初始化企业实例，不再询问部署形态。初始化流程固定为：

```text
数据库与密钥
  -> 企业基本信息
  -> 初始企业管理员
  -> 登录与安全设置
  -> 创建首个应用
  -> 配置模型供应
  -> 发布首个策略
  -> 发起验证请求
```

环境变量和数据库中不再保存 `enabled_profiles`、`default_profile` 或 `deployment_role`。安装完成只记录 setup 状态、组织根实体和初始管理员。

## 6. 应用接入模型

“应用”是所有 AI 调用关系的产品根对象。它可以代表内部服务、员工工具、自动化 Agent、企业 SaaS 产品或合作方系统。

```text
企业组织
  └── 应用
      ├── 应用负责人
      ├── 调用身份
      ├── 访问凭据
      ├── 可用模型
      ├── 访问策略绑定
      ├── 路由策略绑定
      └── 用量与成本归属
```

接入方式只描述凭据来源，不创建产品模式：

| 接入方式 | 适用场景 | 身份事实源 |
| --- | --- | --- |
| API Key | 企业后端、脚本、服务和开发环境 | AsterRouter 管理调用身份与密钥 |
| 企业登录 | 员工直接使用门户或内部工具 | 企业 IdP 与 AsterRouter 组织映射 |
| 委托身份 | SaaS、OEM 或合作方保留自己的最终用户体系 | 外部系统签发短期上下文，AsterRouter 只接收最小授权事实 |

外部系统的用户、会话、订阅、订单和支付始终留在外部系统。AsterRouter 仅保存稳定的接入方、调用身份引用、策略绑定和用量归属。

## 7. 策略体系

### 7.1 为什么只有两类策略

策略只回答两个不同问题：

1. 访问策略：这个请求是否允许，以及允许到什么边界。
2. 路由策略：在允许的供应范围内，应该选择哪条线路。

二者使用统一的版本、发布、绑定、试算、解释和审计机制，但不能混成一个不可解释的巨型规则。访问策略先执行，路由策略不得放宽访问结果。

```text
身份解析
  -> 访问策略：允许 / 拒绝 / 限制
  -> 候选生成：能力、区域、合规、价格、健康、容量
  -> 路由策略：排序与故障切换
  -> 账号调度与执行
  -> 用量、成本、Trace、审计
```

### 7.2 访问策略

访问策略包含：

- 适用范围：组织、部门、用户组、应用、调用身份或访问凭据。
- 模型权限：允许或禁止的模型服务、能力和模态。
- 工具权限：Tool Call、图片输入、联网访问和其他高风险能力。
- 速率限制：QPS、RPM、TPM、并发和任务队列上限。
- 用量额度：周期 Token、请求次数、媒体数量或时长。
- 预算限制：周期预算、单次预估成本上限和超限动作。
- 数据治理：Prompt/Response 记录模式、脱敏、保留期和允许区域。
- 时间条件：生效区间和可选工作时间窗口。

访问策略采用“上层设边界，下层只能收紧”的规则：

```text
组织基线
  -> 部门或用户组
  -> 应用
  -> 调用身份或凭据
```

合并语义必须确定且可解释：禁止集合取并集，允许集合取交集，数值上限取最小正值，数据保留和日志规则取更严格值。任何层级都不能通过空值意外放宽上级限制。

### 7.3 路由策略

路由策略包含七个区块：

1. 匹配条件：应用、模型服务、能力、区域、请求标签和时间窗口。
2. 硬性限制：允许供应商、允许区域、最大预估成本、价格新鲜度和合规标签。
3. 可选线路：明确的供应线路或可复用线路组。
4. 调度偏好：综合优选、成本优先、稳定优先或固定顺序。
5. 容量规则：并发、RPM/TPM、账号配额、冷却和排队边界。
6. 故障切换：最大尝试次数、可重试错误、备用顺序和总时间上限。
7. 一致性：可选的会话亲和、区域亲和和缓存复用条件。

硬性限制先过滤候选，调度偏好只对合格候选排序。不能用权重让低价线路越过权限、区域、健康、容量或预算限制。

内置调度偏好使用中国用户易理解的名称：

| 中文名称 | 英文名称 | 行为 |
| --- | --- | --- |
| 综合优选 | Balanced | 在稳定性、成本和时延目标内选择综合最优线路 |
| 成本优先 | Cost First | 在质量与稳定性底线内选择预计成本最低线路 |
| 稳定优先 | Reliability First | 优先成功率、健康状态和可用容量 |
| 固定顺序 | Fixed Order | 按管理员明确顺序尝试，适合强控制场景 |

“综合优选”不是不透明分数。每个参与维度、归一化方式、缺失值处理和最终排序都必须能在决策说明中查看。

### 7.4 策略绑定与优先级

- 访问策略允许按层级叠加，但只允许收紧。
- 每个“应用 + 模型服务”在同一时间只选择一个已发布路由策略。
- 未显式绑定路由策略时使用组织默认策略。
- 多个路由策略同时匹配时，按明确优先级、范围具体度、发布时间和稳定 ID 决胜，禁止依赖数据库返回顺序。
- 线路组只是候选集合，不嵌套策略，不形成循环依赖。

### 7.5 策略生命周期

```text
草稿 -> 校验 -> 试算 -> 发布 -> 生效
                    |        |
                    |        -> 新版本 / 回滚
                    -> 放弃
```

每次发布必须生成不可变版本，并记录发布人、发布时间、变更说明、影响范围和试算摘要。正在生效的版本不能原地修改；回滚本质上是重新发布一个已知版本。

### 7.6 策略试算

试算必须在发布前回答：

- 请求匹配了哪些访问策略，最终允许还是拒绝。
- 哪些限制来自组织、部门、应用或凭据。
- 生成了哪些供应候选，哪些被排除以及原因。
- 每条候选使用的价格版本、健康和容量状态。
- 最终选择哪条线路，预计成本和备用顺序是什么。
- 与当前已发布版本相比，结果发生了什么变化。

支持两类输入：手工构造的请求上下文，以及经过脱敏的历史 Trace 回放。试算只读，不消耗真实 Provider 配额，不写正式 Usage 或 Billing。

### 7.7 决策说明

每次真实请求至少保存以下决策快照：

- 组织、应用和调用身份引用。
- 访问策略 ID 与版本、限制合并结果。
- 路由策略 ID 与版本、匹配规则。
- 候选线路、排除原因和排序依据。
- 价格快照版本、预计成本和实际成本。
- 最终线路、尝试序列、故障切换原因和执行结果。

产品界面使用“决策说明”，工程诊断中可以使用 `Policy Decision`、`Route Candidate` 和 `Attempt` 等精确术语。

## 8. 领域模型

### 8.1 核心领域

| 领域 | 核心实体 | 事实所有者 |
| --- | --- | --- |
| 组织与身份 | Organization、Department、Group、User、RoleBinding | 组织服务 |
| 应用接入 | Application、Principal、Credential、ExternalIntegration | 接入服务 |
| 模型供应 | ModelService、Provider、ProviderAccount、SupplyRoute、RouteGroup | 供应服务 |
| 策略 | AccessPolicy、RoutingPolicy、PolicyVersion、PolicyBinding | 策略服务 |
| 网关执行 | RequestContext、CandidatePlan、Attempt、Artifact、Job | 网关核心 |
| 用量与成本 | UsageRecord、CostRecord、BudgetState、Allocation | 计量与成本服务 |
| 证据与运维 | Trace、Alert、AuditEvent、ExportJob | 可观测与审计服务 |
| 扩展 | Plugin、Integration、Entitlement、RuntimeState | 扩展服务 |

### 8.2 删除的领域

以下对象不迁移、不重命名，直接删除：

- Relay Customer、Customer Group。
- Plan、Balance、Recharge、Redemption Code。
- Customer Charge Pricing Rule 和中转账单。
- Relay Risk Rule、Operator Notice。
- Personal Workspace 作为独立产品根。
- Profile、Surface、Profile Scope 作为产品隔离根。

企业内部成本分摊保留为 Cost Center / Allocation，不复用客户余额或转售账单模型。

### 8.3 多租户边界

AsterRouter 的顶层数据边界只有企业组织。需要连接外部 SaaS、OEM 或合作方时，在 Organization 下创建 Application 与 ExternalIntegration，并使用不可变的外部主体引用。不要为此恢复 Platform Profile 或建立第二套控制面。

## 9. 权限模型

建议的内置角色：

| 中文名称 | 英文名称 | 主要权限 |
| --- | --- | --- |
| 企业管理员 | Organization Administrator | 组织级设置和管理员委派，不默认读取敏感调用内容 |
| AI 平台管理员 | AI Platform Administrator | 应用、模型供应、路由和网关运行 |
| 安全管理员 | Security Administrator | 身份源、访问策略、数据治理和安全审计 |
| 成本管理员 | Cost Administrator | 预算、成本中心、价格和成本报表 |
| 应用负责人 | Application Owner | 管理授权应用的凭据、策略申请和用量 |
| 审计员 | Auditor | 只读访问审计、策略版本和导出 |
| 使用者 | User | 使用服务门户和被授权的应用资源 |

权限范围只允许 `organization`、`department`、`group`、`application` 和具体 `resource`。删除 `surface` 范围。全局管理员能力必须拆分为可审计权限，不以进入某个后台自动获得所有业务数据。

## 10. 路由与 API 目标

### 10.1 前端路由

```text
/console/workbench
/console/applications
/console/model-services
/console/policies/access
/console/policies/routing
/console/usage
/console/organization
/console/system
/portal/overview
/portal/applications
/portal/access
/portal/usage
/portal/account
```

删除 `/admin`、`/operator`、`/customer`、`/platform` 及其重定向。`/console` 不再表示 Personal，它是唯一管理控制台。

### 10.2 控制面 API

控制面 API 按领域组织，不按界面或角色复制：

```text
/api/v1/organizations/*
/api/v1/applications/*
/api/v1/model-services/*
/api/v1/supply/*
/api/v1/access-policies/*
/api/v1/routing-policies/*
/api/v1/usage/*
/api/v1/costs/*
/api/v1/audit/*
/api/v1/system/*
```

管理控制台和服务门户调用同一组领域服务，服务端根据身份与资源范围投影结果。删除 `/api/v1/admin/*`、`/api/v1/operator/*`、`/api/v1/customer/*`、`/api/v1/platform/*` 等平行控制面。

公开 Gateway API 继续使用 `/v1/*`，通过 Credential 或短期委托身份解析出 Organization、Application、Principal 和策略上下文。客户端不能通过 Header 或参数自行选择组织、策略或供应线路。

## 11. 中文命名与国际化

### 11.1 术语表

| 中文界面 | 英文界面 | 内部领域名 | 使用说明 |
| --- | --- | --- | --- |
| 企业 AI 网关 | Enterprise AI Gateway | gateway | 产品类别 |
| 管理控制台 | Management Console | console | 唯一管理入口 |
| 服务门户 | Service Portal | portal | 企业成员受限入口 |
| 工作台 | Workbench | workbench | 待办与运行结论，不叫 Dashboard |
| 应用 | Application | application | AI 调用关系的产品根 |
| 调用身份 | Principal | principal | 人或非人调用主体 |
| 访问凭据 | Credential | credential | API Key 或委托凭据 |
| 模型服务 | Model Service | model_service | 企业发布给应用的稳定模型能力 |
| 供应商 | Provider | provider | 上游 AI 服务提供方 |
| 供应账号 | Provider Account | provider_account | 受控上游凭据与容量单元 |
| 供应线路 | Supply Route | supply_route | 模型服务到供应账号的可执行路径 |
| 线路组 | Route Group | route_group | 可复用的供应线路集合 |
| 访问策略 | Access Policy | access_policy | 决定是否允许及限制边界 |
| 路由策略 | Routing Policy | routing_policy | 决定合格线路的选择和切换 |
| 策略试算 | Policy Simulation | policy_simulation | 发布前只读验证 |
| 决策说明 | Decision Explanation | decision_explanation | 解释访问与选路结果 |
| 用量与成本 | Usage & Cost | usage_cost | 企业消费和供应成本视图 |
| 组织与权限 | Organization & Access | organization_access | 成员、部门、角色和授权 |

界面不得继续出现 `Profile`、`Surface`、Personal、Relay Operator 或“中转运营”。`Tenant` 只在确有多租户协议语义的工程文档中使用，面向企业用户优先表达为“组织”“应用”或“接入方”。

### 11.2 i18n Key

Key 使用稳定领域语义，不包含页面版本、旧模式或中文拼音：

```text
nav.workbench
nav.applications
nav.modelServices
nav.policies
nav.usageCost
nav.organizationAccess
nav.system
policy.access.title
policy.access.limits
policy.routing.title
policy.routing.guardrails
policy.routing.candidates
policy.routing.preference
policy.routing.failover
policy.simulation.title
policy.explanation.title
```

禁止使用 `admin.*`、`operator.*`、`platform.*`、`personal.*` 作为新领域文案命名空间。组件名称、API 字段和埋点事件同样遵循领域语义。

中文和英文分别维护自然表达。例如：

| Key | `zh-CN` | `en-US` |
| --- | --- | --- |
| `policy.routing.preference` | 调度偏好 | Routing Preference |
| `policy.routing.guardrails` | 硬性限制 | Guardrails |
| `policy.simulation.run` | 开始试算 | Run Simulation |
| `policy.explanation.excluded` | 排除原因 | Exclusion Reason |

## 12. 从当前实现到目标实现

### 12.1 前端收敛

| 当前路径 | 目标动作 | 状态 |
| --- | --- | --- |
| `frontend/src/views/admin/*` | 按新导航迁入统一 Console，去掉 Admin 产品语义 | `current` 重构 |
| `frontend/src/views/console/*` | 重写为统一管理控制台 Shell，不保留个人模式 | `current` 重构 |
| `frontend/src/views/portal/*` | 保留为企业服务门户并复用领域 API | `current` 重构 |
| `frontend/src/views/platform/*` | Tenant/Integration 有效能力并入应用接入，其余删除 | `dead` 收口 |
| `frontend/src/views/operator/*` | 整体删除 | `dead` |
| `frontend/src/views/customer/*` | 整体删除 | `dead` |
| `frontend/src/router/surfaces.ts` | 删除，改为登录后基于权限进入 Console 或 Portal | `dead` |
| Setup 四模式选择 | 删除，改为企业初始化流程 | `dead` |

### 12.2 后端收敛

| 当前能力 | 目标动作 | 状态 |
| --- | --- | --- |
| Profile 配置、持久化与 Guard | 删除，不迁移数据 | `dead` |
| Surface 常量、授权范围与路由守卫 | 删除，改为领域权限与资源范围 | `dead` |
| Admin/Console/Platform 重复路由 | 收敛到领域 API 与同一 Service | `current` 重构 |
| Operator/Customer route/service/repository | 连同表结构、测试和文案整体删除 | `dead` |
| Platform Tenant/Principal/External Auth | 重命名并并入 Application/Principal/ExternalIntegration | `current` 重构 |
| GovernancePolicy | 拆分 AccessPolicy 与 RoutingPolicy，共享版本发布基础 | `current` 重构 |
| Provider/Account/Model/Route/Scheduler | 保留并按 ModelService/SupplyRoute 领域收敛 | `current` |
| Usage/Cost/Trace/Audit | 保留，删除 Profile/Customer 归属，统一到 Organization/Application/Principal | `current` 重构 |

### 12.3 数据原则

由于没有存量用户：

- 不编写 Personal、Relay 或 Customer 数据迁移。
- 不保留旧列、旧表、双写、别名、fallback 或兼容 API。
- 直接修改初始 Schema 和测试 Fixture，删除只服务旧模式的迁移脚本。
- Profile Scope 改为 Organization/Application 归属时，以新模型重建测试数据。
- 删除后增加仓库扫描守卫，阻止旧术语和路径回流。

## 13. 实施顺序

### Phase 1：删除产品分叉

- 删除 Personal、Relay Operator、Customer 的前后端入口、领域和测试。
- 删除 Profile 选择、Profile Settings、Surface Guard 和相关环境变量。
- 建立 `/console` 与 `/portal` 两个 Shell。
- 将现有 Enterprise 能力迁入统一 Console。
- 将有效 Platform 接入能力并入 Application。

验收：仓库除删除守卫外，不再出现旧模式 ID、旧路由或旧 i18n 命名空间。

### Phase 2：统一企业领域

- 建立 Organization、Application、Principal 和 Credential 的唯一归属链。
- 控制面 API 按领域收敛，门户只做权限投影。
- 重命名 Model/Provider/Route 的产品展示，不复制底层调度实现。
- Usage、Cost、Trace、Audit 统一携带 Organization/Application/Principal 快照。

验收：同一资源只有一个 Service、一个 Repository 事实源和一个主要修改入口。

### Phase 3：策略产品化

- 从 GovernancePolicy 提取访问策略。
- 建立路由策略、线路组、硬性限制和四种调度偏好。
- 建立统一版本、校验、试算、发布、回滚与决策说明。
- 将访问决策与候选规划接入真实 Gateway 主链。

验收：任一请求可以解释“为什么允许、为什么选择、为什么切换、花了多少”。

### Phase 4：成本与企业验收

- 接通价格新鲜度、预估成本、实际成本、预算和成本中心。
- 完成中英文术语、响应式、可访问性和关键企业旅程。
- 补齐 PostgreSQL、策略契约、网关回归和浏览器 E2E。
- 增加旧模式、旧路由、旧 API 与旧 i18n Key 的扫描门禁。

验收：企业从初始化到首个受策略治理的请求可以独立完成，旧产品分叉无法通过代码或配置恢复。

## 14. 产品验收标准

### 14.1 体验

- 登录后只进入管理控制台或服务门户，不选择工作台类型。
- 企业管理员可以在一个导航体系内完成应用、模型、策略、用量和组织管理。
- 应用负责人不需要理解 Provider Account、Surface 或 Profile 即可完成接入。
- 任一日常任务最多经过一个一级导航和一个对象详情。
- 中文界面没有生硬直译、混杂英文状态值或旧模式术语。

### 14.2 策略

- 访问策略和路由策略职责不重叠，执行顺序固定。
- 访问限制只能被下级收紧，不能意外放宽。
- 路由硬性限制与调度偏好分离。
- 发布前可以试算，发布后生成不可变版本并可回滚。
- 每个拒绝、排除、选择和故障切换都有明确原因。
- 价格过期、健康未知或容量不可用时按显式规则处理，不伪装成最优选择。

### 14.3 安全与数据

- 服务端按 Organization、Application 和 Resource 执行权限校验。
- Provider Secret 不进入浏览器、插件存储、日志或导出。
- 外部委托身份不能扩大 Integration 上限，也不能调用控制面。
- Usage、Cost、Trace 和 Audit 的归属一致且可对账。
- 删除中转运营后，不残留可写的 Customer、Balance、Plan 或 Risk 数据路径。

### 14.4 工程治理

- `rg` 扫描无法在生产代码中找到旧 Profile/Surface 路径和旧模式文案。
- 前端路由只有 Console、Portal、认证、初始化和合法公共页面。
- 控制面 API 不按 Admin、Operator、Platform 复制服务。
- 仓库测试不再构造 Personal、Relay、Customer 或 Surface Fixture。
- README、部署说明、配置示例、代码和测试使用同一企业产品口径。

## 15. 文档治理

本文是产品目标、体验架构和重构边界的唯一事实源。后续文档只允许补充以下内容：

- 已发布 API Reference。
- 与代码一致的部署和运维 Runbook。
- 面向具体版本的测试证据或 Release Notes。
- 不改变本文决策的领域级实现说明。

禁止再创建 `goal`、`refactor/v2`、`roadmap/v6` 等平级产品事实源。需要改变产品决策时，直接修改本文并在代码变更中同步验证；无效内容直接删除，不建立历史兼容章节。
