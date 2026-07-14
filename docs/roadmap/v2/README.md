# AsterRouter V2 完整版论证文档索引

> **状态说明**：本目录是当前完整 V2 的产品、架构和实施事实源。V2 不按 MVP 口径设计，按可私有化、可商业化、可插件扩展的完整 AI Gateway Platform 交付；尚未验收的能力必须明确标为进行中或未开始。

## 核心结论

AsterRouter V2 的产品定义：

```text
AsterRouter = 一个支持个人、团队、中转运营和企业内部分发的插件化 AI Gateway Platform
```

V2 的设计重点不是“功能少一点先上线”，而是把核心边界一次定清楚：

- 客户端只配置一个 Base URL 和一个 Workspace Key，即可在 Codex App、CLI、Claude Code、IDE、脚本和内部服务之间通用。
- Personal、Relay Operator、Enterprise 可以首次安装时多选，也可以安装后由超级管理员开通或关闭。
- 多 Profile 共用 Core，但导航、权限、默认能力、商业能力、页面语义必须隔离。
- Provider、Provider Account、模型、路由、分组、使用记录、调度、监控、审计、插件、更新、备份属于公共核心。
- V2 不再设计多层客户端接入对象；客户端只面向一个 Workspace Key，管理侧用分组、标签、Key Policy 和审计维度补充治理。
- PostgreSQL 是唯一主数据库事实源，Redis-compatible cache 作为可选运行态加速层。
- 插件系统从 V2 起作为商业化核心能力设计，支持前后端插件、内置插件、Sidecar 插件、官方服务插件、开放平台插件。
- 官方服务中心提供插件目录、更新、License、兑换码、签名、兼容性、安全公告、官方加密数据服务，但本地私有化实例不能依赖它才能运行。
- 英文作为产品第一语言，中文作为辅助语言；当前论证文档用中文表达，架构图、流程图、时序图优先中文标注。

## 文档清单

| 文档 | 用途 |
| --- | --- |
| [Octafuse Gateway 参考调研](./octafuse-gateway-reference-research.md) | 梳理 Octafuse 可参考、不应照搬、需要改造吸收的设计点 |
| [V2 产品定位与 PRD](./asterrouter-v2-product-positioning.md) | 背景、目标、收益、用户画像、用户故事、完整功能边界 |
| [网关生命周期与调度设计](./asterrouter-v2-gateway-lifecycle-and-scheduling.md) | 单 Key、请求生命周期、鉴权、路由、预算、限流、熔断、fallback、sticky、异步记账 |
| [数据模型与使用分析](./asterrouter-v2-data-model-and-usage-analytics.md) | PostgreSQL 核心表、用量日志、审计、分组、预算、成本、分析页面 |
| [分层架构与代码目录](./asterrouter-v2-layered-architecture-and-code-structure.md) | Go + Vue + PostgreSQL 的单体优先架构、后台隔离、代码目录、部署路由 |
| [导航信息架构](./asterrouter-v2-navigation-information-architecture.md) | 四类 Surface 的菜单分组、权限入口、插件导航贡献和验收边界 |
| [插件与官方服务平台](./asterrouter-v2-plugin-and-official-service-platform.md) | 插件生命周期、前后端插件、官方服务中心、兑换码、支付、开放平台和服务插件 |
| [V2 实施计划与验收标准](./asterrouter-v2-implementation-plan.md) | 阶段拆分、优先级、验收标准、风险和非目标 |

## 阅读顺序

1. 先读 [Octafuse Gateway 参考调研](./octafuse-gateway-reference-research.md)，确认哪些能力可以吸收。
2. 再读 [V2 产品定位与 PRD](./asterrouter-v2-product-positioning.md)，统一产品对象和用户路径。
3. 再读 [网关生命周期与调度设计](./asterrouter-v2-gateway-lifecycle-and-scheduling.md)，锁定单 Key 体验和网关核心正确性。
4. 再读 [数据模型与使用分析](./asterrouter-v2-data-model-and-usage-analytics.md)，确认 PostgreSQL 事实源和统计口径。
5. 再读 [分层架构与代码目录](./asterrouter-v2-layered-architecture-and-code-structure.md)，确认代码如何组织。
6. 再读 [导航信息架构](./asterrouter-v2-navigation-information-architecture.md)，确认四类后台的菜单和权限边界。
7. 再读 [插件与官方服务平台](./asterrouter-v2-plugin-and-official-service-platform.md)，确认商业化和插件运行时边界。
8. 最后读 [V2 实施计划与验收标准](./asterrouter-v2-implementation-plan.md)，进入研发拆解。

## V2 相对 V1 的关键修正

| 方向 | V1 表达 | V2 修正 |
| --- | --- | --- |
| 产品阶段 | MVP / 前期论证混合 | 完整版前期论证 |
| 接入对象 | 企业倾向多层治理对象 | 默认 Workspace Key，一个 Key 通用，不再设计额外客户端接入对象 |
| 多 Profile | 已提出 Personal / Operator / Enterprise | 明确可多选、可后期开通、UI 隔离、数据边界隔离 |
| 网关核心 | 基础 Provider / Key / Usage | 增强为完整请求生命周期、调度计划、熔断、sticky、异步记账 |
| 插件商业化 | 已有分类和生命周期 | 扩展为前后端插件、官方服务插件、开放平台、兑换码、支付、云端服务 |
| 数据服务 | Provider Trust 专项 | 纳入官方加密数据服务和本地插件消费机制 |
| 部署 | 私有化和单入口 | 明确无域名、内网 IP、localhost、单域名、多入口 path routing |
| 技术栈 | Go + Vue + PostgreSQL 倾向 | 继续保留，但强调插件可多语言 Sidecar，热路径仍收敛在 Core |

## 关键原则

- 不为兼容历史形态保留复杂包袱，当前产品刚开始开发，可以大胆清理。
- Core 必须小而稳，插件可以丰富，但热路径不能被插件随意破坏。
- Personal 和 Relay Operator 不是 Enterprise 的附属页面，而是独立产品入口。
- 客户端体验优先，一个 Key 通用优先于理想化多层治理。
- 使用记录、Provider 分组、Key 管理、路由、调度、监控、审计必须是完整 CRUD，不是单页堆叠。
- 官方服务和官方插件可以商业化，但公共核心插件不能收费。
- 本地私有化实例必须在离线、无域名、无公网环境下可用。
- 官方服务中心不能接收 prompt、response、Provider Secret、客户 API Key 原文或默认 usage 明细。
- 图示优先用中文标注，便于团队评审。
