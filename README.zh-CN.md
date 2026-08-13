<div align="center">

# AsterRouter

**面向企业的 AI 接入与策略治理平台。**

可私有部署的企业 AI 网关，服务企业应用、团队和内部 AI 平台。

[English](./README.md) · [产品与重构蓝图](./docs/README.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)

</div>

## AsterRouter 是什么

AsterRouter 位于企业应用与已授权的 AI 供应商之间，为企业提供一个稳定的 Gateway API 和一个统一控制面，集中管理：

- 应用接入和凭据生命周期；
- 模型服务与供应商线路；
- 访问策略与路由策略；
- 用量、成本、预算、Trace 和审计证据；
- 私有部署与企业身份边界。

```text
企业应用与员工
      |
      v
AsterRouter 企业 AI 网关
身份 -> 访问策略 -> 路由策略
      |
      v
已授权的 AI 供应商
```

## 产品形态

产品只保留两个用户入口：

| 入口 | 用途 | 路径 |
| --- | --- | --- |
| 管理控制台 | 企业管理员和应用负责人管理 AI 网关 | `/console` |
| 服务门户 | 员工和开发者查看授权接入与用量 | `/portal` |

控制台按“工作台、应用接入、模型服务、策略管理、用量与成本、组织与权限、系统管理”组织。单成员组织、SaaS 集成和大型部门只改变权限与能力，不产生新的产品模式。

## 策略是产品优势

每个请求按以下顺序处理：

```text
身份 -> 访问策略 -> 合格供应候选 -> 路由策略
     -> 容量与账号调度 -> 执行 -> 证据记录
```

访问策略回答“能不能用、能用到什么边界”；路由策略在批准的供应线路中回答“应该怎么选、如何切换”。硬性限制优先于成本、稳定性和时延偏好，所有决策都必须能够解释并留痕。

内置调度偏好为：**综合优选、成本优先、稳定优先、固定顺序**。它们是路由偏好，不是部署模式，也不是四个产品。

## 企业边界

AsterRouter 不提供个人代理工作台、AI 中转销售、客户余额、套餐、充值、兑换或转售风控。外部 SaaS/OEM 继续拥有自己的用户、会话、订阅、订单和支付；AsterRouter 只管理应用集成、授权上下文、路由、用量和成本证据。

## 当前状态

仓库正在从早期多 Profile 原型收敛到 [docs/README.md](./docs/README.md) 定义的企业产品。该文档是当前产品决策，实施阶段明确区分目标与已交付能力，未通过验收的目标行为不得对外宣称已经支持。

## 本地开发

```bash
cd frontend
npm install
cd ..
bash scripts/dev.sh
```

前端地址为 `http://localhost:5173`，并将 API 请求代理到 `http://localhost:8080`。

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

## 测试

浏览器覆盖的机器事实源为 [docs/test/v1/scenario-registry.json](./docs/test/v1/scenario-registry.json)。证明层级、测试夹具、交付门禁和证据规则见 [docs/test/v1/README.md](./docs/test/v1/README.md)。

```bash
# 静态覆盖合同与 PR 浏览器门禁
cd frontend
npm run check:e2e-coverage
npm run test:e2e:pr

# 本地后端、前端与浏览器完整测试
cd ..
bash scripts/test.sh all
```

## 许可证

AsterRouter 使用 [Apache License 2.0](./LICENSE) 授权。
