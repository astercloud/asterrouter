<div align="center">

# AsterRouter

**Enterprise AI access, governed by policy.**

A private-deployable AI gateway for enterprise applications, teams, and internal platforms.

[简体中文](./README.zh-CN.md) · [Product and refactor blueprint](./docs/README.md)

[![Release](https://img.shields.io/github/v/release/astercloud/asterrouter?style=flat-square)](https://github.com/astercloud/asterrouter/releases)
[![CI](https://img.shields.io/github/actions/workflow/status/astercloud/asterrouter/ci.yml?branch=main&style=flat-square&label=CI)](https://github.com/astercloud/asterrouter/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/astercloud/asterrouter?style=flat-square)](./LICENSE)

</div>

## What AsterRouter is

AsterRouter is an enterprise AI gateway between applications and authorized AI providers. It gives one stable API endpoint and one control plane for:

- application access and credential lifecycle;
- model service and provider supply;
- access policies and routing policies;
- usage, cost, budgets, traces, and audit evidence;
- private deployment and enterprise identity boundaries.

```text
Enterprise applications and employees
                 |
                 v
      AsterRouter Enterprise AI Gateway
  Identity -> Access Policy -> Routing Policy
                 |
                 v
          Authorized AI providers
```

## Product shape

There is one public product entry and two authenticated work surfaces:

| Surface | Purpose | Entry |
| --- | --- | --- |
| Official Website | Public product scope, routing model, and enterprise boundary | `/` |
| Management Console | Administrators and application owners manage the enterprise gateway | `/console` |
| Service Portal | Employees and developers view authorized access and usage | `/portal` |

The website is informational and never acts as another control plane. The console is organized around Workbench, Applications, Model Services, Policies, Usage & Cost, Organization & Access, and System. A SaaS integration or a larger department changes permissions and enabled capabilities, not the authenticated product shape.

## Strategy is the product advantage

The gateway evaluates every request in this order:

```text
Identity -> Access Policy -> Candidate Supply -> Routing Policy
         -> Capacity and account scheduling -> Execution -> Evidence
```

Access Policy answers whether a request is allowed and within which limits. Routing Policy selects among approved supply routes using explicit guardrails, cost and reliability preferences, capacity, and failover rules. Every decision is explainable and recorded.

The four built-in routing preferences are **Cost First**, **Speed First**, **Stability First**, and **Balanced**. Fixed order is expressed through ordered resource batches, not a fifth preference.

## Enterprise boundary

AsterRouter does not provide personal proxy workspaces, AI relay resale operations, customer balances, plans, recharge, redemption, or resale risk workflows. An external SaaS or OEM keeps its own users, sessions, subscriptions, orders, and payments; AsterRouter manages the application integration, authorization context, routing, usage, and cost evidence.

## Status

The repository is converging from an earlier multi-profile prototype to the enterprise-only product defined in [docs/README.md](./docs/README.md). The blueprint is the current product decision, while its implementation phases are explicit; unfinished target behavior must not be presented as shipped functionality.

## Local development

```bash
cd frontend
npm install
cd ..
bash scripts/dev.sh
```

The frontend runs at `http://localhost:5173` and proxies API traffic to the backend at `http://localhost:8080`.

Run the backend test suite:

```bash
cd backend
go test ./...
```

Build the frontend:

```bash
cd frontend
npm run build
```

## Testing

The machine-readable browser coverage source is [docs/test/v1/scenario-registry.json](./docs/test/v1/scenario-registry.json). The proof levels, fixtures, delivery gates, and evidence rules are documented in [docs/test/v1/README.md](./docs/test/v1/README.md).

```bash
# Static coverage contract and pull-request browser gate
cd frontend
npm run check:e2e-coverage
npm run test:e2e:pr

# Full local backend, frontend, and browser suite
cd ..
bash scripts/test.sh all
```

## License

AsterRouter is licensed under the [Apache License 2.0](./LICENSE).
