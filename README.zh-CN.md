<div align="center">

# AsterRouter

**让每一次 AI 调用更省钱，也更可控。**

统一接入多家 AI 供应商，自动优化成本，完成企业治理，并支持私有化与托管交付。

[English](./README.md) · [简体中文](./README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?style=flat-square)](./backend/go.mod)

</div>

## 一个网关，管好成本与访问

AsterRouter 当前为团队提供统一、可控的 AI 网关，并将逐步扩展为文本、图片、音频和视频的一体化能力层。接入已有供应商，为团队和产品安全分发访问能力，并清楚看到每一笔 Token 和费用去向；多模态路线图会把相同治理能力延伸到媒体任务与产物。

下一阶段不再停留在成本报表：价格插件持续同步供应商报价，成本感知调度在满足企业策略、健康状态和容量要求的前提下，自动选择成本更低的可用线路。

| | 你能获得什么 |
| --- | --- |
| 降低成本 | 比较合格供应商的有效价格，为每次请求选择更划算的可用线路。 |
| 统一接入 | 应用使用稳定的流式、媒体和长任务 API，供应商与线路变化留在网关内部。 |
| 稳定服务 | 自动绕过异常、限流或容量不足的线路，并执行失败切换。 |
| 企业治理 | 管理团队权限、模型范围、预算、告警、审计和数据保留。 |
| 灵活交付 | 可以自主部署，也可以选择私有化交付和持续托管运维。 |

## AsterRouter 如何省钱

1. **接入现有供应商。** 继续使用经过授权的供应商账号和标准 API。
2. **持续更新价格。** 价格源插件通过供应商 API、签名 Feed、文件导入或可选浏览器扩展同步报价。
3. **带约束地择优。** AsterRouter 先保证策略、可用性与容量，再从合格线路中选择预计成本最低的路径。
4. **证明节省结果。** 用量和路由证据展示选择价格、备选线路、实际成本与可量化节省金额。

> 报价驱动调度、第三方价格插件、节省账本和浏览器扩展属于路线图能力；路由、治理、用量、成本分摊、审计和插件底座已经可用。

## 为真实组织而设计

**研发团队**只需维护一个入口和一套 Key，即可使用多个供应商。

**财务与运营团队**可以按用户、部门、Group、Key 和模型核算成本，并配置预算与告警。

**安全团队**可以获得私有化部署、Secret 加密、权限隔离、审计证据、数据保留和离线运行能力。

**平台负责人**可以管理线路健康、失败切换、容量、备份、升级、诊断，以及承载供应商扩展的插件体系。

## 为内部团队和外部客户提供 AI 能力

AsterRouter 既可以支撑企业内部工具，也可以作为面向客户产品背后的 AI 能力供应层，不需要让每一个业务服务都重复建设 AI 网关。

| 场景 | AsterRouter 提供什么 |
| --- | --- |
| 企业内部 AI 平台 | 为员工、部门、服务和自动化任务提供带权限、额度与成本治理的 AI 访问。 |
| SaaS 与桌面客户端 | 保留平台现有客户登录，由 AsterRouter 在产品背后执行 AI 策略、额度、路由和用量。 |
| 内容与媒体产品 | 同时支持即时图片/音频响应、原生流式输出和排队长任务，不需要每个产品重复建设供应商容量调度、产物交付和媒体计费。 |
| AI API 与开发者平台 | 直接签发 AsterRouter API Key，用一个自有品牌入口提供多模型能力，不暴露上游凭据。 |
| 合作伙伴与 OEM | 隔离不同租户，执行合作伙伴策略，并把用量返回给掌握商业关系的业务系统。 |

```text
客户访问
  ├─ AsterRouter API Key
  └─ 原有平台登录与委托凭据
              ↓
       AsterRouter Gateway
              ↓
          AI 供应商
```

可以按产品选择接入方式。AI API、中转服务、Agent 平台或可信服务端集成可以直接由 AsterRouter 签发和治理 Bearer API Key；已经拥有用户、登录 Session、订阅和权益体系的 SaaS/OEM 产品，则继续掌握自己的用户体系，只把 AI 访问上下文委托给 AsterRouter。两种方式进入同一套策略、账号调度、流式处理、用量、成本和审计流水线，都不会把 Provider Secret 暴露给客户。

AsterRouter API Key、策略、路由、计量和隔离底座当前已经具备。AI Platform 同时支持可信产品后端签发的短期 HMAC 委托 Context，以及保留自有登录体系的产品使用 RS256 JWT/JWKS 委托凭据；两者均绑定一个 Platform Tenant 和非人类 Gateway Principal，且只能收紧模型、QPS 与月 Token Ceiling。平台运营者还可以向绑定 Integration 的 HTTPS 端点投递签名、仅含计量字段的用量事件：Usage 与待投递事件同事务提交，后台使用租约重试、死信和人工重放，回调失败不会阻塞网关请求。OIDC Introspection、撤销事件、平台鉴权插件、Key 级模态 Scope 与分布式并发限制仍属于路线图能力。

## 一个可信 Core，按场景插件化扩展

不同客户需要不同工作流，但不应该因此产生多套网关。AsterRouter 把安全关键决策放在统一 Core 中，把可选业务集成交给插件。

| 可信 Core | 场景插件 |
| --- | --- |
| API Key 生命周期、统一鉴权上下文与租户隔离 | 平台鉴权、JWT/JWKS、Introspection、企业目录与 SSO 适配 |
| 模型策略、额度、预算与风控 | SaaS 套餐、订阅和权益同步 |
| Provider 路由、Fallback 与成本计量 | 供应商价格源和浏览器辅助采集 |
| 同步/流式请求的直接执行、显式异步任务的公平队列、产物策略、用量和结算状态 | GPT Image、Gemini、Midjourney 兼容服务、即梦等 Provider 协议适配 |
| Integration 绑定的用量事件投递、签名、重试、死信与重放 | 客户账务、权益、ERP、CRM 和数据仓库映射 |
| 从单机到 Kubernetes 共用一个 Core | Redis、S3 兼容存储、Cloudflare R2、阿里云 OSS 和未来消息队列的可替换基础设施适配 |
| Usage、Trace、Audit 与数据治理 | 通知、导出、品牌和客户工作流 |

所有凭据来源最终进入同一网关流水线：协议归一、权限与额度、候选规划、供应商账号调度、容量控制、协议执行、用量结算和审计。插件只能通过受控接口提供供应商或业务事实，不能绕过这条流水线、签发无限制凭据、读取无关 Secret，或在可信调度器之外决定线路。

## 当前已经提供

- OpenAI 兼容的模型列表和 Chat Completions，包括流式响应。
- 多供应商调度，包括优先级、权重、容量、RPM/TPM、冷却、熔断、Sticky 路由和 Fallback。
- Workspace Key、模型白名单、限流、Token 配额、预算和超额策略。
- 用量分析、成本分摊、告警、路由 Trace、策略证据、审计日志和数据导出。
- OIDC、飞书/Lark、钉钉、部门、Group 和资源范围角色等企业身份与权限治理。
- 管理后台、员工 Portal、插件中心、备份恢复、诊断和可验证更新。
- Personal、Relay Operator、Enterprise 和 AI Platform 四种互斥的部署角色，以及中英文界面。AI Platform 已提供独立控制面、Platform Tenant、Gateway Principal、绑定 Tenant 的 workspace/service API Key、HMAC 与 RS256 JWT/JWKS 委托访问接入，以及带重试、死信和人工重放的可靠签名 HTTPS 用量回传；它不会创建外部产品的用户、会话、订阅、订单或余额。OIDC Introspection、权益适配、媒体运营和异步任务仍属于 V4 路线图。每个新建生产实例只选择一种且不可切换的部署角色；需要其他业务模型时部署独立实例。

当前网关提供 OpenAI 兼容的 Models 和 Chat Completions。Responses、Embeddings、Anthropic Messages、Gemini、实时会话、图片生成与编辑、音频、视频、媒体上传、异步任务和产物交付都属于路线图能力，不能作为当前已交付能力宣传。

多模态路线图会区分即时执行和持久排队：同步 JSON、图片/音频原生流式输出和实时会话只在容量可用时直接执行，显式异步任务才进入持久公平队列。生产最低基础设施为 PostgreSQL + Redis，不强制安装额外消息队列。可预测流量先用同一 Core 单机运行，波峰明显后再把 API、调度器、各模态 Worker、对账和产物交付拆成 Kubernetes Workload。波谷时 Worker Pod 和可选突发节点可以缩容，但供应商容量、租户预算和全局成本上限始终是硬约束。媒体可以保存在本地、AWS S3、Cloudflare R2、MinIO 等 S3 兼容存储、阿里云 OSS，或可靠交付到客户自己的存储。

## 快速开始

### 先选择部署角色

首次安装只选择一个业务部署角色。它决定初始后台、业务对象、角色和默认扩展，不是一个只改变首页的显示选项，也不是可以任意叠加的功能开关。应按谁拥有业务关系、商业结算和身份事实选择，不要按是否需要 API Key、流式输出或某个模型来选择。

| 适合选择的部署角色 | 业务与身份事实源 | 初始后台 | 明确不包含 |
| --- | --- | --- | --- |
| **个人**：个人或小团队需要轻量网关 | 自己的 Workspace 与内部协作 | `/console` | 企业组织管理、中转账务和外部产品集成 |
| **中转运营**：已有客户、余额、套餐和风控运营流程 | 自己拥有客户、套餐、余额和风控流程 | `/operator` 与 `/customer` | 企业员工管理和外部产品租户 |
| **企业**：需要治理一个组织内的员工和服务访问 | 组织拥有员工、目录和内部访问事实 | `/admin` 与 `/portal` | 中转转售对象，以及外部终端用户身份和订阅管理 |
| **AI 平台**：运营开发者 API，或为 SaaS/OEM 产品提供 AI 能力 | 平台拥有 Tenant、调用主体和接入边界；接入产品拥有最终用户 | `/platform` | 企业 HR 对象、中转余额/套餐，以及外部终端用户账号和会话 |

AI 平台与中转运营是两个独立角色。两者都可能使用 API 凭据，但中转运营管理客户余额、套餐和风控；AI 平台管理开发者 API Key 或产品委托接入的网关边界，接入产品仍然拥有自己的用户、登录会话、订阅和订单事实。企业管理员工和部门治理，个人只管理自己的 Workspace；它们是不同的业务根对象，不是同一客户模型下的四个页面。

Linux Release 安装必须传入 `--deployment`，也可预设 `ASTERROUTER_DEPLOYMENT_ROLE`；Docker 和源码开发通过 `/setup` 显式选择，且不会默认选中任何角色。选择会在首次启动时写入 PostgreSQL，并固定为当前实例的业务模型；后续启动若环境中声明的角色与已持久化角色不一致，服务会拒绝启动，环境变量不能把企业实例切换成 AI 平台或其他角色。这样可避免不同业务模型的 Provider、凭据、用量和审计数据混在一起。需要第二种业务模型时部署独立实例。已有多 Profile 实例保持兼容运行，但其 Profile 配置被冻结。面向新部署的多 Profile 仅是未来迁移能力，前提是端到端 `profile_scope`、显式 Provider Sharing Binding 和租户隔离均已落地；规则见 [部署角色与安装分流](./docs/roadmap/v4/profile-bundles-and-installation.md)。

### Linux Release

```bash
curl -sSL https://raw.githubusercontent.com/astercloud/asterrouter/main/deploy/install.sh | sudo bash -s -- install --deployment enterprise
```

将 `enterprise` 替换为 `personal`、`relay_operator` 或 `platform`。安装器会把 AsterRouter 部署到 `/opt/asterrouter`，并在 `/etc/asterrouter` 下创建服务配置。正式环境启动需要 PostgreSQL、稳定的加密密钥，以及管理员密码或 Token。新的 Linux 安装若未指定角色，会在下载或写配置前被拒绝。

### Docker

```bash
docker compose up --build
```

访问 `http://localhost:8080/setup`，选择一个部署角色，确认其启用范围和明确排除的业务对象后完成初始配置。非交互部署设置 `ASTER_DEPLOYMENT_ROLE=platform`；旧的匹配 `ASTER_PROFILES=platform` 与 `ASTER_DEFAULT_PROFILE=platform` 仍兼容。首次启动会将该选择持久化且不可切换。需要其他角色时部署独立实例。

## 私有化与托管交付

AsterRouter 支持三种交付方式：

- **自主运维：** 部署在自己的环境中，由团队独立管理。
- **私有化交付：** 部署到客户控制的网络，提供安装、迁移与验收支持。
- **托管运维：** 持续提供升级、备份、健康检查、诊断与运维支持，客户仍然掌握数据和供应商凭据。

每种交付方式都可以从低成本单机部署起步，在流量出现明显波峰后把同一 Core 和数据模型迁移为按 Role 运行的 Kubernetes Workload。Kubernetes 负责伸缩 AsterRouter Pod 和可选 Worker 节点，但不会把第三方供应商并发误当成无限容量。

官方在线服务不可用时，Core 仍可继续运行。官方 Feed 同步路径不会上传 Prompt、Response、Provider Secret、Workspace Key、详细网关用量或浏览器采集的供应商会话。

## 项目入口

- [版本发布](https://github.com/astercloud/asterrouter/releases)
- [构建状态](https://github.com/astercloud/asterrouter/actions)
- [部署环境变量模板](./deploy/asterrouter.env.example)
- [English README](./README.md)

<details>
<summary><strong>本地开发</strong></summary>

安装前端依赖并同时启动前后端：

```bash
cd frontend
npm install
cd ..
bash scripts/dev.sh
```

前端运行在 `http://localhost:5173`，并把 API 请求代理到 `http://localhost:8080`。

运行后端测试：

```bash
cd backend
go test ./...
```

构建前端：

```bash
cd frontend
npm run build
```

环境变量见[部署模板](./deploy/asterrouter.env.example)。

</details>

## 开源许可

AsterRouter 使用 [Apache License 2.0](./LICENSE)。
