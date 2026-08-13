# AsterRouter 测试与交付事实源 v1

> 状态：`CURRENT`
> 适用范围：后端、前端、数据库、浏览器、发布包与远程验证
> 产品边界：企业管理控制台、企业服务门户、统一 AI Gateway
> 场景注册表：[`scenario-registry.json`](./scenario-registry.json)
> Owner 证据表：[`owner-evidence.json`](./owner-evidence.json)
> 路由策略算法：[`routing-policy-algorithm-matrix.md`](./routing-policy-algorithm-matrix.md)

## 1. 完整测试的定义

AsterRouter 的测试结论必须声明证明层级和声明边界。低层测试通过，不能替代高层产品闭环。

| 层级 | 运行环境 | 能证明什么 | 不能证明什么 |
| --- | --- | --- | --- |
| L0 | 静态检查、场景注册表 | 路由、场景、CI 与文档没有治理漂移 | 运行时行为 |
| L1 | Go/Vitest 单元与组件测试 | 纯规则、组件状态、序列化和边界条件 | 进程、数据库或浏览器集成 |
| L2 | `httptest` + memory repository | HTTP 合同、领域协作、鉴权和副作用 | SQL、迁移和重启持久化 |
| L3 | PostgreSQL 16 | 事务、约束、迁移、并发和重启持久化 | 生产构建与浏览器交互 |
| Gate A | Vite + memory backend + fake upstream + Playwright | 浏览器路由、表单、响应式与产品纵向闭环 | 生产单源构建和 SQL 持久化 |
| Gate B | 生产单源 Go 二进制 + PostgreSQL + fake upstream + Playwright | 候选构建、真实数据库与浏览器的交付闭环 | Linux 包、容器和远程网络 |
| Platform | Docker、Linux artifact、安装/升级/回滚 | 打包、权限、信号、安装和运维合同 | 未授权远程环境 |
| Remote | 明确授权的隔离远程环境 | 部署配置、网络和目标环境可用性 | 其他版本或其他配置类别 |

“全部功能有完整 E2E”由两个互补合同组成：

1. 每个用户可见路由至少有一个 surface contract，验证 URL、主标题、授权边界、控制台/网络错误和 `1440x900`、`1280x800`、`390x844` 三个视口。
2. 每个产品能力至少有一条 vertical journey，验证用户操作、API 结果、持久化刷新、失败路径，以及适用的 RBAC、审计、用量、Trace、告警或导出副作用。

Surface contract 不能代替 vertical journey。浏览器也不重复证明所有纯算法分支；算法和策略组合在 L1/L2 完整覆盖，浏览器证明用户能够完成真实闭环。

能力证据按风险分层，避免把低风险只读投影机械复制成四套同质测试：

- 所有 HTTP 操作必须有成功证据；
- 所有命令以及 P0 查询必须有失败和边界证据；
- 所有命令以及 P0/P1 查询必须有浏览器操作或可见投影证据；
- P2 查询只强制 owner 成功证据，但已有的失败、边界或浏览器证据仍会校验引用真实性；
- 一个 journey 只有实际调用或点击了该操作，才能登记为该操作的证据，页面可达或同页其他动作不能代替。
- browser evidence 由场景的 `operations` 单向生成，并在能力注册表中做双向一致性校验，禁止手工把未触发操作挂到场景上。
- success、negative 和 boundary evidence 由 `owner-evidence.json` 单向生成；Browser journey 不得冒充 Owner 成功证据。

## 2. 不可违反的测试规则

- `scenario-registry.json` 是浏览器覆盖的机器事实源。新增、删除或移动产品路由时必须同步更新注册表和场景。
- `owner-evidence.json` 是 Owner 测试到 API 操作的机器事实源，只能登记已核验的 `文件#TestName` 及其实际证明类型。
- 每个 Playwright 测试必须有唯一稳定的 `@e2e-*` 场景 ID；禁止用文件名、行号或自然语言标题充当稳定标识。
- 禁止固定等待。使用 web-first assertion、`expect.poll`、响应事件或可观察的 DOM 状态。
- 禁止连接真实模型供应商、生产身份源、生产邮件、生产对象存储或生产数据库。默认使用仓库内 fake upstream 和隔离 fixture。
- 禁止共享数据库、端口、用户目录和可变测试身份。测试数据必须带唯一 run ID，并只清理自身数据。
- 不允许生产 mock fallback。浏览器无法支持的能力只能放在显式 test-only fixture 中。
- 断言完整 typed object、事件或 read model 的关键字段，不只断言 HTTP 状态码或成功文案。
- 每个缺陷在根因 owner 层补回归；跨进程或用户可见缺陷再补一条 vertical slice。
- auth、RBAC、Gateway 策略、计费、迁移、备份恢复和插件信任变更必须包含 negative path。
- 测试失败必须保留 trace、截图、视频或 JUnit 证据；不得通过提高重试次数掩盖 flaky。

## 3. 浏览器场景模型

注册场景必须包含：

- `id`：唯一 `@e2e-*` 标签；
- `kind`：`surface`、`journey` 或 `setup`；
- `owner`：根因归属模块；
- `proofLevels`：实际可运行的证明层；
- `fixture`：身份、存储和外部依赖；
- `gates`：`pr`、`nightly`、`release`；
- `claim`：场景通过后允许声明的边界；
- `routes`：该场景直接验证的产品路由。
- `operations`：仅列出该 vertical journey 通过可见交互或页面投影实际触发的前端产品 API；surface 不得声明操作。

路由合同必须分别引用 `surface` 和一个或多个 `journeys`。`npm run check:e2e-coverage` 使用 TypeScript AST 读取 Vue Router 和 Playwright 测试，校验：

- 所有产品叶子路由都已登记，没有陈旧路由；
- 场景 ID 唯一，测试标题与 spec 文件一致；
- spec 文件存在，owner、fixture、proof level、gate 和 claim 完整；
- 每个路由同时有 surface 和 vertical journey；
- 注册表中的 Gate 可生成非空、可执行的 Playwright 筛选表达式。

## 4. 环境与门禁

| 门禁 | 必跑内容 | 失败处理 |
| --- | --- | --- |
| PR | L0、L1、L2、L3、`pr` 浏览器场景、首次安装 | 阻止合并 |
| Nightly | 全量 Go/Vitest、race/benchmark、全部 Chromium/Firefox/WebKit 场景 | 建立问题并阻止发布候选 |
| Release | Gate B P0 场景、首次安装、容器、HA、Linux artifact、checksum | 阻止发布 |
| Remote | 只读 health/version 起步，再运行授权的隔离 canary | 不回写默认门禁 |

本地从窄到宽执行：

```bash
cd frontend
npm run check:e2e-coverage
npm run typecheck
npm run test:unit
npm run test:e2e:pr
npm run test:e2e:full

cd ..
bash scripts/test-single-origin.sh
```

PostgreSQL、候选包与远程验证遵循 `.codex/skills/remote-tests/SKILL.md`，必须使用专用数据库和唯一证据目录。

## 5. 测试数据与身份

| Fixture | 用途 | 默认身份 | 存储 |
| --- | --- | --- | --- |
| `demo-enterprise` | Gate A 快速浏览器闭环 | `demo/demo`，企业管理员 | 独立 memory runtime |
| `registered-developer` | Portal、RBAC 与会话隔离 | 每场景唯一邮箱 | 独立 memory 或 PostgreSQL |
| `first-install` | 空实例初始化 | 每次唯一管理员 | 专用空数据库 |
| `release-enterprise` | Gate B 候选包 | `admin` + 测试密码 | `asterrouter_release_test_*` |
| `fake-openai` | Gateway 正常、流式、429、5xx、超时和断连 | 无真实凭据 | 独立本地端口 |

APIRequestContext 只用于准备前置数据和读取副作用。主要用户动作必须通过可见控件完成；测试结束后仅删除带本次 run ID 的记录。

## 6. 证据与声明

失败证据最少包括：场景 ID、URL、项目/视口、身份、locale/theme、失败交互、控制台错误、失败请求、trace 和截图路径。Gate B 还必须记录版本、commit、数据库类别、候选包和单源 URL。

测试报告只允许声明实际跑过的最高证明层。例如 Gate A 通过只能声明“开发构建浏览器闭环通过”，不能声明“PostgreSQL 候选包可发布”。环境不兼容或未执行必须显式写明原因、owner 和后续动作。

## 7. 新功能完成条件

功能只有同时满足以下条件才算完成：

1. 在根因 owner 层有成功、失败和边界测试；
2. 用户可见路由登记 surface contract；
3. 产品能力登记 vertical journey，并验证刷新持久化和适用副作用；
4. 场景进入正确 PR/Nightly/Release gate；
5. `npm run check:e2e-coverage`、相关单元测试和目标 E2E 全部通过；
6. 文档中的 claim 与实际证明层一致。
